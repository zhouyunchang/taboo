// 存储层：SQLite（modernc.org/sqlite 纯 Go 驱动，免 CGO）
// DDL 以 PostgreSQL 语义设计、SQLite 方言落地，与 Node MVP schema 完全一致。
// 注：sqlc 代码生成在 PG 主模式确定后接入（见 issue #1 任务清单）。
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func Open(dataDir string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s/taboo.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", dataDir)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 单写多读：SQLite 单连接避免 database is locked
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'active',
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS orgs (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,
  dek_encrypted TEXT NOT NULL,
  created_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS org_members (
  org_id    TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  joined_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS projects (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (org_id, slug)
);

CREATE TABLE IF NOT EXISTS environments (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  UNIQUE (project_id, slug)
);

CREATE TABLE IF NOT EXISTS secrets (
  id             TEXT PRIMARY KEY,
  env_id         TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  folder         TEXT NOT NULL DEFAULT '/',
  key            TEXT NOT NULL,
  comment        TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '[]',
  latest_version INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (env_id, folder, key)
);

CREATE TABLE IF NOT EXISTS secret_versions (
  id         TEXT PRIMARY KEY,
  secret_id  TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  version    INTEGER NOT NULL,
  ciphertext TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (secret_id, version)
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL,
  actor_id   TEXT NOT NULL,
  actor_name TEXT NOT NULL DEFAULT '',
  actor_type TEXT NOT NULL DEFAULT 'user',
  action     TEXT NOT NULL,
  resource   TEXT NOT NULL,
  metadata   TEXT NOT NULL DEFAULT '{}',
  ip         TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_audit_org_time ON audit_logs(org_id, created_at);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 机器身份（M2 #3）：CI/CD、AI Agent 的受限凭据
CREATE TABLE IF NOT EXISTS machine_identities (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  auth_type   TEXT NOT NULL DEFAULT 'client_credentials',
  client_id   TEXT NOT NULL UNIQUE,
  secret_hash TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  token_ttl   INTEGER NOT NULL DEFAULT 900,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 作用域：显式 (项目, 环境, 权限) 三元组，禁止通配（设计文档 §6）
CREATE TABLE IF NOT EXISTS identity_scopes (
  identity_id TEXT NOT NULL REFERENCES machine_identities(id) ON DELETE CASCADE,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  env_id      TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  permission  TEXT NOT NULL CHECK (permission IN ('read','write')),
  PRIMARY KEY (identity_id, project_id, env_id)
);
`)
	// 既有库补齐 audit_logs.actor_type 列（区分 user / identity 主体）
	_, _ = db.Exec(`ALTER TABLE audit_logs ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user'`)
	return err
}
