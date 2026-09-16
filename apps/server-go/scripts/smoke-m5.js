// M5 冒烟：Webhooks（HMAC 验签 + 投递）、Secret Sync 入队、审计导出、OIDC 端点
// 运行：TABOO_BASE=http://localhost:7101 node smoke-m5.js
import http from 'node:http';
import crypto from 'node:crypto';

const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
const results = [];
const ok = (name, cond, extra = '') => {
  results.push([cond, name]);
  console.log(`  ${cond ? '✓' : '✗'} ${name}${extra ? ' — ' + extra : ''}`);
  if (!cond) process.exitCode = 1;
};

async function req(method, path, body, token) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data;
  try { data = JSON.parse(text); } catch { data = text; }
  return { status: res.status, data, headers: res.headers };
}

// 本地 webhook 接收端：记录请求并验签
const received = [];
const receiver = http.createServer((req2, res2) => {
  let body = '';
  req2.on('data', (c) => { body += c; });
  req2.on('end', () => {
    received.push({ headers: req2.headers, body });
    res2.writeHead(200).end('{}');
  });
});
await new Promise((r) => receiver.listen(0, r));
const receiverPort = receiver.address().port;

// 1. 注册
const email = `m5-${Date.now()}@smoke.dev`;
const reg = await req('POST', '/api/v1/auth/register', { email, password: 'smoke-password', name: 'M5 Smoke' });
ok('register', reg.status === 201, `org slug via /me`);
const token = reg.data.tokens.access;
const me = await req('GET', '/api/v1/me', null, token);
const slug = me.data.orgs[0].slug;
const project = (await req('GET', `/api/v1/orgs/${slug}/projects`, null, token)).data.projects[0];
ok('me + projects', !!slug && !!project);

// 2. Webhook：创建 → 写密钥 → 等投递 → 验签
const hook = await req('POST', `/api/v1/orgs/${slug}/webhooks`, {
  url: `http://127.0.0.1:${receiverPort}/hook`, secret: 'smoke-signing-secret',
  events: ['secret.created'],
}, token);
ok('webhook create (secret returned once)', hook.status === 201 && !!hook.data.secret);

await req('POST', `/api/v1/projects/${project.id}/secrets?env=dev`, { key: 'M5_KEY', value: 'm5-value' }, token);
await new Promise((r) => setTimeout(r, 6500)); // 等投递 worker（5s 轮询）
ok('webhook delivered', received.length >= 1, `${received.length} delivery(ies)`);

if (received.length) {
  const { headers, body } = received[0];
  const sig = headers['x-taboo-signature'] || '';
  const m = sig.match(/t=(\d+),v1=([0-9a-f]+)/);
  let sigOK = false;
  if (m) {
    const expect = crypto.createHmac('sha256', 'smoke-signing-secret').update(`${m[1]}.${body}`).digest('hex');
    sigOK = expect === m[2] && Math.abs(Date.now() / 1000 - Number(m[1])) < 300;
  }
  ok('HMAC-SHA256 signature valid + timestamp window', sigOK);
  const payload = JSON.parse(body);
  ok('payload has no plaintext', payload.key === 'M5_KEY' && !JSON.stringify(payload).includes('m5-value'),
    `event=${payload.type} v${payload.version}`);
}

// 3. Secret Sync：创建目标（不 ping）→ 写密钥 → 入队记录
const target = await req('POST', `/api/v1/orgs/${slug}/sync/targets`, {
  name: 'smoke-github', platform: 'github', project_id: project.id, env_slug: 'dev',
  config: { token: 'fake', owner: 'o', repo: 'r' },
}, token);
ok('sync target create', target.status === 201, target.data.id);
await req('POST', `/api/v1/projects/${project.id}/secrets?env=dev`, { key: 'M5_SYNC_KEY', value: 'sync-me' }, token);
await new Promise((r) => setTimeout(r, 12000));
const runs = await req('GET', `/api/v1/orgs/${slug}/sync/runs`, null, token);
const run = (runs.data.runs || []).find((x) => x.secret_key === 'M5_SYNC_KEY');
ok('sync run enqueued + attempted', !!run, run ? `status=${run.status} attempts=${run.attempts}` : 'no run');
ok('sync failure recorded (fake token, no plaintext leak)',
  !!run && (run.status === 'failed' || run.status === 'dead') && !JSON.stringify(run).includes('sync-me'));

// 4. 审计导出 CSV / JSONL
const csvRes = await fetch(`${BASE}/api/v1/orgs/${slug}/audit/export?format=csv`, { headers: { Authorization: `Bearer ${token}` } });
const csvText = await csvRes.text();
ok('audit export CSV', csvRes.status === 200 && csvText.includes('secrets.create') && csvText.includes('webhook.create'), `status=${csvRes.status} head=${csvText.slice(0, 80).replace(/\n/g, '|')}`);
const jsonlRes = await fetch(`${BASE}/api/v1/orgs/${slug}/audit/export?format=jsonl`, { headers: { Authorization: `Bearer ${token}` } });
const jsonlText = await jsonlRes.text();
ok('audit export JSONL', jsonlRes.status === 200 && jsonlText.trim().split('\n').every((l) => JSON.parse(l).action), `status=${jsonlRes.status}`);

// 5. OIDC 端点（未配置 → 404；登录限流路径可达）
const oidc404 = await req('GET', `/api/v1/auth/oidc/${slug}/login`);
ok('oidc login 404 without provider', oidc404.status === 404);
const oidcList = await req('GET', `/api/v1/orgs/${slug}/oidc`, null, token);
ok('oidc list empty', oidcList.status === 200 && oidcList.data.providers.length === 0);

// 6. 审计含 M5 动作
const audit = await req('GET', `/api/v1/orgs/${slug}/audit?limit=100`, null, token);
const actions = new Set(audit.data.logs.map((l) => l.action));
ok('audit contains webhook.create/sync.target.create/audit.export',
  actions.has('webhook.create') && actions.has('sync.target.create') && actions.has('audit.export'));

receiver.close();
console.log(results.every(([c]) => c) ? '\nM5 SMOKE: ALL CHECKS PASSED' : '\nM5 SMOKE: FAILURES');
