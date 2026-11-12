-- +goose Up

CREATE TABLE scan_reports (
  id              TEXT PRIMARY KEY,
  org_id          TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  created_by      TEXT NOT NULL,
  actor_name      TEXT NOT NULL DEFAULT '',
  source          TEXT NOT NULL DEFAULT '',
  finding_count   INTEGER NOT NULL DEFAULT 0,
  critical_count  INTEGER NOT NULL DEFAULT 0,
  warning_count   INTEGER NOT NULL DEFAULT 0,
  findings_json   TEXT NOT NULL,
  created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE rotation_policies (
  id                TEXT PRIMARY KEY,
  org_id            TEXT NOT NULL,
  project_id        TEXT NOT NULL,
  secret_id         TEXT NOT NULL UNIQUE REFERENCES secrets(id) ON DELETE CASCADE,
  interval_seconds  INTEGER NOT NULL DEFAULT 0,
  cron              TEXT NOT NULL DEFAULT '',
  enabled           INTEGER NOT NULL DEFAULT 1,
  next_run_at       INTEGER NOT NULL,
  last_run_at       INTEGER NOT NULL DEFAULT 0,
  last_event_id     TEXT NOT NULL DEFAULT '',
  lease_token       TEXT NOT NULL DEFAULT '',
  lease_until       INTEGER NOT NULL DEFAULT 0,
  created_at        TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE dynamic_engines (
  id             TEXT PRIMARY KEY,
  org_id         TEXT NOT NULL,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  type           TEXT NOT NULL DEFAULT 'postgres' CHECK (type IN ('postgres','mysql')),
  conn_encrypted TEXT NOT NULL,
  database       TEXT NOT NULL DEFAULT 'postgres',
  default_ttl    INTEGER NOT NULL DEFAULT 3600,
  max_ttl        INTEGER NOT NULL DEFAULT 86400,
  created_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE dynamic_leases (
  id                 TEXT PRIMARY KEY,
  engine_id          TEXT NOT NULL REFERENCES dynamic_engines(id) ON DELETE CASCADE,
  identity_id        TEXT NOT NULL,
  username           TEXT NOT NULL UNIQUE,
  password_encrypted TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','revoked','failed','revoking')),
  reclaim_fails      INTEGER NOT NULL DEFAULT 0,
  expires_at         INTEGER NOT NULL,
  created_at         TEXT NOT NULL DEFAULT (datetime('now')),
  revoked_at         TEXT
, lease_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0);

CREATE INDEX idx_scan_reports_org ON scan_reports(org_id, created_at);

CREATE INDEX idx_rotation_due ON rotation_policies(enabled, next_run_at);

CREATE TRIGGER integrity_dynamic_engines_INSERT BEFORE INSERT ON dynamic_engines WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_dynamic_engines_UPDATE BEFORE UPDATE ON dynamic_engines WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER immutable_dynamic_engines BEFORE UPDATE ON dynamic_engines WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;
