// REST API 路由 —— 对齐设计文档 §5（MVP 子集）
//   POST /api/v1/auth/register | /login | /refresh
//   GET  /api/v1/orgs
//   GET/POST /api/v1/orgs/:slug/projects
//   GET/POST /api/v1/projects/:pid/environments
//   GET    /api/v1/projects/:pid/secrets?env=dev            列表（无值）
//   GET    /api/v1/projects/:pid/secrets/:key?env=dev       取单值（reveal，记审计）
//   POST   /api/v1/projects/:pid/secrets                    创建/更新（产生新版本）
//   GET    /api/v1/projects/:pid/secrets/:key/versions      版本历史
//   POST   /api/v1/projects/:pid/secrets/:key/rollback      回滚到 vN
//   GET    /api/v1/projects/:pid/export?env=dev             导出 .env（记审计）
//   GET    /api/v1/orgs/:slug/audit                         审计查询
import { q, q1, run } from './db.js';
import { encrypt, decrypt, hashPassword, verifyPassword, getOrgDEK, generateDEK, sha256, randomId } from './crypto.js';
import { audit, can, issueTokens, membership } from './auth.js';

const json = (res, code, body) => {
  res.writeHead(code, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(body));
};
const bodyOf = (req) => new Promise((resolve) => {
  let data = '';
  req.on('data', (c) => { data += c; if (data.length > 1_000_000) req.destroy(); });
  req.on('end', () => { try { resolve(data ? JSON.parse(data) : {}); } catch { resolve({}); } });
});
const slugify = (s) => String(s).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'org';

