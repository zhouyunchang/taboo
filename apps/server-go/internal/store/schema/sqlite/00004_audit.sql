-- +goose Up

CREATE TABLE audit_logs (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  actor_name  TEXT NOT NULL DEFAULT '',
  actor_type  TEXT NOT NULL DEFAULT 'user',
  action      TEXT NOT NULL,
  resource    TEXT NOT NULL,
  metadata    TEXT NOT NULL DEFAULT '{}',
  ip          TEXT NOT NULL DEFAULT '',
  user_agent  TEXT NOT NULL DEFAULT '',
  request_id  TEXT NOT NULL DEFAULT '',
  prev_hash   TEXT NOT NULL DEFAULT '',
  entry_hash  TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE audit_exports (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  format      TEXT NOT NULL CHECK (format IN ('csv','jsonl')),
  filters     TEXT NOT NULL DEFAULT '{}',
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','failed')),
  file_path   TEXT NOT NULL DEFAULT '',
  row_count   INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (datetime('now')),
  completed_at TEXT
, lease_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0);

CREATE TABLE outbox_events (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL,
  event_id   TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'secret',
  payload    TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','dead')),
  error      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
, event_sequence INTEGER NOT NULL DEFAULT 0);

CREATE TABLE audit_checkpoints (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL,
  last_id    TEXT NOT NULL,
  last_hash  TEXT NOT NULL,
  hmac       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE mutation_receipts (org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE, actor_id TEXT NOT NULL, request_key TEXT NOT NULL, request_hash TEXT NOT NULL, response TEXT NOT NULL, PRIMARY KEY(org_id,actor_id,request_key));

CREATE TABLE audit_heads (org_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0);

CREATE INDEX idx_audit_org_time ON audit_logs(org_id, created_at);

CREATE INDEX idx_audit_exports_org ON audit_exports(org_id);

CREATE INDEX idx_outbox_pending ON outbox_events(status, created_at);

CREATE INDEX idx_audit_cp_org ON audit_checkpoints(org_id, created_at);
