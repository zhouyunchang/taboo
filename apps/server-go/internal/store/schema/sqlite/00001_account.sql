-- +goose Up

CREATE TABLE users (
  id               TEXT PRIMARY KEY,
  email            TEXT NOT NULL UNIQUE,
  name             TEXT NOT NULL DEFAULT '',
  password_hash    TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'active',
  auth_source      TEXT NOT NULL DEFAULT 'local',
  external_sub     TEXT NOT NULL DEFAULT '',
  totp_pending_enc TEXT,
  totp_secret_enc  TEXT,
  totp_enabled     INTEGER NOT NULL DEFAULT 0,
  totp_last_step   INTEGER NOT NULL DEFAULT 0,
  created_at       TEXT NOT NULL DEFAULT (datetime('now'))
, handle TEXT NOT NULL DEFAULT '', handle_confirmed INTEGER NOT NULL DEFAULT 0);

CREATE TABLE orgs (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,
  dek_encrypted TEXT NOT NULL,
  kind          TEXT NOT NULL DEFAULT 'organization' CHECK (kind IN ('personal','organization')),
  owner_user_id TEXT REFERENCES users(id),
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE org_members (
  org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  restricted INTEGER NOT NULL DEFAULT 0,
  joined_at  TEXT NOT NULL DEFAULT (datetime('now')), scim_managed INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE refresh_tokens (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE recovery_codes (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL UNIQUE,
  used_at    TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE path_codes (
  code   TEXT PRIMARY KEY,
  kind   TEXT NOT NULL CHECK (kind IN ('user','organization')),
  ref_id TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_users_external ON users(auth_source, external_sub) WHERE external_sub != '';

CREATE UNIQUE INDEX orgs_one_personal_owner ON orgs(owner_user_id) WHERE kind = 'personal';

CREATE UNIQUE INDEX idx_users_handle ON users(handle) WHERE handle <> '';
