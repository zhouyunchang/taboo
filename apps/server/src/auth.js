// 认证中间件 + RBAC-lite + 审计
// 策略四元组（设计文档 §6）：(主体, 资源, 动作, 环境约束)
// MVP 预置角色：owner > admin > developer > viewer
//   viewer    读元数据（列表）
//   developer 读元数据 + dev/staging reveal + 写 dev/staging
//   admin     全环境 read/write/reveal
//   owner     admin + 成员管理
// reveal（取明文）与 read（看元数据）分离 —— 列表可见 ≠ 能看值。
import { q1, run } from './db.js';
import { verifyJWT, signJWT, sha256, randomId } from './crypto.js';

export const ROLE_RANK = { viewer: 1, developer: 2, admin: 3, owner: 4 };

export function audit(db, { orgId, actor, action, resource, metadata = {}, ip = '' }) {
  run(
    db,
    `INSERT INTO audit_logs (id, org_id, actor_id, actor_name, action, resource, metadata, ip)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    randomId(), orgId, actor?.id ?? 'system', actor?.name ?? '', action, resource,
    JSON.stringify(metadata), ip,
  );
}

export function membership(db, orgId, userId) {
  return q1(db, `SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, orgId, userId);
}

// action: 'read' | 'reveal' | 'write' | 'manage'
export function can(db, userId, orgId, action, envSlug = null) {
  const m = membership(db, orgId, userId);
  if (!m) return false;
  const role = m.role;
  switch (action) {
    case 'read':    return ROLE_RANK[role] >= ROLE_RANK.viewer;
    case 'reveal':
      if (ROLE_RANK[role] >= ROLE_RANK.admin) return true;
      if (role === 'developer') return envSlug !== 'prod';
      return false;
    case 'write':
      if (ROLE_RANK[role] >= ROLE_RANK.admin) return true;
      if (role === 'developer') return envSlug !== 'prod';
      return false;
    case 'manage':  return ROLE_RANK[role] >= ROLE_RANK.owner;
    default:        return false;
  }
}

// ---- HTTP 中间件 ----

export function makeAuth(db, jwtSecret) {
  return function auth(req, res) {
    const header = req.headers.authorization || '';
    const token = header.startsWith('Bearer ') ? header.slice(7) : null;
    const payload = token && verifyJWT(token, jwtSecret);
    if (!payload) {
      res.writeHead(401, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ code: 'UNAUTHORIZED', message: 'missing or invalid token' }));
      return null;
    }
    const user = q1(db, `SELECT id, email, name, status FROM users WHERE id = ?`, payload.sub);
    if (!user || user.status !== 'active') {
      res.writeHead(401, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ code: 'UNAUTHORIZED', message: 'user not found or disabled' }));
      return null;
    }
    return user;
  };
}

export function issueTokens(db, jwtSecret, user) {
  const access = signJWT({ sub: user.id, email: user.email }, jwtSecret, 900); // 15min
  const refresh = randomId() + randomId();
  run(
    db,
    `INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
    randomId(), user.id, sha256(refresh), Date.now() + 30 * 86400_000,
  );
  return { access, refresh, tokenType: 'Bearer', expiresIn: 900 };
}
