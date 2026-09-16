// 冒烟测试：register → login → create secret → list(无值) → reveal → versions → rollback → export → audit
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

const email = `smoke-${Date.now()}@taboo.dev`;
const password = 'smoke-pass-123';

console.log('== taboo smoke test ==');
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password, name: 'Smoke' } });
check('register 201', reg.status === 201, JSON.stringify(reg.data));
const token = reg.data?.tokens?.access;

const me = await call('GET', '/api/v1/me', { token });
check('me returns orgs', Array.isArray(me.data?.orgs) && me.data.orgs.length >= 1);
const orgSlug = me.data?.orgs?.[0]?.slug;
const orgs = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const projectId = orgs.data?.projects?.[0]?.id;
check('default project exists', !!projectId);

const envs = await call('GET', `/api/v1/projects/${projectId}/environments`, { token });
check('dev/staging/prod envs', ['dev', 'staging', 'prod'].every((s) => envs.data?.environments?.some((e) => e.slug === s)));

const s1 = await call('POST', `/api/v1/projects/${projectId}/secrets`, { token, body: { key: 'DB_PASS', value: 's3cret-v1', comment: 'db password', tags: ['db'] } });
check('create secret v1', s1.data?.version === 1, JSON.stringify(s1.data));
const s2 = await call('POST', `/api/v1/projects/${projectId}/secrets`, { token, body: { key: 'DB_PASS', value: 's3cret-v2' } });
check('update secret v2', s2.data?.version === 2, JSON.stringify(s2.data));

const list = await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`, { token });
check('list has no values', list.data?.secrets?.[0]?.value === undefined && list.data?.secrets?.length === 1);

const reveal = await call('GET', `/api/v1/projects/${projectId}/secrets/DB_PASS?env=dev`, { token });
check('reveal returns v2 value', reveal.data?.value === 's3cret-v2', JSON.stringify(reveal.data));

const vers = await call('GET', `/api/v1/projects/${projectId}/secrets/DB_PASS/versions?env=dev`, { token });
check('versions has 2 entries', vers.data?.versions?.length === 2);

const rb = await call('POST', `/api/v1/projects/${projectId}/secrets/DB_PASS/rollback?env=dev`, { token, body: { version: 1 } });
check('rollback creates v3', rb.data?.version === 3, JSON.stringify(rb.data));
const reveal2 = await call('GET', `/api/v1/projects/${projectId}/secrets/DB_PASS?env=dev`, { token });
check('reveal after rollback = v1 value', reveal2.data?.value === 's3cret-v1');

const exp = await call('GET', `/api/v1/projects/${projectId}/export?env=dev`, { token, raw: true });
const expText = await exp.text();
check('export .env format', expText.includes('DB_PASS=s3cret-v1'), expText);

const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit`, { token });
const actions = new Set(audit.data?.logs?.map((l) => l.action));
check('audit has create/reveal/rollback/export', ['secrets.create', 'secrets.reveal', 'secrets.rollback', 'secrets.export'].every((a) => actions.has(a)), [...actions].join(','));

// 负向：未认证 / 越权环境
const noauth = await call('GET', `/api/v1/projects/${projectId}/secrets?env=dev`);
check('unauth rejected 401', noauth.status === 401);

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL CHECKS PASSED');
process.exit(failures ? 1 : 0);
