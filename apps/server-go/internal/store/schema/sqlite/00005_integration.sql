-- +goose Up

CREATE TABLE sync_targets (
  id           TEXT PRIMARY KEY,
  org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  platform     TEXT NOT NULL CHECK (platform IN ('github','vercel','cloudflare')),
  project_id   TEXT REFERENCES projects(id) ON DELETE CASCADE,
  config_enc   TEXT NOT NULL,
  enabled      INTEGER NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE sync_runs (
  id           TEXT PRIMARY KEY,
  target_id    TEXT NOT NULL REFERENCES sync_targets(id) ON DELETE CASCADE,
  org_id       TEXT NOT NULL,
  event_id     TEXT NOT NULL DEFAULT '',
  secret_key   TEXT NOT NULL,
  action       TEXT NOT NULL,
  fingerprint  TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','dead')),
  trigger_type TEXT NOT NULL DEFAULT 'event',
  attempts     INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL DEFAULT 0,
  error        TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
, lease_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0, event_payload TEXT NOT NULL DEFAULT '{}', event_sequence INTEGER NOT NULL DEFAULT 0);

CREATE TABLE webhooks (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,
  secret_enc  TEXT NOT NULL,
  events      TEXT NOT NULL DEFAULT '[]',
  active      INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE webhook_deliveries (
  id             TEXT PRIMARY KEY,
  webhook_id     TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  org_id         TEXT NOT NULL,
  event_id       TEXT NOT NULL DEFAULT '',
  event          TEXT NOT NULL,
  payload        TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','dead')),
  http_code      INTEGER NOT NULL DEFAULT 0,
  error          TEXT NOT NULL DEFAULT '',
  attempts       INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (datetime('now')),
  delivered_at   TEXT
, lease_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0);

CREATE INDEX idx_sync_targets_org ON sync_targets(org_id);

CREATE INDEX idx_sync_runs_target ON sync_runs(target_id, created_at);

CREATE INDEX idx_sync_due ON sync_runs(status, next_attempt_at);

CREATE INDEX idx_webhooks_org ON webhooks(org_id);

CREATE INDEX idx_whd_due ON webhook_deliveries(status, next_attempt_at);

CREATE TRIGGER integrity_sync_targets_INSERT BEFORE INSERT ON sync_targets WHEN NOT (NEW.project_id IS NULL OR EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_sync_targets_UPDATE BEFORE UPDATE ON sync_targets WHEN NOT (NEW.project_id IS NULL OR EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_sync_runs_INSERT BEFORE INSERT ON sync_runs WHEN NOT (EXISTS(SELECT 1 FROM sync_targets t WHERE t.id=NEW.target_id AND t.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_sync_runs_UPDATE BEFORE UPDATE ON sync_runs WHEN NOT (EXISTS(SELECT 1 FROM sync_targets t WHERE t.id=NEW.target_id AND t.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_webhook_deliveries_INSERT BEFORE INSERT ON webhook_deliveries WHEN NOT (EXISTS(SELECT 1 FROM webhooks w WHERE w.id=NEW.webhook_id AND w.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_webhook_deliveries_UPDATE BEFORE UPDATE ON webhook_deliveries WHEN NOT (EXISTS(SELECT 1 FROM webhooks w WHERE w.id=NEW.webhook_id AND w.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER immutable_sync_targets BEFORE UPDATE ON sync_targets WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_webhooks BEFORE UPDATE ON webhooks WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;
