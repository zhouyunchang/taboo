// 动态密钥真实 PostgreSQL 联调（M4 #9 收尾）—— 针对真实引擎启动的服务
// （不设 TABOO_DYNAMIC_ENGINE，连接串指向容器）。除 API 行为外，直接查 pg_roles /
// 用 lease 账号登录验证 CREATE USER / VALID UNTIL / 授权 / DROP 真实发生。
import { execFileSync } from 'node:child_process';

const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
const PG = process.env.TABOO_TEST_PG || 'postgres://postgres:taboo_test_pw@localhost:5433/mockdb?sslmode=disable';
const CONTAINER = process.env.TABOO_TEST_PG_CONTAINER || 'taboo-pg-test';
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
  return { status: res.status, data: await res.json().catch(() => ({})) };
}
const check = (name, cond, extra = '') => {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// 容器内执行 psql（经 WSL 里的 podman）
const psql = (sql) => execFileSync('wsl', ['-d', 'podman-machine-default', '-e', 'podman', 'exec', CONTAINER,
  'psql', '-U', 'postgres', '-d', 'mockdb', '-tAc', sql], { encoding: 'utf8' }).trim();
// pg 返回 timestamptz 带 +00 后缀（如 2026-09-16 08:49:26+00），转 epoch 前剥掉
const pgTs = (s) => Math.floor(new Date(s.replace(' ', 'T').replace(/\+00$/, '') + 'Z').getTime() / 1000);
const roleLine = (u) => psql(`SELECT rolname || '|' || COALESCE(rolvaliduntil::text, 'null') FROM pg_roles WHERE rolname = '${u}'`);
const roleGone = (u) => { try { return roleLine(u) === ''; } catch { return true; } };

const email = `dyn-pg-${Date.now()}@taboo.dev`;
console.log('== taboo dynamic-secrets REAL POSTGRES integration ==');
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password: 'password123' } });
const token = reg.data.tokens.access;
const me = await call('GET', '/api/v1/me', { token });
const orgSlug = me.data.orgs[0].slug;
const projects = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const pid = projects.data.projects[0].id;

// 1. 真实引擎配置（postgres 超管连接串，指向容器）
const eng = await call('POST', `/api/v1/projects/${pid}/dynamic-engines`, {
  token, body: { name: 'pg-real', connection_string: PG, database: 'mockdb', default_ttl: 3600, max_ttl: 86400 },
});
check('create engine 201 (real pg)', eng.status === 201 && !!eng.data.id, JSON.stringify(eng.data));

// 2. 机器身份申请 lease
const ident = await call('POST', `/api/v1/orgs/${orgSlug}/identities`, {
  token, body: { name: 'pg-agent', token_ttl: 900, scopes: [{ project_id: pid, env: 'dev', permission: 'read' }] },
});
const tok = await call('POST', '/api/v1/identities/token', {
  body: { client_id: ident.data.client_id, client_secret: ident.data.client_secret },
});
const lease = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, {
  token: tok.data.access_token, body: { ttl: 120 },
});
check('identity lease 201', lease.status === 201 && /^taboo_[0-9a-f]{12}$/.test(lease.data.username), JSON.stringify(lease.data).slice(0, 120));
const u1 = lease.data.username;

// 3. pg_roles 侧：角色真实存在，VALID UNTIL ≈ now+120s
let line = roleLine(u1);
const [, vu] = line.split('|');
const vuEpoch = pgTs(vu);
check('role exists in pg_roles', line.startsWith(u1 + '|'), line);
check('VALID UNTIL ≈ +120s ±10s', Math.abs(vuEpoch - Math.floor(Date.now() / 1000) - 120) <= 10, `vu=${vu}`);

// 4. 用 lease 账号真实登录（密码认证 + SELECT 权限）
let loginOk = false;
try {
  execFileSync('wsl', ['-d', 'podman-machine-default', '-e', 'podman', 'exec', '-e', `PGPASSWORD=${lease.data.password}`,
    CONTAINER, 'psql', '-h', '127.0.0.1', '-U', u1, '-d', 'mockdb', '-tAc', 'SELECT 1'], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
  loginOk = true;
} catch { loginOk = false; }
check('lease account can login & select', loginOk);

// 5. 续期：VALID UNTIL 延长
const renew = await call('POST', `/api/v1/projects/${pid}/dynamic-leases/${lease.data.id}/renew`, {
  token: tok.data.access_token, body: { ttl: 600 },
});
line = roleLine(u1);
const vu2 = pgTs(line.split('|')[1]);
check('renew extends VALID UNTIL', renew.status === 200 && vu2 > vuEpoch, `vu2=${line.split('|')[1]}`);

// 6. 手动回收：角色从 pg_roles 消失
const rev = await call('POST', `/api/v1/projects/${pid}/dynamic-leases/${lease.data.id}/revoke`, { token });
check('manual revoke 200', rev.status === 200 && rev.data.status === 'revoked', JSON.stringify(rev.data));
check('role dropped from pg_roles', roleGone(u1));

// 7. 短 TTL lease → worker 到期回收（pg_roles 轮询至多 60s）
const lease2 = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, {
  token: tok.data.access_token, body: { ttl: 2 },
});
const u2 = lease2.data.username;
let gone = false;
for (let i = 0; i < 12 && !gone; i++) { await sleep(5000); gone = roleGone(u2); }
check('worker expired lease → role dropped', gone && lease2.status === 201);

// 8. 身份吊销联动回收（pg_roles 侧确认）
const lease3 = await call('POST', `/api/v1/projects/${pid}/dynamic-engines/${eng.data.id}/lease`, {
  token: tok.data.access_token, body: { ttl: 3600 },
});
const u3 = lease3.data.username;
await call('POST', `/api/v1/orgs/${orgSlug}/identities/${ident.data.id}/revoke`, { token });
gone = false;
for (let i = 0; i < 12 && !gone; i++) { await sleep(5000); gone = roleGone(u3); }
check('identity revoke sweeps lease → role dropped', gone && lease3.status === 201);

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL REAL-PG CHECKS PASSED');
process.exit(failures ? 1 : 0);
