// 动态密钥（M4 #9）端到端验证 —— 针对 Go 后端（TABOO_DYNAMIC_ENGINE=mock 启动）
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
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}
const check = (name, cond, extra = '') => {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const email = `dyn-smoke-${Date.now()}@taboo.dev`;
console.log('== taboo dynamic-secrets smoke ==');
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password: 'password123' } });
const token = reg.data.tokens.access;
const me = await call('GET', '/api/v1/me', { token });
const orgSlug = me.data.orgs[0].slug;
const projects = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const pid = projects.data.projects[0].id;

// 1. 建引擎配置（owner）
const eng = await call('POST', `/api/v1/projects/${pid}/dynamic-engines`, {
  token, body: { name: 'pg-main', connection_string: 'postgres://mock:mock@127.0.0.1:5432/mockdb', database: 'mockdb', default_ttl: 3600, max_ttl: 86400 },
});
check('create engine 201', eng.status === 201 && !!eng.data.id, JSON.stringify(eng.data));
check('conn string not echoed', !JSON.stringify(eng.data).includes('postgres://mock:mock'));

// 2. 列表：无连接串、无密码
const list0 = await call('GET', `/api/v1/projects/${pid}/dynamic-engines`, { token });
const engRow = list0.data.engines?.find((e) => e.id === eng.data.id);
check('list engine ok', !!engRow && engRow.type === 'postgres' && Array.isArray(engRow.leases));
check('list leaks no secret material', !JSON.stringify(list0.data).includes('mock:mock'));

// 3. 用户 token 申请 lease → 拒绝
const userLease = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token, body: {} });
check('user token lease 403', userLease.status === 403, JSON.stringify(userLease.data));

// 4. 机器身份（本项目 read scope）申请 lease
const identA = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'dyn-agent-a', token_ttl: 900, scopes: [{ project_id: pid, env: 'dev', permission: 'read' }] },
});
const tokA = await call('POST', '/api/v1/identities/token', { body: { client_id: identA.data.client_id, client_secret: identA.data.client_secret } });
const atA = tokA.data.access_token;

const lease1 = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token: atA, body: { ttl: 120 } });
check('identity lease 201', lease1.status === 201 && /^taboo_[0-9a-f]{12}$/.test(lease1.data.username) && /^[0-9a-f]{32}$/.test(lease1.data.password),
  JSON.stringify(lease1.data).slice(0, 150));
const exp1 = lease1.data.expires_at;
check('ttl honored ±5s', Math.abs(exp1 - Math.floor(Date.now() / 1000) - 120) <= 5);

// 5. 无本项目 scope 的身份 → 403（scope 在另一个项目）
await call('POST', `/api/v1/orgs/${orgSlug}/projects`, { token, body: { name: 'other' } });
const projects2 = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const pid2 = projects2.data.projects.find((p) => p.slug !== 'default')?.id;
const identB = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'dyn-agent-b', token_ttl: 900, scopes: [{ project_id: pid2, env: 'dev', permission: 'read' }] },
});
const tokB = await call('POST', '/api/v1/identities/token', { body: { client_id: identB.data.client_id, client_secret: identB.data.client_secret } });
const denied = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token: tokB.data.access_token, body: {} });
check('cross-project identity lease 403', denied.status === 403, JSON.stringify(denied.data));

// 6. 续期：正常 + 超上限
const renew = await call('POST', `/api/v1/projects/${pid}/dynamic-leases/${lease1.data.id}/renew`, { token: atA, body: { ttl: 600 } });
check('renew extends expiry', renew.status === 200 && renew.data.expires_at > exp1, JSON.stringify(renew.data));
const renewBig = await call('POST', `/api/v1/projects/${pid}/dynamic-leases/${lease1.data.id}/renew`, { token: atA, body: { ttl: 999999 } });
check('renew beyond max_ttl 400', renewBig.status === 400, JSON.stringify(renewBig.data));

// 7. 同身份第二个 lease（多 lease 并存）
const lease2 = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token: atA, body: { ttl: 3000 } });
check('second lease ok', lease2.status === 201 && lease2.data.username !== lease1.data.username);

// 8. 手动回收 lease1（owner 代管）
const rev1 = await call('POST', `/api/v1/projects/${pid}/dynamic-leases/${lease1.data.id}/revoke`, { token });
check('manual revoke 200', rev1.status === 200 && rev1.data.status === 'revoked');
const listAfter = await call('GET', `/api/v1/projects/${pid}/dynamic-engines`, { token });
const active1 = listAfter.data.engines.find((e) => e.id === eng.data.id).leases;
check('revoked lease leaves active list', active1.every((l) => l.id !== lease1.data.id) && active1.length === 1);

// 9. 短 TTL lease + worker 到期回收（轮询至多 45s，worker 周期 20s）
const lease3 = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token: atA, body: { ttl: 2 } });
let expired = false;
for (let i = 0; i < 9 && !expired; i++) {
  await sleep(5000);
  const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=300`, { token });
  expired = audit.data.logs.some((l) => l.action === 'dynamic.lease.expire');
}
check('worker expired short-ttl lease', expired);

// 10. 吊销 identity → 立即拒绝新 lease + worker 联动回收其活跃 lease
await call('POST', `/api/v1/orgs/${orgSlug}/identities/${identA.data.id}/revoke`, { token });
const afterRevoke = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, { token: atA, body: { ttl: 60 } });
check('revoked identity lease 401', afterRevoke.status === 401, JSON.stringify(afterRevoke.data));
let swept = false;
let sweepAudit = { data: { logs: [] } };
for (let i = 0; i < 9 && !swept; i++) {
  await sleep(5000);
  sweepAudit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=300`, { token });
  swept = sweepAudit.data.logs.some((l) => l.action === 'dynamic.lease.revoke' && JSON.stringify(l.metadata || '').includes('identity_revoked'));
}
check('identity revoke sweeps active lease', swept);

// 11. 审计动作齐全
const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=400`, { token });
const acts = new Set(audit.data.logs.map((l) => l.action));
check('audit has dynamic lifecycle', ['dynamic.engine.create', 'dynamic.lease.create', 'dynamic.lease.renew', 'dynamic.lease.revoke', 'dynamic.lease.expire'].every((a) => acts.has(a)),
  [...acts].filter((a) => a.startsWith('dynamic')).join(','));

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL DYNAMIC CHECKS PASSED');
process.exit(failures ? 1 : 0);
