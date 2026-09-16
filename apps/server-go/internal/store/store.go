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
  id               TEXT PRIMARY KEY,
  email            TEXT NOT NULL UNIQUE,
  name             TEXT NOT NULL DEFAULT '',
  password_hash    TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'active',
  totp_pending_enc TEXT,
  totp_secret_enc  TEXT,
  totp_enabled     INTEGER NOT NULL DEFAULT 0,
  totp_last_step   INTEGER NOT NULL DEFAULT 0,
  created_at       TEXT NOT NULL DEFAULT (datetime('now'))
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
  folder_id      TEXT REFERENCES folders(id),
  key            TEXT NOT NULL,
  comment        TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '[]',
  latest_version INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at     TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (env_id, folder, key)
);

-- 多级文件夹（M2 #5）：物化路径 /a/b/
CREATE TABLE IF NOT EXISTS folders (
  id         TEXT PRIMARY KEY,
  env_id     TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  parent_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE (env_id, path)
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

-- TOTP 2FA（M2 #4）：恢复码仅存 sha256 哈希，用后即废
CREATE TABLE IF NOT EXISTS recovery_codes (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL UNIQUE,
  used_at    TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 动态密钥（M4 #9）：高权限连接串 Master Key 加密自举；lease 绑定机器身份
CREATE TABLE IF NOT EXISTS dynamic_engines (
  id             TEXT PRIMARY KEY,
  org_id         TEXT NOT NULL,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  type           TEXT NOT NULL DEFAULT 'postgres' CHECK (type IN ('postgres')),
  conn_encrypted TEXT NOT NULL,
  database       TEXT NOT NULL DEFAULT 'postgres',
  default_ttl    INTEGER NOT NULL DEFAULT 3600,
  max_ttl        INTEGER NOT NULL DEFAULT 86400,
  created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS dynamic_leases (
  id                 TEXT PRIMARY KEY,
  engine_id          TEXT NOT NULL REFERENCES dynamic_engines(id) ON DELETE CASCADE,
  identity_id        TEXT NOT NULL,
  username           TEXT NOT NULL UNIQUE,
  password_encrypted TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','revoked','failed')),
  expires_at         INTEGER NOT NULL,
  created_at         TEXT NOT NULL DEFAULT (datetime('now')),
  revoked_at         TEXT
);
`)
	if err != nil {
		return err
	}
	// 既有库补齐列与约束
	_, _ = db.Exec(`ALTER TABLE audit_logs ADD COLUMN actor_type TEXT NOT NULL DEFAULT 'user'`)
	_, _ = db.Exec(`ALTER TABLE secrets ADD COLUMN folder_id TEXT REFERENCES folders(id)`)
	_, _ = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_secrets_env_folder_key ON secrets(env_id, folder_id, key)`)
	// TOTP 2FA（M2 #4）：users 补列 + recovery_codes 表
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN totp_pending_enc TEXT`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN totp_secret_enc TEXT`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0`)
	// 存量迁移：每环境补根文件夹，folder 字符串回填 folder_id（幂等）
	return migrateFolders(db)
}

// migrateFolders 幂等迁移：为每个环境创建根文件夹 '/'
// 并将 secrets.folder（物化路径字符串）映射到 folders 记录
func migrateFolders(db *sql.DB) error {
	// 1. 根文件夹
	if _, err := db.Exec(`INSERT INTO folders (id, env_id, parent_id, name, path)
		SELECT lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-' || lower(hex(randomblob(2))) || '-' || lower(hex(randomblob(2))) || '-' || lower(hex(randomblob(6))),
		       e.id, NULL, '/', '/'
		FROM environments e
		WHERE NOT EXISTS (SELECT 1 FROM folders f WHERE f.env_id = e.id AND f.path = '/')`); err != nil {
		return err
	}
	// 2. 非根 folder 字符串补建记录（挂到根下）
	if _, err := db.Exec(`INSERT INTO folders (id, env_id, parent_id, name, path)
		SELECT lower(hex(randomblob(16))), s.env_id,
		       (SELECT id FROM folders f WHERE f.env_id = s.env_id AND f.path = '/'),
		       trim(s.folder, '/'), s.folder
		FROM (SELECT DISTINCT env_id, folder FROM secrets WHERE folder <> '/') s
		WHERE NOT EXISTS (SELECT 1 FROM folders f WHERE f.env_id = s.env_id AND f.path = s.folder)`); err != nil {
		return err
	}
	// 3. 回填 secrets.folder_id
	_, err := db.Exec(`UPDATE secrets SET folder_id =
		(SELECT id FROM folders f WHERE f.env_id = secrets.env_id AND f.path = secrets.folder)
		WHERE folder_id IS NULL`)
	return err
}