export function makeApi({ db, masterKey, jwtSecret, requireAuth }) {
  return async function api(req, res, pathname, query) {
    const seg = pathname.split('/').filter(Boolean); // ['api','v1',...]

    // ---------- 认证（公开） ----------
    if (req.method === 'POST' && pathname === '/api/v1/auth/register') {
      const b = await bodyOf(req);
      const email = String(b.email || '').trim().toLowerCase();
      const password = String(b.password || '');
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email)) return json(res, 400, { code: 'INVALID_EMAIL', message: 'invalid email' });
      if (password.length < 8) return json(res, 400, { code: 'WEAK_PASSWORD', message: 'password must be at least 8 chars' });
      if (q1(db, `SELECT id FROM users WHERE email = ?`, email)) {
        return json(res, 409, { code: 'EMAIL_TAKEN', message: 'email already registered' });
      }
      const userId = randomId();
      run(db, `INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)`,
        userId, email, String(b.name || ''), hashPassword(password));
      // 注册即创建个人组织（DEK 经 Root Key 加密落库）
      const orgId = randomId();
      let slug = slugify(email.split('@')[0]);
      while (q1(db, `SELECT id FROM orgs WHERE slug = ?`, slug)) slug += '-' + randomId().slice(0, 4);
      run(db, `INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, ?, ?, ?)`,
        orgId, `${email}'s org`, slug, encrypt(masterKey, generateDEK().toString('hex')));
      run(db, `INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, 'owner')`, orgId, userId);
      // 默认项目 + dev/staging/prod 三环境
      const projectId = randomId();
      run(db, `INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'default', 'default')`, projectId, orgId);
      ['dev', 'staging', 'prod'].forEach((s, i) =>
        run(db, `INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`,
          randomId(), projectId, s, s, i));
      const user = q1(db, `SELECT id, email, name FROM users WHERE id = ?`, userId);
      audit(db, { orgId, actor: user, action: 'auth.register', resource: `user/${email}`, ip: req.socket.remoteAddress });
      return json(res, 201, { user, tokens: issueTokens(db, jwtSecret, user) });
    }

    if (req.method === 'POST' && pathname === '/api/v1/auth/login') {
      const b = await bodyOf(req);
      const email = String(b.email || '').trim().toLowerCase();
      const row = q1(db, `SELECT * FROM users WHERE email = ?`, email);
      if (!row || !verifyPassword(String(b.password || ''), row.password_hash)) {
        return json(res, 401, { code: 'BAD_CREDENTIALS', message: 'invalid email or password' });
      }
      const user = { id: row.id, email: row.email, name: row.name };
      const org = q1(db, `SELECT id, slug FROM orgs o JOIN org_members m ON m.org_id = o.id WHERE m.user_id = ? LIMIT 1`, row.id);
      if (org) audit(db, { orgId: org.id, actor: user, action: 'auth.login', resource: `user/${email}`, ip: req.socket.remoteAddress });
      return json(res, 200, { user, tokens: issueTokens(db, jwtSecret, user) });
    }

    if (req.method === 'POST' && pathname === '/api/v1/auth/refresh') {
      const b = await bodyOf(req);
      const row = q1(db, `SELECT * FROM refresh_tokens WHERE token_hash = ?`, sha256(String(b.refresh || '')));
      if (!row || row.expires_at < Date.now()) return json(res, 401, { code: 'INVALID_REFRESH', message: 'invalid refresh token' });
      run(db, `DELETE FROM refresh_tokens WHERE id = ?`, row.id); // 旋转
      const user = q1(db, `SELECT id, email, name FROM users WHERE id = ?`, row.user_id);
      if (!user) return json(res, 401, { code: 'INVALID_REFRESH', message: 'user gone' });
      return json(res, 200, { tokens: issueTokens(db, jwtSecret, user) });
    }

    // ---------- 以下全部需要认证 ----------
    const user = requireAuth(req, res);
    if (!user) return;
    const actor = { id: user.id, name: user.name || user.email };
    const ip = req.socket.remoteAddress || '';

    // GET /api/v1/me
    if (req.method === 'GET' && pathname === '/api/v1/me') {
      const orgs = q(db, `SELECT o.id, o.name, o.slug, m.role FROM orgs o JOIN org_members m ON m.org_id = o.id WHERE m.user_id = ?`, user.id);
      return json(res, 200, { user, orgs });
    }

    // GET /api/v1/orgs/:slug/projects | POST 创建
    if (seg[0] === 'api' && seg[1] === 'v1' && seg[2] === 'orgs' && seg[4] === 'projects') {
      const org = q1(db, `SELECT * FROM orgs WHERE slug = ?`, seg[3]);
      if (!org) return json(res, 404, { code: 'NOT_FOUND', message: 'org not found' });
      if (!membership(db, org.id, user.id)) return json(res, 403, { code: 'FORBIDDEN', message: 'not a member' });
      if (req.method === 'GET') {
        return json(res, 200, { projects: q(db, `SELECT id, name, slug, created_at FROM projects WHERE org_id = ? ORDER BY created_at`, org.id) });
      }
      if (req.method === 'POST') {
        const b = await bodyOf(req);
        const name = String(b.name || '').trim();
        if (!name) return json(res, 400, { code: 'INVALID', message: 'name required' });
        let slug = slugify(name);
        while (q1(db, `SELECT id FROM projects WHERE org_id = ? AND slug = ?`, org.id, slug)) slug += '-' + randomId().slice(0, 4);
        const pid = randomId();
        run(db, `INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, ?, ?)`, pid, org.id, name, slug);
        ['dev', 'staging', 'prod'].forEach((s, i) =>
          run(db, `INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`, randomId(), pid, s, s, i));
        audit(db, { orgId: org.id, actor, action: 'project.create', resource: `project/${slug}`, ip });
        return json(res, 201, { id: pid, name, slug });
      }
    }

    // GET/POST /api/v1/projects/:pid/environments
    if (seg[2] === 'projects' && seg[4] === 'environments' && !seg[5]) {
      const project = q1(db, `SELECT * FROM projects WHERE id = ?`, seg[3]);
      if (!project) return json(res, 404, { code: 'NOT_FOUND', message: 'project not found' });
      if (!can(db, user.id, project.org_id, 'read')) return json(res, 403, { code: 'FORBIDDEN', message: 'no access' });
      if (req.method === 'GET') {
        return json(res, 200, { environments: q(db, `SELECT id, name, slug, sort_order FROM environments WHERE project_id = ? ORDER BY sort_order`, project.id) });
      }
      const b = await bodyOf(req);
      const name = String(b.name || '').trim();
      if (!name) return json(res, 400, { code: 'INVALID', message: 'name required' });
      const max = q1(db, `SELECT COALESCE(MAX(sort_order), -1) AS m FROM environments WHERE project_id = ?`, project.id).m;
      const id = randomId();
      run(db, `INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`,
        id, project.id, name, slugify(name), max + 1);
      return json(res, 201, { id, name });
    }

    const project = seg[2] === 'projects' ? q1(db, `SELECT * FROM projects WHERE id = ?`, seg[3]) : null;
    if (project) {
      const orgId = project.org_id;
      const envSlug = String(query.get('env') || 'dev');
      const env = q1(db, `SELECT * FROM environments WHERE project_id = ? AND slug = ?`, project.id, envSlug);
      const folder = String(query.get('path') || '/');
      const key = decodeURIComponent(seg[5] || '');
      const resource = (k) => `project/${project.slug}/env/${envSlug}/secret/${k}`;

      // GET .../export?env=dev
      if (req.method === 'GET' && seg[4] === 'export' && !seg[5]) {
        if (!env) return json(res, 404, { code: 'NOT_FOUND', message: 'env not found' });
        if (!can(db, user.id, orgId, 'reveal', envSlug)) return json(res, 403, { code: 'FORBIDDEN', message: 'reveal not allowed for this role/env' });
        const dek = getOrgDEK(masterKey, q1(db, `SELECT * FROM orgs WHERE id = ?`, orgId));
        const rows = q(db, `SELECT s.key, v.ciphertext FROM secrets s JOIN secret_versions v ON v.secret_id = s.id AND v.version = s.latest_version WHERE s.env_id = ? ORDER BY s.key`, env.id);
        const lines = rows.map((r) => `${r.key}=${decrypt(dek, r.ciphertext)}`);
        audit(db, { orgId, actor, action: 'secrets.export', resource: `project/${project.slug}/env/${envSlug}`, metadata: { count: rows.length }, ip });
        res.writeHead(200, { 'Content-Type': 'text/plain', 'Content-Disposition': `attachment; filename="${project.slug}-${envSlug}.env"`, 'Cache-Control': 'no-store' });
        return res.end(lines.join('\n') + '\n');
      }

      // GET .../secrets  列表（元数据，无值）
      if (req.method === 'GET' && seg[4] === 'secrets' && !seg[5]) {
        if (!env) return json(res, 404, { code: 'NOT_FOUND', message: 'env not found' });
        if (!can(db, user.id, orgId, 'read')) return json(res, 403, { code: 'FORBIDDEN', message: 'no access' });
        const rows = q(db, `SELECT id, folder, key, comment, tags, latest_version AS version, updated_at FROM secrets WHERE env_id = ? ORDER BY folder, key`, env.id);
        const canReveal = can(db, user.id, orgId, 'reveal', envSlug);
        audit(db, { orgId, actor, action: 'secrets.list', resource: `project/${project.slug}/env/${envSlug}`, metadata: { count: rows.length }, ip });
        return json(res, 200, { secrets: rows.map((r) => ({ ...r, tags: JSON.parse(r.tags || '[]'), value: undefined, canReveal })) });
      }

      // POST .../secrets  创建/更新（产生新版本）
      if (req.method === 'POST' && seg[4] === 'secrets' && !seg[5]) {
        if (!env) return json(res, 404, { code: 'NOT_FOUND', message: 'env not found' });
        if (!can(db, user.id, orgId, 'write', envSlug)) return json(res, 403, { code: 'FORBIDDEN', message: 'write not allowed for this role/env' });
        const b = await bodyOf(req);
        const k = String(b.key || '').trim();
        const value = b.value;
        if (!k || typeof value !== 'string') return json(res, 400, { code: 'INVALID', message: 'key and value required' });
        const dek = getOrgDEK(masterKey, q1(db, `SELECT * FROM orgs WHERE id = ?`, orgId));
        let secret = q1(db, `SELECT * FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`, env.id, folder, k);
        const ct = encrypt(dek, value);
        if (!secret) {
          secret = { id: randomId() };
          run(db, `INSERT INTO secrets (id, env_id, folder, key, comment, tags) VALUES (?, ?, ?, ?, ?, ?)`,
            secret.id, env.id, folder, k, String(b.comment || ''), JSON.stringify(b.tags || []));
        } else {
          run(db, `UPDATE secrets SET comment = ?, tags = ?, updated_at = datetime('now') WHERE id = ?`,
            String(b.comment ?? secret.comment), JSON.stringify(b.tags ?? JSON.parse(secret.tags || '[]')), secret.id);
        }
        const nextVersion = (secret.latest_version || 0) + 1;
        run(db, `INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
          randomId(), secret.id, nextVersion, ct, user.id);
        run(db, `UPDATE secrets SET latest_version = ? WHERE id = ?`, nextVersion, secret.id);
        audit(db, { orgId, actor, action: secret.latest_version ? 'secrets.update' : 'secrets.create', resource: resource(k), metadata: { version: nextVersion }, ip });
        return json(res, 200, { key: k, version: nextVersion });
      }

      if (!seg[5] || !env) return json(res, 404, { code: 'NOT_FOUND', message: 'not found' });

      // GET .../secrets/:key  取明文（reveal，记审计）
      if (req.method === 'GET' && seg[6] === undefined) {
        if (!can(db, user.id, orgId, 'reveal', envSlug)) return json(res, 403, { code: 'FORBIDDEN', message: 'reveal not allowed for this role/env' });
        const secret = q1(db, `SELECT * FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`, env.id, folder, key);
        if (!secret) return json(res, 404, { code: 'NOT_FOUND', message: 'secret not found' });
        const ver = q1(db, `SELECT * FROM secret_versions WHERE secret_id = ? AND version = ?`, secret.id, secret.latest_version);
        const dek = getOrgDEK(masterKey, q1(db, `SELECT * FROM orgs WHERE id = ?`, orgId));
        audit(db, { orgId, actor, action: 'secrets.reveal', resource: resource(key), metadata: { version: secret.latest_version }, ip });
        return json(res, 200, { key, value: decrypt(dek, ver.ciphertext), version: secret.latest_version, comment: secret.comment, tags: JSON.parse(secret.tags || '[]') });
      }

      // GET .../secrets/:key/versions
      if (req.method === 'GET' && seg[6] === 'versions') {
        if (!can(db, user.id, orgId, 'read')) return json(res, 403, { code: 'FORBIDDEN', message: 'no access' });
        const secret = q1(db, `SELECT * FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`, env.id, folder, key);
        if (!secret) return json(res, 404, { code: 'NOT_FOUND', message: 'secret not found' });
        const versions = q(db, `SELECT version, created_by, created_at FROM secret_versions WHERE secret_id = ? ORDER BY version DESC`, secret.id);
        return json(res, 200, { key, latest: secret.latest_version, versions });
      }

      // POST .../secrets/:key/rollback {version}
      if (req.method === 'POST' && seg[6] === 'rollback') {
        if (!can(db, user.id, orgId, 'write', envSlug)) return json(res, 403, { code: 'FORBIDDEN', message: 'write not allowed' });
        const b = await bodyOf(req);
        const target = Number(b.version);
        const secret = q1(db, `SELECT * FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`, env.id, folder, key);
        if (!secret) return json(res, 404, { code: 'NOT_FOUND', message: 'secret not found' });
        const ver = q1(db, `SELECT * FROM secret_versions WHERE secret_id = ? AND version = ?`, secret.id, target);
        if (!ver) return json(res, 404, { code: 'NOT_FOUND', message: 'version not found' });
        const nextVersion = secret.latest_version + 1;
        run(db, `INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
          randomId(), secret.id, nextVersion, ver.ciphertext, user.id);
        run(db, `UPDATE secrets SET latest_version = ?, updated_at = datetime('now') WHERE id = ?`, nextVersion, secret.id);
        audit(db, { orgId, actor, action: 'secrets.rollback', resource: resource(key), metadata: { from: target, to: nextVersion }, ip });
        return json(res, 200, { key, version: nextVersion, rolledBackFrom: target });
      }
    }

    // GET /api/v1/orgs/:slug/audit
    if (seg[2] === 'orgs' && seg[4] === 'audit') {
      const org = q1(db, `SELECT * FROM orgs WHERE slug = ?`, seg[3]);
      if (!org) return json(res, 404, { code: 'NOT_FOUND', message: 'org not found' });
      if (!can(db, user.id, org.id, 'read')) return json(res, 403, { code: 'FORBIDDEN', message: 'no access' });
      const limit = Math.min(Number(query.get('limit') || 100), 500);
      const logs = q(db, `SELECT * FROM audit_logs WHERE org_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, org.id, limit);
      return json(res, 200, { logs });
    }

    return json(res, 404, { code: 'NOT_FOUND', message: `no route: ${req.method} ${pathname}` });
  };
}
