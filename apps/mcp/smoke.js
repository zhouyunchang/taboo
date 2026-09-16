// MCP Server 验收冒烟（M4 #10）—— 起真实服务 + 真实 mcp 进程，走 stdio JSON-RPC
// 验收：吊销 identity 后下一次 tool call 必须拒绝且产生审计记录
import { spawn } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
const BIN = path.join(path.dirname(fileURLToPath(import.meta.url)), 'taboo-mcp.exe');
let failures = 0;

async function api(method, p, { token, body } = {}) {
  const res = await fetch(BASE + p, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  return { status: res.status, data: await res.json().catch(() => ({})) };
}
const check = (name, cond, extra = '') => {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
};

// ---- MCP stdio 客户端 ----
function startMcp(env) {
  const child = spawn(BIN, [], { env: { ...process.env, ...env }, stdio: ['pipe', 'pipe', 'pipe'] });
  let buf = '';
  const pending = new Map();
  let nextId = 0;
  child.stdout.on('data', (d) => {
    buf += d.toString();
    let i;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i); buf = buf.slice(i + 1);
      if (!line.trim()) continue;
      const msg = JSON.parse(line);
      if (msg.id !== undefined && pending.has(msg.id)) { pending.get(msg.id)(msg); pending.delete(msg.id); }
    }
  });
  const stderr = [];
  child.stderr.on('data', (d) => stderr.push(d.toString()));
  const call = (method, params) => new Promise((resolve, reject) => {
    const id = ++nextId;
    pending.set(id, resolve);
    child.stdin.write(JSON.stringify({ jsonrpc: '2.0', id, method, params }) + '\n');
    setTimeout(() => { if (pending.has(id)) { pending.delete(id); reject(new Error('timeout ' + method)); } }, 15_000);
  });
  return { child, call, stderr: () => stderr.join('') };
}

console.log('== taboo mcp smoke ==');
const email = `mcp-smoke-${Date.now()}@taboo.dev`;
const reg = await api('POST', '/api/v1/auth/register', { body: { email, password: 'password123' } });
const token = reg.data.tokens.access;
const me = await api('GET', '/api/v1/me', { token });
const orgSlug = me.data.orgs[0].slug;
const projects = await api('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const pid = projects.data.projects[0].id;
await api('POST', `/api/v1/projects/${pid}/secrets?env=dev`, { token, body: { key: 'MCP_DEMO', value: 'mcp-secret-value', tags: ['mcp'] } });

// 机器身份：read scope，限 dev
const ident = await api('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'mcp-agent', token_ttl: 900, scopes: [{ project_id: pid, env: 'dev', permission: 'read' }] },
});
check('identity created', ident.status === 201 && !!ident.data.client_secret, JSON.stringify(ident.data));

const mcpEnv = {
  TABOO_SERVER: BASE,
  TABOO_CLIENT_ID: ident.data.client_id,
  TABOO_CLIENT_SECRET: ident.data.client_secret,
  TABOO_PROJECT_ID: pid,
  TABOO_ENV: 'dev',
};
const mcp = startMcp(mcpEnv);

// 1. initialize / tools/list
const init = await mcp.call('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'smoke', version: '0' } });
check('initialize', init.result?.serverInfo?.name === 'taboo', JSON.stringify(init).slice(0, 150));
mcp.child.stdin.write(JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) + '\n');
const tools = await mcp.call('tools/list', {});
const names = (tools.result?.tools ?? []).map((t) => t.name).sort();
check('exactly 2 tools', names.length === 2 && names[0] === 'secrets.get' && names[1] === 'secrets.list', names.join(','));

// 2. secrets.list：元数据，无值
const list = await mcp.call('tools/call', { name: 'secrets.list', arguments: {} });
check('list ok no values', !list.result?.isError && list.result?.content?.[0]?.text?.includes('MCP_DEMO')
  && !list.result.content[0].text.includes('mcp-secret-value'), list.result?.content?.[0]?.text);

// 3. secrets.get：默认掩码
const getMasked = await mcp.call('tools/call', { name: 'secrets.get', arguments: { key: 'MCP_DEMO' } });
check('get masked by default', !getMasked.result?.isError && getMasked.result?.content?.[0]?.text?.includes('mc••••••••••••ue')
  && !getMasked.result.content[0].text.includes('mcp-secret-value'), getMasked.result?.content?.[0]?.text);

// 4. secrets.get reveal=true：明文 + 审计
const getReveal = await mcp.call('tools/call', { name: 'secrets.get', arguments: { key: 'MCP_DEMO', reveal: true } });
check('get reveal returns plaintext', !getReveal.result?.isError && getReveal.result?.content?.[0]?.text?.includes('mcp-secret-value'));

// 5. 越权探测：身份 scope 限定 dev → 访问 prod 必须拒绝
const prodDenied = await mcp.call('tools/call', { name: 'secrets.list', arguments: { env: 'prod' } });
check('dev-scoped identity prod denied 403', prodDenied.result?.isError === true
  && prodDenied.result?.content?.[0]?.text?.includes('access denied'), prodDenied.result?.content?.[0]?.text);

// 6. 吊销 identity → 下一次调用必须失败，且服务端留审计
const revoke = await api('POST', `/api/v1/orgs/${orgSlug}/identities/${ident.data.id}/revoke`, { token });
check('identity revoked', revoke.status === 200);
const afterRevoke = await mcp.call('tools/call', { name: 'secrets.list', arguments: {} });
check('revoked → call denied', afterRevoke.result?.isError === true
  && afterRevoke.result?.content?.[0]?.text?.includes('access denied'), JSON.stringify(afterRevoke.result).slice(0, 200));

const audit = await api('GET', `/api/v1/orgs/${orgSlug}/audit?limit=300`, { token });
const actorHit = audit.data.logs.some((l) => (l.actor_name || '').startsWith('identity:') || (l.actor_type === 'identity'));
const revealHit = audit.data.logs.some((l) => l.action === 'secrets.reveal');
check('audit has identity actor', actorHit);
check('audit has secrets.reveal (masked list) / reveal attempt', revealHit, audit.data.logs.map((l) => l.action).slice(0, 10).join(','));

mcp.child.kill();
console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL MCP CHECKS PASSED');
process.exit(failures ? 1 : 0);
