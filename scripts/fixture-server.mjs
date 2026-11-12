#!/usr/bin/env node
// 合成夹具 API —— 仅用于本地视觉验收（截图/交互原型），不属于产品代码。
// 完整实现前端会调用的每个端点，返回结构与 api/openapi.yaml 一致；
// 数据全部为合成数据，不含任何真实密钥。
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '../..');
const DIST = process.env.DIST_DIR || path.join(ROOT, 'apps/web/dist');
const PORT = Number(process.env.PORT || 7301);
const MODE = process.env.FIXTURE_MODE || 'new'; // new | old | login | cli

const now = () => Math.floor(Date.now() / 1000);
const iso = (offsetSec = 0) => new Date(Date.now() + offsetSec * 1000).toISOString();
const stamp = (offsetSec = 0) => iso(offsetSec).slice(0, 19).replace('T', ' ');

const login = MODE === 'login';

const user = { id: 'usr_7f3a91c2', email: 'mei@taboo.dev', name: '陈梅', kind: 'user', handle: 'mei', handle_confirmed: true };
const orgs = [
  { id: 'org_personal', name: '陈梅', slug: 'mei', kind: 'personal', role: 'owner' },
  { id: 'org_tide', name: '潮汐工作室', slug: 'tide', kind: 'organization', role: 'owner' },
];
const projects = [
  { id: 'prj_core', name: 'core-api', slug: 'core-api', created_at: stamp(-86400 * 200), approval_required: false },
  { id: 'prj_web', name: 'web-console', slug: 'web-console', created_at: stamp(-86400 * 120), approval_required: false },
  { id: 'prj_pay', name: 'billing', slug: 'billing', created_at: stamp(-86400 * 60), approval_required: true },
];
const folders = [
  { id: 'f_root', parent_id: '', name: 'core-api', path: '/' },
  { id: 'f_db', parent_id: 'f_root', name: 'db', path: '/db/' },
  { id: 'f_db_pg', parent_id: 'f_db', name: 'postgres', path: '/db/postgres/' },
  { id: 'f_redis', parent_id: 'f_db', name: 'redis', path: '/db/redis/' },
  { id: 'f_cloud', parent_id: 'f_root', name: 'cloud', path: '/cloud/' },
  { id: 'f_oauth', parent_id: 'f_cloud', name: 'oauth', path: '/cloud/oauth/' },
];

const long = (n) => '•'.repeat(0).concat('x'.repeat(n)); // eslint-disable-line
const secrets = [
  { id: 's1', key: 'DATABASE_URL', folder: '/db/postgres/', value: 'postgres://app:9f2c…@db.internal:5432/core', len: 58, tags: ['db', 'prod'], version: 14, updated_at: stamp(-2600), comment: '主库只读连接' },
  { id: 's2', key: 'STRIPE_SECRET_KEY', folder: '/', value: 'sk_live_51N…', len: 32, tags: ['billing'], version: 3, updated_at: stamp(-86400 * 2), comment: '计费回调签名' },
  { id: 's3', key: 'JWT_SIGNING_SECRET', folder: '/', value: `${'k7Qd9fLx'.repeat(8)}`, len: 64, tags: ['auth', 'rotate-30d'], version: 9, updated_at: stamp(-86400 * 9), comment: '轮换策略：30 天' },
  { id: 's4', key: 'REDIS_URL', folder: '/db/redis/', value: 'redis://cache.internal:6379/0', len: 26, tags: ['db'], version: 2, updated_at: stamp(-86400 * 21), comment: '' },
  { id: 's5', key: 'GITHUB_APP_PRIVATE_KEY', folder: '/cloud/oauth/', value: '-----BEGIN RSA…', len: 1624, tags: ['ci'], version: 1, updated_at: stamp(-86400 * 31), comment: '' },
  { id: 's6', key: 'SMTP_PASSWORD', folder: '/', value: 'mT4p!w9', len: 8, tags: [], version: 5, updated_at: stamp(-86400 * 4), comment: '' },
  { id: 's7', key: 'SENTRY_DSN', folder: '/', value: 'https://abc@o1.ingest.sentry.io/42', len: 38, tags: ['ops'], version: 1, updated_at: stamp(-86400 * 44), comment: '' },
  { id: 's8', key: 'UPSTREAM_API', folder: '/cloud/', value: '${DATABASE_URL}/${project:core-api:REDIS_URL}', len: 21, tags: ['ref'], version: 2, updated_at: stamp(-86400 * 6), comment: '引用其它密钥' },
];

