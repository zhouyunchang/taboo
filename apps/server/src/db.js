// SQLite 存储层（node:sqlite 内置驱动，免外部依赖，MVP 单文件嵌入式模式）
// 生产形态对齐设计文档：PostgreSQL 为主存储，本文件中的 DDL 以 PG 语义设计、SQLite 方言落地。
import { DatabaseSync } from 'node:sqlite';
import fs from 'node:fs';
import path from 'node:path';

export function openDatabase(dataDir) {
  fs.mkdirSync(dataDir, { recursive: true });
  const db = new DatabaseSync(path.join(dataDir, 'taboo.db'));
  db.exec('PRAGMA journal_mode = WAL');
  db.exec('PRAGMA foreign_keys = ON');
  migrate(db);
  return db;
}

function migrate(db) {
  db.exec(`
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
    dek_encrypted TEXT NOT NULL,          -- 组织 DEK，经 Root Key 加密
    created_at    TEXT NOT NULL DEFAULT (datetime('now'))
  );

  CREATE TABLE IF NOT EXISTS org_members (
    org_id   TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role     TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
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
    id               TEXT PRIMARY KEY,
    env_id           TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    folder           TEXT NOT NULL DEFAULT '/',
    key              TEXT NOT NULL,
    comment          TEXT NOT NULL DEFAULT '',
    tags             TEXT NOT NULL DEFAULT '[]',
    latest_version   INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (env_id, folder, key)
  );

  -- 只追加不修改：版本历史 + 密文（AES-256-GCM, nonce|tag|ct）
  CREATE TABLE IF NOT EXISTS secret_versions (
    id         TEXT PRIMARY KEY,
    secret_id  TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    ciphertext TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (secret_id, version)
  );

  -- 审计：append-only，应用层强制只 INSERT
  CREATE TABLE IF NOT EXISTS audit_logs (
    id         TEXT PRIMARY KEY,
    org_id     TEXT NOT NULL,
    actor_id   TEXT NOT NULL,
    actor_name TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,            -- secrets.read / secrets.write / secrets.reveal / secrets.rollback / secrets.export / auth.login ...
    resource   TEXT NOT NULL,            -- e.g. project/billing/env/prod/secret/DB_PASS
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
  `);
}

// ---- 通用小工具 ----
export function q(db, sql, ...params) {
  return db.prepare(sql).all(...params);
}
export function q1(db, sql, ...params) {
  return db.prepare(sql).get(...params);
}
export function run(db, sql, ...params) {
  return db.prepare(sql).run(...params);
}
