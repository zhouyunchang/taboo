// 冒烟测试：register|login → create secret → list(无值) → reveal → versions → rollback → export → audit → cleanup
//
// 两种账号模式：
//   缺省：注册一次性账号 smoke-<ts>@taboo.dev（仅用于一次性本地实例；结束时打印停用提示）。
//   登录模式：设置 TABOO_SMOKE_CREDENTIALS=<json 文件 {email,password}>，或 TABOO_SMOKE_EMAIL +
//   TABOO_SMOKE_PASSWORD —— 复用专用冒烟账号（注册已关闭的部署，如 dev122：deploy/dev122/smoke.sh
//   负责运行前启用、运行后停用该账号）。
// 每次运行使用唯一 Key，结束时删除自己创建的密钥（TABOO_SMOKE_KEEP=1 可保留排查）。
import { readFileSync } from 'node:fs';

const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
let failures = 0;

async function call(method, path, { token, body, raw } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (raw) return res;
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}

function check(name, cond, extra = '') {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
}

let creds = null;
if (process.env.TABOO_SMOKE_CREDENTIALS) creds = JSON.parse(readFileSync(process.env.TABOO_SMOKE_CREDENTIALS, 'utf8'));
else if (process.env.TABOO_SMOKE_EMAIL) creds = { email: process.env.TABOO_SMOKE_EMAIL, password: process.env.TABOO_SMOKE_PASSWORD };
const loginMode = !!creds;
const runId = Date.now();
const email = loginMode ? creds.email : `smoke-${runId}@taboo.dev`;
const password = loginMode ? creds.password : 'smoke-pass-123';
// 注册模式保持历史 Key 名；登录模式（长期账号）每次唯一，避免与历史残留冲突
const KEY = loginMode ? `SMOKE_${runId}` : 'DB_PASS';

console.log(`== taboo smoke test (${loginMode ? 'login: dedicated account' : 'register: throwaway account'}) ==`);
let token;
if (loginMode) {
  const li = await call('POST', '/api/v1/auth/login', { body: { email, password } });
  check('login 200', li.status === 200 && !!li.data?.tokens?.access, `status=${li.status} ${li.data?.code ?? ''}`);
  token = li.data?.tokens?.access;
} else {
  const reg = await call('POST', '/api/v1/auth/register', { body: { email, password, name: 'Smoke' } });
  check('register 201', reg.status === 201, JSON.stringify(reg.data));
  token = reg.data?.tokens?.access;
}

const me = await call('GET', '/api/v1/me', { token });
check('me returns orgs', Array.isArray(me.data?.orgs) && me.data.orgs.length >= 1);
const orgSlug = me.data?.orgs?.[0]?.slug;
const orgs = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const projectId = orgs.data?.projects?.[0]?.id;
check('default project exists', !!projectId);

const envs = await call('GET', `/api/v1/projects/${projectId}/environments`, { token });
check('dev/staging/prod envs', ['dev', 'staging', 'prod'].every((s) => envs.data?.environments?.some((e) => e.slug === s)));

const s1 = await call('POST', `/api/v1/projects/${projectId}/secrets`, { token, body: { key: KEY, value: 's3cret-v1', comment: 'db password', tags: ['db'] } });
check('create secret v1', s1.data?.version === 1, JSON.stringify(s1.data));
const s2 = await call('POST', `/api/v1/projects/${projectId}/secrets`, { token, body: { key: KEY, value: 's3cret-v2' } });
check('update secret v2', s2.data?.version === 2, JSON.stringify(s2.data));