const permissions = { write: true, reveal: true, export: true, rollback: true, delete: true };

const identities = [
  { id: 'id_ci', name: 'ci-runner', client_id: 'taboo_9c1f2e7a4b8d', status: 'active', token_ttl: 900, created_at: stamp(-86400 * 90), allowed_cidrs: ['10.0.0.0/8'], list_scan_reports: true, scopes: [{ project_id: 'prj_core', project_slug: 'core-api', permission: 'read' }, { project_id: 'prj_core', project_slug: 'core-api', permission: 'reveal' }] },
  { id: 'id_agent', name: 'claude-agent', client_id: 'taboo_1ab77cd09e42', status: 'active', token_ttl: 1800, created_at: stamp(-86400 * 34), allowed_cidrs: [], list_scan_reports: false, scopes: [{ project_id: 'prj_web', project_slug: 'web-console', permission: 'read' }] },
  { id: 'id_old', name: 'jenkins-legacy', client_id: 'taboo_ee31a0c7bb19', status: 'revoked', token_ttl: 900, created_at: stamp(-86400 * 260), allowed_cidrs: ['192.0.2.0/24'], list_scan_reports: false, scopes: [{ project_id: 'prj_web', project_slug: 'web-console', permission: 'write' }] },
];

const engines = [
  { id: 'eng_pg', name: 'pg-main', type: 'postgres', database: 'core', default_ttl: 3600, max_ttl: 86400, created_at: stamp(-86400 * 70), leases: [
    { id: 'lease_1', username: 'taboo_a1b2c3d4e5f6', status: 'active', expires_at: now() + 2740, seconds_left: 2740, created_at: stamp(-860) },
    { id: 'lease_2', username: 'taboo_9f8e7d6c5b4a', status: 'active', expires_at: now() + 480, seconds_left: 480, created_at: stamp(-3120) },
    { id: 'lease_3', username: 'taboo_0011aabbccdd', status: 'revoked', expires_at: now() - 600, seconds_left: 0, created_at: stamp(-4200) },
  ] },
  { id: 'eng_my', name: 'mysql-analytics', type: 'mysql', database: 'analytics', default_ttl: 1800, max_ttl: 7200, created_at: stamp(-86400 * 20), leases: [] },
];

const auditActions = ['secrets.reveal', 'secrets.write', 'secrets.read', 'secrets.rollback', 'secrets.delete', 'auth.login', 'identity.token', 'export.csv', 'member.grant', 'approval.approve'];
const actors = ['陈梅', '周远', 'ci-runner', 'claude-agent', 'identity:ci-runner'];
const resources = ['core-api/DATABASE_URL', 'core-api/JWT_SIGNING_SECRET', 'web-console/VITE_API_BASE', 'user/mei@taboo.dev', 'org/tide', 'billing/STRIPE_SECRET_KEY'];
const logs = Array.from({ length: 60 }, (_, i) => {
  const action = auditActions[i % auditActions.length];
  return {
    id: `log_${i}`,
    actor_name: actors[i % actors.length],
    action,
    resource: resources[(i * 7) % resources.length],
    metadata: '{}',
    ip: i % 3 === 0 ? '203.0.113.42' : '10.20.0.' + (10 + (i % 40)),
    created_at: stamp(-i * 1450 - 120),
  };
});

const members = [
  { user_id: 'usr_7f3a91c2', email: 'mei@taboo.dev', name: '陈梅', role: 'owner', restricted: false, grants: [] },
  { user_id: 'usr_2', email: 'zhou@taboo.dev', name: '周远', role: 'admin', restricted: false, grants: [] },
  { user_id: 'usr_3', email: 'lin@taboo.dev', name: '林澈', role: 'developer', restricted: true, grants: [{ project_id: 'prj_core', project_slug: 'core-api', project_name: 'core-api', role: 'developer' }] },
  { user_id: 'usr_4', email: 'yu@taboo.dev', name: '于悦', role: 'viewer', restricted: true, grants: [{ project_id: 'prj_pay', project_slug: 'billing', project_name: 'billing', role: 'viewer' }] },
];

