package store

// Phase 6 tables. INTEGER/BIGINT and datetime('now') are valid on both SQLite
// and the PostgreSQL rewriter. Partial unique indexes keep empty external ids
// from colliding.
const phase6Tables = `
CREATE TABLE IF NOT EXISTS approval_requests (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL,
  folder TEXT NOT NULL DEFAULT '/',
  secret_key TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('upsert','delete','rollback')),
  payload_enc TEXT NOT NULL DEFAULT '',
  payload_mac TEXT NOT NULL DEFAULT '',
  meta_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('pending','applied','rejected','expired','cancelled')),
  requester_id TEXT NOT NULL,
  requester_kind TEXT NOT NULL,
  approver_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  decision_note TEXT NOT NULL DEFAULT '',
  expires_at BIGINT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  decided_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_approval_org ON approval_requests(org_id, status, expires_at);

CREATE TABLE IF NOT EXISTS access_requests (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK (action IN ('reveal','export','write','delete','rollback')),
  requester_id TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('pending','approved','rejected','expired','revoked')),
  approver_id TEXT NOT NULL DEFAULT '',
  decision_note TEXT NOT NULL DEFAULT '',
  ttl_seconds INTEGER NOT NULL,
  expires_at BIGINT NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  decided_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_access_active ON access_requests(org_id, requester_id, status, expires_at);

CREATE TABLE IF NOT EXISTS scim_tokens (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  last_used_at BIGINT NOT NULL DEFAULT 0,
  expires_at BIGINT NOT NULL DEFAULT 0,
  revoked_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_scim_tokens_org ON scim_tokens(org_id);

CREATE TABLE IF NOT EXISTS scim_users (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  user_name TEXT NOT NULL,
  active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (org_id, user_name)
);
CREATE INDEX IF NOT EXISTS idx_scim_users_user ON scim_users(org_id, user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scim_users_external ON scim_users(org_id, external_id) WHERE external_id <> '';

CREATE TABLE IF NOT EXISTS scim_groups (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  display_name TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '' CHECK (role IN ('','viewer','developer','admin')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (org_id, display_name)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scim_groups_external ON scim_groups(org_id, external_id) WHERE external_id <> '';

CREATE TABLE IF NOT EXISTS scim_group_members (
  org_id TEXT NOT NULL,
  group_id TEXT NOT NULL,
  user_scim_id TEXT NOT NULL,
  PRIMARY KEY (group_id, user_scim_id)
);
CREATE INDEX IF NOT EXISTS idx_scim_members_user ON scim_group_members(org_id, user_scim_id);

CREATE TABLE IF NOT EXISTS device_authorizations (
  id TEXT PRIMARY KEY,
  device_hash TEXT NOT NULL UNIQUE,
  user_code_hash TEXT NOT NULL UNIQUE,
  client_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','approved','denied','consumed','expired')),
  user_id TEXT NOT NULL DEFAULT '',
  org_id TEXT NOT NULL DEFAULT '',
  interval_seconds INTEGER NOT NULL,
  expires_at BIGINT NOT NULL,
  last_polled_at BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_device_exp ON device_authorizations(expires_at);

CREATE TABLE IF NOT EXISTS cli_auth_codes (
  code_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  org_id TEXT NOT NULL,
  challenge TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  expires_at BIGINT NOT NULL,
  consumed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS logout_replays (
  token_hash TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL,
  expires_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS rate_counters (
  bucket TEXT NOT NULL,
  window_start BIGINT NOT NULL,
  hits INTEGER NOT NULL,
  PRIMARY KEY (bucket, window_start)
);

CREATE TABLE IF NOT EXISTS cluster_leases (
  name TEXT PRIMARY KEY,
  holder TEXT NOT NULL,
  lease_until BIGINT NOT NULL
);
`