const list = await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`, { token });
const listed = list.data?.secrets ?? [];
check('list has no values', listed.some((x) => x.key === KEY) && listed.every((x) => x.value === undefined) && (loginMode || listed.length === 1));

const reveal = await call('GET', `/api/v1/projects/${projectId}/secrets/${KEY}?env=dev`, { token });
check('reveal returns v2 value', reveal.data?.value === 's3cret-v2', JSON.stringify(reveal.data));

const vers = await call('GET', `/api/v1/projects/${projectId}/secrets/${KEY}/versions?env=dev`, { token });
check('versions has 2 entries', vers.data?.versions?.length === 2);

const rb = await call('POST', `/api/v1/projects/${projectId}/secrets/${KEY}/rollback?env=dev`, { token, body: { version: 1 } });
check('rollback creates v3', rb.data?.version === 3, JSON.stringify(rb.data));
const reveal2 = await call('GET', `/api/v1/projects/${projectId}/secrets/${KEY}?env=dev`, { token });
check('reveal after rollback = v1 value', reveal2.data?.value === 's3cret-v1');

const exp = await call('GET', `/api/v1/projects/${projectId}/export?env=dev`, { token, raw: true });
const expText = await exp.text();
check('export .env format', expText.includes(`${KEY}=s3cret-v1`), expText);

const imported = await call('POST', `/api/v1/projects/${projectId}/import?env=dev`, { token, body: { format: 'doppler', preview: true, content: '{"SMOKE_DOPPLER":"preview-only","DOPPLER_PROJECT":"demo"}' } });
check('doppler preview does not require strategy', imported.status === 200 && imported.data?.created === 1 && !(imported.data?.entries || []).some((e) => e.key?.startsWith('DOPPLER_')), JSON.stringify(imported.data));
const rotation = await call('PUT', `/api/v1/projects/${projectId}/secrets/${KEY}/rotation?env=dev`, { token, body: { interval_seconds: 86400 } });
check('rotation policy saved', rotation.status === 200 && rotation.data?.enabled === true && !JSON.stringify(rotation.data).includes('s3cret'), JSON.stringify(rotation.data));
const scan = await call('POST', `/api/v1/orgs/${orgSlug}/scan-reports`, { token, body: { source: 'smoke', findings: [{ rule_id: 'generic-api-key', level: 'warning', file: 'smoke.env', line: 1, match: 'smoke-secret-value' }] } });
check('scan report hmac', scan.status === 201 && !JSON.stringify(scan.data).includes('smoke-secret-value'), JSON.stringify(scan.data));
const scanDetail = await call('GET', `/api/v1/orgs/${orgSlug}/scan-reports/${scan.data?.id}`, { token });
check('scan detail drops match', scanDetail.status === 200 && scanDetail.data?.findings?.[0]?.alg === 'hmac-sha256' && !JSON.stringify(scanDetail.data).includes('smoke-secret-value'), JSON.stringify(scanDetail.data));
const scanDel = await call('DELETE', `/api/v1/orgs/${orgSlug}/scan-reports/${scan.data?.id}`, { token });
check('scan report deleted', scanDel.status === 200, JSON.stringify(scanDel.data));
const cache = await fetch(BASE + `/api/v1/projects/${projectId}/secrets?env=dev`, { headers: { Authorization: `Bearer ${token}` } });
check('cache-control no-store', cache.headers.get('cache-control') === 'no-store');
await cache.json().catch(() => {});

const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit`, { token });
const actions = new Set(audit.data?.logs?.map((l) => l.action));
check('audit has create/reveal/rollback/export', ['secrets.create', 'secrets.reveal', 'secrets.rollback', 'secrets.export'].every((a) => actions.has(a)), [...actions].join(','));

// 负向：未认证 / 越权环境
const noauth = await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`);
check('unauth rejected 401', noauth.status === 401);

// 清理：删除本次创建的密钥；注册模式提示停用一次性账号
if (projectId && token && process.env.TABOO_SMOKE_KEEP !== '1') {
  await call('DELETE', `/api/v1/projects/${projectId}/secrets/${KEY}/rotation?env=dev`, { token });
  const del = await call('DELETE', `/api/v1/projects/${projectId}/secrets/${KEY}?env=dev&expectedVersion=3`, { token });
  check('cleanup: smoke secret deleted', del.status === 200 || del.status === 204, `status=${del.status} ${JSON.stringify(del.data)}`);
}
if (!loginMode) {
  console.log(`  i throwaway account ${email} — on a persistent deployment run: taboo-server user disable ${email}`);
}

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL CHECKS PASSED');
process.exit(failures ? 1 : 0);