const providers = [
  { id: 'oidc_1', name: '公司 Keycloak', issuer: 'https://sso.tide.dev/realms/main', client_id: 'taboo', scopes: 'openid,email,profile', role_claim: 'groups', role_map: { admins: 'admin' }, default_role: 'viewer', public_login: true, autocreate: false, sync_role: true, enabled: true, created_at: stamp(-86400 * 140), login_url: '/api/v1/auth/oidc/tide/login?pid=oidc_1' },
  { id: 'oidc_2', name: 'GitHub 组织', issuer: 'https://token.actions.githubusercontent.com', client_id: 'gh-actions', scopes: 'openid', role_claim: 'groups', role_map: {}, default_role: 'developer', public_login: false, autocreate: false, sync_role: false, enabled: true, created_at: stamp(-86400 * 40), login_url: '/api/v1/auth/oidc/tide/login?pid=oidc_2' },
];

const syncTargets = [
  { id: 'sync_1', name: 'github · tide/core-api', platform: 'github', project_id: 'prj_core', enabled: true, created_at: stamp(-86400 * 60), config_keys: ['token', 'owner', 'repo'] },
  { id: 'sync_2', name: 'vercel · web-console', platform: 'vercel', project_id: 'prj_web', enabled: true, created_at: stamp(-86400 * 30), config_keys: ['token', 'project_id', 'targets'] },
  { id: 'sync_3', name: 'cloudflare · edge', platform: 'cloudflare', project_id: '', enabled: false, created_at: stamp(-86400 * 12), config_keys: ['token', 'account_id'] },
];
const syncRuns = Array.from({ length: 12 }, (_, i) => ({
  id: `run_${i}`, target_id: 'sync_1', secret_key: secrets[i % secrets.length].key, action: 'upsert',
  fingerprint: Math.random().toString(16).slice(2, 18), status: i % 5 === 3 ? 'failed' : i % 7 === 6 ? 'dead' : 'success',
  trigger_type: i % 4 === 0 ? 'manual' : 'event', attempts: i % 5 === 3 ? 3 : 1, error: i % 5 === 3 ? 'upstream 502 Bad Gateway' : '', created_at: stamp(-i * 900), updated_at: stamp(-i * 900 + 12),
}));

const webhooks = [
  { id: 'wh_1', url: 'https://hooks.slack.com/services/T00/B00/x', events: ['secret.created', 'secret.updated'], active: true, created_at: stamp(-86400 * 45) },
  { id: 'wh_2', url: 'https://ops.tide.dev/taboo/audit', events: ['secret.reveal', 'secret.deleted', 'rotation.due'], active: true, created_at: stamp(-86400 * 18) },
];

const scanReports = [
  { id: 'sr_1', source: 'cli@mac-studio', actor_name: '陈梅', finding_count: 7, critical_count: 2, warning_count: 5, created_at: stamp(-3600) },
  { id: 'sr_2', source: 'github-actions/core-api', actor_name: 'ci-runner', finding_count: 0, critical_count: 0, warning_count: 0, created_at: stamp(-86400 * 2) },
];
const scanFindings = [
  { rule_id: 'aws-access-key', level: 'critical', file: 'scripts/seed.ts', line: 42, fingerprint: 'a9f3'.repeat(16), alg: 'hmac-sha256' },
  { rule_id: 'stripe-live-key', level: 'critical', file: 'src/billing/client.ts', line: 17, fingerprint: 'b1c2'.repeat(16), alg: 'hmac-sha256' },
  { rule_id: 'generic-password', level: 'warning', file: 'docker-compose.yml', line: 9, fingerprint: 'c3d4'.repeat(16), alg: 'hmac-sha256' },
  { rule_id: 'generic-password', level: 'warning', file: 'docker-compose.yml', line: 14, fingerprint: 'd4e5'.repeat(16), alg: 'hmac-sha256' },
  { rule_id: 'private-key', level: 'warning', file: 'test/fixtures/key.pem', line: 1, fingerprint: 'e5f6'.repeat(16), alg: 'sha256' },
];

