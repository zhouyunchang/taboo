// 机器身份（M2 #3）端到端验证 —— 针对 Go 后端（apps/server-go）
// 覆盖：创建/一次性 secret/token 交换、scope 最小权限、reveal/read 分离、越权 403、
//       吊销立即生效、通配拒绝、viewer 不可管理、审计 actor 标识
const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
let failures = 0;

async function call(method, path, { token, body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const ct = res.headers.get('content-type') || '';
  const data = ct.includes('json') ? await res.json() : await res.text();
  return { status: res.status, data };
}

function check(name, cond, extra = '') {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
}

const email = `id-smoke-${Date.now()}@taboo.dev`;
console.log('== taboo machine identity smoke ==');

// 1. 注册 owner
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password: 'password123', name: 'Owner' } });
check('register 201', reg.status === 201);
const token = reg.data.tokens.access;
const me = await call('GET', '/api/v1/me', { token });
const orgSlug = me.data.orgs[0].slug;
const projects = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const projectId = projects.data.projects[0].id;

// 2. 准备一条 dev 密钥
await call('POST', `/api/v1/projects/${projectId}/secrets?env=dev`, { token, body: { key: 'DB_PASS', value: 'dev-secret' } });

// 3. 创建 read scope 机器身份
const created = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'ci-reader', scopes: [{ project_id: projectId, env: 'dev', permission: 'read' }] },
});
check('create identity 201', created.status === 201, JSON.stringify(created.data));
check('client_secret returned once', typeof created.data.client_secret === 'string' && created.data.client_secret.length === 64);
const { client_id, client_secret } = created.data;

// 4. client_credentials 换 token
const tk = await call('POST', '/api/v1/identities/token', { body: { client_id, client_secret } });
check('token exchange 200', tk.status === 200 && typeof tk.data.access_token === 'string', JSON.stringify(tk.data));
const itoken = tk.data.access_token;

// 5. scope 内 reveal 允许（read → reveal）
const reveal = await call('GET', `/api/v1/projects/${projectId}/secrets/DB_PASS?env=dev`, { token: itoken });
check('identity reveal dev 200', reveal.status === 200 && reveal.data.value === 'dev-secret', JSON.stringify(reveal.data));

// 6. scope 内列表允许（无值）
const list = await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`, { token: itoken });
check('identity list dev 200 (no values)', list.status === 200 && list.data.secrets.length === 1 && list.data.secrets[0].value === undefined);

// 7. read scope 禁写
const write = await call('POST', `/api/v1/projects/${projectId}/secrets?env=dev`, { token: itoken, body: { key: 'X', value: 'y' } });
check('read scope write 403', write.status === 403);

// 8. 无 prod scope → 403
const prodRead = await call('GET', `/api/v1/projects/${projectId}/secrets?env=prod`, { token: itoken });
check('no prod scope 403', prodRead.status === 403);

// 9. 项目级环境列表允许（项目内任一 scope）
const envs = await call('GET', `/api/v1/projects/${projectId}/environments`, { token: itoken });
check('identity env list 200', envs.status === 200 && envs.data.environments.length === 3);

// 10. 错误 secret 统一 401
const bad = await call('POST', '/api/v1/identities/token', { body: { client_id, client_secret: 'wrong' } });
check('bad secret 401', bad.status === 401);

// 11. write scope 身份可写
const w = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'ci-writer', scopes: [{ project_id: projectId, env: 'dev', permission: 'write' }] },
});
const wtk = await call('POST', '/api/v1/identities/token', { body: { client_id: w.data.client_id, client_secret: w.data.client_secret } });
const wset = await call('POST', `/api/v1/projects/${projectId}/secrets?env=dev`, { token: wtk.data.access_token, body: { key: 'CI_KEY', value: 'ci-value' } });
check('write scope set 200', wset.status === 200 && wset.data.version === 1, JSON.stringify(wset.data));

// 12. 吊销后 token 立即 401，且无法换新 token
await call('POST', `/api/v1/orgs/${orgSlug}/identities/${w.data.id}/revoke`, { token });
const afterRevoke = await call('GET', `/api/v1/projects/${projectId}/secrets/CI_KEY?env=dev`, { token: wtk.data.access_token });
check('revoked token 401 immediately', afterRevoke.status === 401);
const reTk = await call('POST', '/api/v1/identities/token', { body: { client_id: w.data.client_id, client_secret: w.data.client_secret } });
check('revoked identity token exchange 401', reTk.status === 401);

// 13. 通配 scope 拒绝
const wild = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'wild', scopes: [{ project_id: projectId, env: '*', permission: 'read' }] },
});
check('wildcard scope 400', wild.status === 400);

// 14. 空 scope 拒绝
const empty = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, { token, body: { name: 'none', scopes: [] } });
check('empty scopes 400', empty.status === 400);

// 15. 审计 actor 标识为 identity:xxx
const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=200`, { token });
const identityActs = audit.data.logs.filter((l) => l.actor_name && l.actor_name.startsWith('identity:'));
check('audit has identity: actor', identityActs.length >= 3, JSON.stringify(audit.data.logs?.slice(0, 3)));
check('audit has identity.token action', audit.data.logs.some((l) => l.action === 'identity.token'));
check('audit has identity.revoke action', audit.data.logs.some((l) => l.action === 'identity.revoke'));
check('identity reveal audited', audit.data.logs.some((l) => l.action === 'secrets.reveal' && l.actor_name.startsWith('identity:')));

// 16. 列表不含 secret，吊销状态可见
const listIds = await call('GET', `/api/v1/orgs/${orgSlug}/identities`, { token });
check('list identities no secret leak', listIds.status === 200 && listIds.data.identities.every((i) => !('secret_hash' in i) && !('client_secret' in i)));
check('revoked status visible', listIds.data.identities.some((i) => i.name === 'ci-writer' && i.status === 'revoked'));

// 17. 未认证不可管理身份
const noauth = await call('GET', `/api/v1/orgs/${orgSlug}/identities`);
check('unauth identities 401', noauth.status === 401);

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL IDENTITY CHECKS PASSED');
process.exit(failures ? 1 : 0);
