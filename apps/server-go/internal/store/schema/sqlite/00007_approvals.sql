-- +goose Up

CREATE TABLE approval_requests (
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

CREATE TABLE access_requests (
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

CREATE INDEX idx_approval_org ON approval_requests(org_id, status, expires_at);

CREATE INDEX idx_access_active ON access_requests(org_id, requester_id, status, expires_at);
