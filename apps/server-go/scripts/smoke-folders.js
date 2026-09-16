// 多级文件夹（M2 #5）端到端验证 —— 针对 Go 后端
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

const email = `folder-smoke-${Date.now()}@taboo.dev`;
console.log('== taboo folder smoke ==');
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password: 'password123' } });
const token = reg.data.tokens.access;
const me = await call('GET', '/api/v1/me', { token });
const orgSlug = me.data.orgs[0].slug;
const projects = await call('GET', `/api/v1/orgs/${orgSlug}/projects`, { token });
const pid = projects.data.projects[0].id;

// 1. 默认根文件夹存在
const root = await call('GET', `/api/v1/projects/${pid}/folders?env=dev`, { token });
check('root folder exists', root.status === 200 && root.data.folders.some((f) => f.path === '/'), JSON.stringify(root.data));

// 2. 创建多级文件夹（自动补中间节点）
const created = await call('POST', `/api/v1/projects/${pid}/folders?env=dev`, { token, body: { path: '/db/mysql/' } });
check('create nested 201', created.status === 201 && created.data.path === '/db/mysql/', JSON.stringify(created.data));
const after = await call('GET', `/api/v1/projects/${pid}/folders?env=dev`, { token });
check('intermediate /db/ auto-created', after.data.folders.some((f) => f.path === '/db/'));
check('nested /db/mysql/ exists', after.data.folders.some((f) => f.path === '/db/mysql/'));
const parentOf = after.data.folders.find((f) => f.path === '/db/mysql/').parent_id;
check('parent linkage correct', after.data.folders.find((f) => f.id === parentOf)?.path === '/db/');

// 3. 文件夹内密钥操作
const set1 = await call('POST', `/api/v1/projects/${pid}/secrets?env=dev&path=/db/mysql/`, { token, body: { key: 'DB_HOST', value: 'mysql.internal' } });
check('set secret in folder', set1.status === 200 && set1.data.version === 1);
const set2 = await call('POST', `/api/v1/projects/${pid}/secrets?env=dev&path=/db/`, { token, body: { key: 'DB_HOST', value: 'postgres.internal' } });
check('same key different folder OK', set2.status === 200);
const reveal = await call('GET', `/api/v1/projects/${pid}/secrets/DB_HOST?env=dev&path=/db/mysql/`, { token });
check('reveal from folder', reveal.data.value === 'mysql.internal');
const reveal2 = await call('GET', `/api/v1/projects/${pid}/secrets/DB_HOST?env=dev&path=/db/`, { token });
check('same key other folder value differs', reveal2.data.value === 'postgres.internal');

// 4. 列表按 path 过滤
const listRoot = await call('GET', `/api/v1/projects/${pid}/secrets?env=dev`, { token });
check('list all has folder paths', listRoot.data.secrets.length === 2 && listRoot.data.secrets.every((s) => s.folder));
const listDb = await call('GET', `/api/v1/projects/${pid}/secrets?env=dev&path=/db/`, { token });
check('list filtered by path', listDb.data.secrets.length === 1 && listDb.data.secrets[0].folder === '/db/');

// 5. 版本与回滚在文件夹内
const v2 = await call('POST', `/api/v1/projects/${pid}/secrets?env=dev&path=/db/mysql/`, { token, body: { key: 'DB_HOST', value: 'mysql.v2' } });
check('update in folder v2', v2.data.version === 2);
const rb = await call('POST', `/api/v1/projects/${pid}/secrets/DB_HOST/rollback?env=dev&path=/db/mysql/`, { token, body: { version: 1 } });
check('rollback in folder v3', rb.data.version === 3);

// 6. 移动级联：/db/ → /database/，子孙路径与密钥路径同步
const mv = await call('POST', `/api/v1/projects/${pid}/folders/move?env=dev`, { token, body: { from: '/db/', to: '/database/' } });
check('move 200', mv.status === 200, JSON.stringify(mv.data));
const afterMove = await call('GET', `/api/v1/projects/${pid}/folders?env=dev`, { token });
check('children cascaded', afterMove.data.folders.some((f) => f.path === '/database/mysql/'));
const revealMoved = await call('GET', `/api/v1/projects/${pid}/secrets/DB_HOST?env=dev&path=/database/mysql/`, { token });
check('secret reachable at moved path', revealMoved.status === 200 && revealMoved.data.value === 'mysql.internal');
const oldGone = await call('GET', `/api/v1/projects/${pid}/secrets/DB_HOST?env=dev&path=/db/mysql/`, { token });
check('old path 404', oldGone.status === 404);
const movedFolderField = await call('GET', `/api/v1/projects/${pid}/secrets?env=dev&path=/database/mysql/`, { token });
check('folder field synced', movedFolderField.data.secrets[0]?.folder === '/database/mysql/');

// 7. 非空删除拒绝 / 空删除成功
const delNonEmpty = await call('DELETE', `/api/v1/projects/${pid}/folders?env=dev&path=/database/`, { token });
check('delete non-empty 400', delNonEmpty.status === 400, JSON.stringify(delNonEmpty.data));
const tmp = await call('POST', `/api/v1/projects/${pid}/folders?env=dev`, { token, body: { path: '/tmp/' } });
const delLeaf = await call('DELETE', `/api/v1/projects/${pid}/folders?env=dev&path=/tmp/`, { token });
check('create+delete empty folder', tmp.status === 201 && delLeaf.status === 200, `create=${tmp.status} delete=${delLeaf.status}`);

// 8. 根目录保护 + 非法路径
const delRoot = await call('DELETE', `/api/v1/projects/${pid}/folders?env=dev&path=/`, { token });
check('delete root 400', delRoot.status === 400);
const bad = await call('POST', `/api/v1/projects/${pid}/folders?env=dev`, { token, body: { path: '/a/../b/' } });
check('path traversal rejected', bad.status === 400);

// 9. 权限：未认证 401（viewer/developer 粒度由 RBAC 中间件统一控制）
const noauth = await call('POST', `/api/v1/projects/${pid}/folders?env=dev`, { body: { path: '/x/' } });
check('unauth folder create 401', noauth.status === 401);

// 10. 文件夹操作落审计
const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=200`, { token });
const acts = new Set(audit.data.logs.map((l) => l.action));
check('audit has folders.create/move/delete', ['folders.create', 'folders.move', 'folders.delete'].every((a) => acts.has(a)), [...acts].join(','));

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL FOLDER CHECKS PASSED');
process.exit(failures ? 1 : 0);