const approvals = [
  { id: 'ap_1', key: 'STRIPE_SECRET_KEY', action: 'write', status: 'pending', requester_id: 'usr_3', reason: '轮换计费回调密钥，旧 key 已在 Stripe 后台删除', expires_at: now() + 3600 },
  { id: 'ap_2', key: 'DATABASE_URL', action: 'rollback', status: 'pending', requester_id: 'usr_2', reason: '回滚到 v12，v13 的连接串指向了正在迁移的副本', expires_at: now() + 7200 },
  { id: 'ap_3', key: 'JWT_SIGNING_SECRET', action: 'rollback', status: 'approved', requester_id: 'usr_3', reason: '轮换窗口提前', expires_at: now() + 1800 },
];
const accessRequests = [
  { id: 'ar_1', project_id: 'prj_pay', action: 'reveal', status: 'pending', requester_id: 'usr_4', reason: '对账工单 OPS-1183，需要核对支付网关 key 指纹', ttl_seconds: 3600, expires_at: now() + 3600 },
  { id: 'ar_2', project_id: 'prj_core', action: 'export', status: 'approved', requester_id: 'usr_3', reason: '把 dev 层导出到本地 .env', ttl_seconds: 1800, expires_at: now() + 900 },
];
const scimTokens = [{ id: 'scim_1', name: 'Okta provisioning', revoked: false, expires_at: now() + 86400 * 60 }];

const trusts = [
  { id: 'tr_1', name: 'github-actions OIDC', issuer: 'https://token.actions.githubusercontent.com', audience: 'https://taboo.internal', metadata_mode: 'oidc', token_profile: 'JWT_RFC9068', client_id: '', max_lifetime: 900, enabled: true, revocation_profile: 'BOUNDED' },
];
const workloads = [
  { id: 'wl_1', name: 'core-api deploy', trust_id: 'tr_1', subject: 'repo:tide/core-api:ref:refs/heads/main', client_id: 'gh-deploy', enabled: true, grants: [{ project_id: 'prj_core', action: 'read', required_scope: 'secrets:read' }, { project_id: 'prj_core', action: 'reveal', required_scope: 'secrets:reveal' }] },
];
const memberGrants = [{ project_id: 'prj_core', action: 'read' }, { project_id: 'prj_core', action: 'reveal' }];

