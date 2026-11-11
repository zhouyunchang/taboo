-- +goose Up

CREATE TABLE scim_tokens (
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

CREATE TABLE scim_users (
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

CREATE TABLE scim_groups (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  display_name TEXT NOT NULL,
  external_id TEXT NOT NULL DEFAULT '',
  role TEXT NOT NULL DEFAULT '' CHECK (role IN ('','viewer','developer','admin')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (org_id, display_name)
);

CREATE TABLE scim_group_members (
  org_id TEXT NOT NULL,
  group_id TEXT NOT NULL,
  user_scim_id TEXT NOT NULL,
  PRIMARY KEY (group_id, user_scim_id)
);

CREATE INDEX idx_scim_tokens_org ON scim_tokens(org_id);

CREATE INDEX idx_scim_users_user ON scim_users(org_id, user_id);

CREATE UNIQUE INDEX idx_scim_users_external ON scim_users(org_id, external_id) WHERE external_id <> '';

CREATE UNIQUE INDEX idx_scim_groups_external ON scim_groups(org_id, external_id) WHERE external_id <> '';

CREATE INDEX idx_scim_members_user ON scim_group_members(org_id, user_scim_id);