const send = (res, status, body, headers = {}) => {
  const isBuf = Buffer.isBuffer(body) || typeof body === 'string';
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store', ...headers });
  res.end(isBuf ? body : JSON.stringify(body));
};
const unauthorized = (res) => send(res, 401, { code: 'UNAUTHORIZED', message: '需要登录' });

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, `http://${req.headers.host}`);
  const p = url.pathname;
  let body = {};
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    try { body = JSON.parse(Buffer.concat(chunks).toString() || '{}'); } catch { body = {}; }
  }
  const m = req.method;

  // ---------- 静态资源 ----------
  if (!p.startsWith('/api/')) {
    let file = path.join(DIST, p === '/' ? 'index.html' : p);
    if (!fs.existsSync(file)) file = path.join(DIST, 'index.html');
    if (!fs.existsSync(file)) return send(res, 404, 'build first');
    const ext = path.extname(file);
    const type = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2', '.json': 'application/json' }[ext] || 'application/octet-stream';
    res.writeHead(200, { 'Content-Type': type });
    return res.end(fs.readFileSync(file));
  }

  // ---------- 会话 ----------
  if (p === '/api/v1/auth/session') {
    if (MODE === 'login') return send(res, 404, { code: 'NOT_FOUND', message: 'legacy' });
    if (MODE === 'cli') return send(res, 404, { code: 'NOT_FOUND', message: 'legacy' });
    return send(res, 200, {
      authenticated: true, authentication: 'oidc', csrf_token: 'fixture-csrf',
      revocation_profile: 'BOUNDED', sensitive_operations_allowed: true,
      expires_at: now() + 420, auth_valid_until: now() + 420,
      reauthentication_url: '/api/v1/auth/oidc/tide/login', rp_logout: true,
    });
  }
  if (p === '/api/v1/auth/logout') { res.writeHead(204); return res.end(); }
  if (p === '/api/v1/auth/sources') return send(res, 200, { register_enabled: true, password_enabled: true, github_enabled: true, sources: [] });
  if (p === '/api/v1/healthz') return send(res, 200, { version: '1.4.2', commit: 'f34815c', build_time: iso() });
  if (p === '/api/v1/readyz') return send(res, 200, { ready: true });
  if (p === '/api/v1/auth/login' || p === '/api/v1/auth/register') {
    return send(res, 200, { user, tokens: { access: 'fixture-access', refresh: 'fixture-refresh' } });
  }
  if (p === '/api/v1/auth/totp/login') return send(res, 200, { user, tokens: { access: 'fixture-access', refresh: 'fixture-refresh' } });

  if (MODE === 'login') {
    if (!req.headers.authorization && p !== '/api/v1/access' && p !== '/api/v1/auth/sources') return unauthorized(res);
    if (p === '/api/v1/access' && m === 'GET') return send(res, 200, { items: [] });
    if (p === '/api/v1/access' && m === 'POST') return send(res, 200, { status: 'approved' });
  }

  if (p === '/api/v1/auth/organizations') return send(res, 200, { organizations: [
    { id: 'org_personal', name: '陈梅', slug: 'mei', kind: 'personal', role: 'owner', current: true },
    { id: 'org_tide', name: '潮汐工作室', slug: 'tide', kind: 'organization', role: 'owner', current: false, login_url: '/api/v1/auth/oidc/tide/login' },
  ] });
  if (p === '/api/v1/me') {
    if (m === 'GET') return send(res, 200, { user, orgs, totp_enabled: false });
  }
  if (p === '/api/v1/access' && m === 'POST') return send(res, 200, { status: 'approved' });
  if (p === '/api/v1/orgs' && m === 'POST') return send(res, 201, { id: 'org_new' });

  let g;
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/projects$/))) {
    if (m === 'GET') return send(res, 200, { projects });
    return send(res, 201, { id: 'prj_new', name: body.name, slug: body.code });
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)$/)) && m === 'PATCH') return send(res, 200, {});

  // 文件夹
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/folders$/))) {
    if (m === 'GET') return send(res, 200, { folders });
    if (m === 'POST') return send(res, 201, { id: 'f_new', path: body.path });
    if (m === 'DELETE') return send(res, 200, {});
  }
  // 密钥
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets$/))) {
    if (m === 'GET') {
      const f = url.searchParams.get('path') || '/';
      const list = secrets.filter((s) => s.folder === f).map((s) => ({ ...s, value: undefined, canReveal: true, canWrite: true, canDelete: true, canRollback: true }));
      return send(res, 200, { secrets: list, permissions });
    }
    if (m === 'POST') {
      const folder = url.searchParams.get('path') || '/';
      const existing = secrets.find((s) => s.key === body.key && s.folder === folder);
      if (existing) { existing.version += 1; existing.updated_at = stamp(); return send(res, 200, { version: existing.version }); }
      const made = { id: 's_new', key: body.key, folder, value: body.value, len: String(body.value || '').length, tags: body.tags || [], version: 1, updated_at: stamp(), comment: body.comment || '' };
      secrets.push(made);
      return send(res, 201, { version: 1 });
    }
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets\/([^/]+)$/))) {
    const key = decodeURIComponent(g[2]);
    const s = secrets.find((x) => x.key === key);
    if (!s) return send(res, 404, { code: 'NOT_FOUND', message: 'no such secret' });
    if (m === 'GET') return send(res, 200, { ...s, canReveal: true, canWrite: true, canDelete: true, canRollback: true });
    if (m === 'DELETE') { const i = secrets.indexOf(s); secrets.splice(i, 1); return send(res, 200, {}); }
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets\/([^/]+)\/versions$/))) {
    const s = secrets.find((x) => x.key === decodeURIComponent(g[2]));
    const versions = Array.from({ length: Math.min(6, s?.version ?? 3) }, (_, i) => ({
      version: (s?.version ?? 3) - i, created_by: ['usr_7f3a91c2ffee', 'usr_2aa', 'ci-runner'][i % 3], created_at: stamp(-86400 * i - 600),
    }));
    return send(res, 200, { versions, latest: s?.version ?? 3, canRollback: true });
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets\/([^/]+)\/versions\/(\d+)$/))) {
    return send(res, 200, { key: decodeURIComponent(g[2]), version: Number(g[3]), value: '•••• masked ••••' });
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets\/([^/]+)\/rollback$/))) return send(res, 200, { version: 15 });
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/secrets\/([^/]+)\/rotation$/))) {
    if (m === 'GET') return send(res, 200, { id: 'rot_1', key: decodeURIComponent(g[2]), interval_seconds: 2592000, cron: '', enabled: true, next_run_at: now() + 86400 * 21, last_run_at: now() - 86400 * 9, last_event_id: 'evt_9' });
    if (m === 'PUT') return send(res, 200, { id: 'rot_1', key: decodeURIComponent(g[2]), interval_seconds: body.interval_seconds ?? 2592000, cron: body.cron ?? '', enabled: body.enabled ?? true, next_run_at: now() + 86400 * 30, last_run_at: null, last_event_id: null });
    if (m === 'DELETE') return send(res, 200, {});
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/export$/))) return send(res, 200, { content: 'DATABASE_URL=postgres://…\nJWT_SIGNING_SECRET=…\n' });
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/import$/))) {
    const entries = (body.content || '').split('\n').filter((l) => l.includes('=')).map((l, i) => ({ key: l.split('=')[0], action: i % 3 === 2 ? 'skip' : i % 3 === 1 ? 'overwrite' : 'create', version: 1 }));
    if (body.preview) return send(res, 200, { preview: true, strategy: body.strategy || 'skip', created: entries.filter((e) => e.action === 'create').length, updated: entries.filter((e) => e.action === 'overwrite').length, skipped: entries.filter((e) => e.action === 'skip').length, conflicts: [], entries });
    return send(res, 200, { preview: false, created: entries.length, updated: 0, skipped: 0, conflicts: [], entries });
  }
  // 审计
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit$/))) return send(res, 200, { logs });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit\/verify$/))) return send(res, 200, { ok: true, checked: logs.length });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit\/exports$/))) return send(res, 200, { exports: [{ id: 'exp_1', format: 'csv', status: 'done', row_count: 12840, created_at: stamp(-5400), completed_at: stamp(-5300) }, { id: 'exp_2', format: 'jsonl', status: 'pending', created_at: stamp(-60) }] });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit\/export$/))) return send(res, 200, { id: 'exp_3', status: 'pending' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit\/exports\/([^/]+)\/retry$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/audit\/exports\/([^/]+)$/))) return send(res, 200, 'id,action\n1,secrets.reveal\n', { 'Content-Type': 'text/csv' });
  // 机器身份
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/identities$/))) {
    if (m === 'GET') return send(res, 200, { identities });
    return send(res, 201, { ...identities[0], id: 'id_new', name: body.name, client_id: 'taboo_' + Math.random().toString(16).slice(2, 14), client_secret: 'tbs_' + 'k9Qd'.repeat(10) });
  }
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/identities\/([^/]+)\/revoke$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/identities\/([^/]+)$/)) && m === 'PATCH') return send(res, 200, {});
  // 动态密钥
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/dynamic-engines$/))) {
    if (m === 'GET') return send(res, 200, { engines: engines.map((e) => ({ ...e, leases: e.leases.map((l) => ({ ...l, seconds_left: l.status === 'active' ? Math.max(0, l.expires_at - now()) : 0 })) })) });
    return send(res, 201, { id: 'eng_new' });
  }
  if ((g = p.match(/^\/api\/v1\/projects\/([^/]+)\/dynamic-leases\/([^/]+)\/revoke$/))) return send(res, 200, {});
  // 成员
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/members$/))) return send(res, 200, { members });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/members\/([^/]+)$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/members\/([^/]+)\/grants$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/members\/([^/]+)\/actions$/))) return send(res, 200, { grants: memberGrants });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/invites$/))) return send(res, 201, { url: `http://127.0.0.1:${PORT}/invite/fx_2f9a1c7e` });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/security$/))) return send(res, 200, { login_policy: 'LOCAL_OR_EXTERNAL', external_secret_permissions: 'EXPLICIT_ACTIONS' });
  // OIDC
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/oidc$/))) {
    if (m === 'GET') return send(res, 200, { providers });
    return send(res, 201, { id: 'oidc_new' });
  }
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/oidc\/([^/]+)\/security$/))) return send(res, 200, { profile: 'BOUNDED', session_ttl: 900, max_auth_age: 300, required_acr: [], required_amr: ['pwd'], online_audience: '', rp_logout: false });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/oidc\/([^/]+)$/))) return send(res, 200, {});
  // Sync / Webhooks
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/sync\/targets$/))) return m === 'GET' ? send(res, 200, { targets: syncTargets }) : send(res, 201, { id: 'sync_new' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/sync\/targets\/([^/]+)\/retry$/))) return send(res, 200, { enqueued: 8 });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/sync\/targets\/([^/]+)$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/sync\/runs$/))) return send(res, 200, { runs: syncRuns });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/webhooks$/))) return m === 'GET' ? send(res, 200, { webhooks }) : send(res, 201, { ...webhooks[0], id: 'wh_new', secret: 'whsec_' + 'a1'.repeat(16) });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/webhooks\/([^/]+)\/deliveries$/))) return send(res, 200, { deliveries: Array.from({ length: 6 }, (_, i) => ({ id: `d${i}`, status: i === 2 ? 'failed' : 'delivered', response_status: i === 2 ? 502 : 200, attempts: i === 2 ? 3 : 1, created_at: stamp(-i * 700) })) });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/webhooks\/([^/]+)$/))) return send(res, 200, {});
  // 扫描报告
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/scan-reports$/))) return send(res, 200, { reports: scanReports, can_delete: true });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/scan-reports\/([^/]+)$/))) {
    if (m === 'DELETE') return send(res, 200, {});
    return send(res, 200, { ...scanReports[0], findings: scanFindings });
  }
  // 审批 / 限时访问 / SCIM
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/approvals$/))) return send(res, 200, { approvals });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/access-requests$/))) return m === 'GET' ? send(res, 200, { requests: accessRequests }) : send(res, 201, { id: 'ar_new' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/(approvals|access-requests)\/([^/]+)\/(approve|reject|cancel|revoke)$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/scim\/tokens$/))) return m === 'GET' ? send(res, 200, { tokens: scimTokens }) : send(res, 201, { token: 'scim_' + 'b7'.repeat(16), id: 'scim_new' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/scim\/tokens\/([^/]+)$/))) return send(res, 200, {});
  // OAuth 信任 / 工作负载
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/oauth-trusts$/))) return m === 'GET' ? send(res, 200, { trusts }) : send(res, 201, { id: 'tr_new' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/oauth-trusts\/([^/]+)$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/workloads$/))) return m === 'GET' ? send(res, 200, { workloads }) : send(res, 201, { id: 'wl_new' });
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/workloads\/([^/]+)\/(grants|revoke)$/))) return send(res, 200, {});
  if ((g = p.match(/^\/api\/v1\/orgs\/([^/]+)\/workloads\/([^/]+)$/))) return send(res, 200, {});
  // TOTP
  if (p === '/api/v1/auth/totp/setup') return send(res, 200, { secret: 'JBSWY3DPEHPK3PXP', otpauth_url: 'otpauth://totp/taboo', qr_png: QR });
  if (p === '/api/v1/auth/totp/verify') return send(res, 200, { enabled: true, recovery_codes: ['8q2f-1mzx-7kd4', 'p0we-9vbr-3hn6', 'y5ta-2juc-8lq1', 'd6gh-4szm-0kr7', 'n1vb-7xyd-5tf3', 'u4kr-0qwe-9mz8', 'c9aj-3lph-6vx2', 'r2sd-8nft-1zb5', 'k7ml-5cyr-4qw9', 'f3bn-6hza-2kj4'].map((c) => c) });
  if (p === '/api/v1/auth/totp/disable') return send(res, 200, { enabled: false });
  if ((g = p.match(/^\/api\/v1\/me\/handle/))) return send(res, 200, {});
  if (p === '/api/v1/auth/github/session') return send(res, 200, { user, tokens: { access: 'a', refresh: 'r' } });

  return send(res, 404, { code: 'NOT_FOUND', message: `${m} ${p}` });
});

// 1x1 PNG（二维码占位，避免依赖图片）
const QR = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

server.listen(PORT, '127.0.0.1', () => console.log(`[fixture] ${MODE} on http://127.0.0.1:${PORT} (dist: ${DIST})`));
