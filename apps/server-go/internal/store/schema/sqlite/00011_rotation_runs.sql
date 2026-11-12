-- 轮换重设计：策略状态、生成档位、轮次与 worker 心跳。
-- +goose Up

ALTER TABLE rotation_policies ADD COLUMN created_by TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN mode TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE rotation_policies ADD COLUMN owner_user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN grace_seconds INTEGER NOT NULL DEFAULT 604800;
ALTER TABLE rotation_policies ADD COLUMN status TEXT NOT NULL DEFAULT 'paused';
ALTER TABLE rotation_policies ADD COLUMN fixed_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN fixed_by TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN fixed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN generation_profile TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN generation_length INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rotation_policies ADD COLUMN generation_prefix TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN auto_generate INTEGER NOT NULL DEFAULT 0;
ALTER TABLE rotation_policies ADD COLUMN last_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE rotation_policies ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

UPDATE rotation_policies SET status = CASE WHEN enabled = 1 THEN 'active' ELSE 'paused' END;
UPDATE rotation_policies SET next_run_at = 0 WHERE status <> 'active';
UPDATE rotation_policies
SET owner_user_id = COALESCE((
  SELECT om.user_id
  FROM projects pr
  JOIN org_members om ON om.org_id = pr.org_id
  WHERE pr.id = rotation_policies.project_id AND om.role IN ('owner', 'admin')
  ORDER BY CASE om.role WHEN 'owner' THEN 0 ELSE 1 END, om.user_id
  LIMIT 1
), '');

CREATE TABLE rotation_runs (
  id               TEXT PRIMARY KEY,
  org_id           TEXT NOT NULL,
  project_id       TEXT NOT NULL,
  policy_id        TEXT REFERENCES rotation_policies(id) ON DELETE SET NULL,
  secret_id        TEXT REFERENCES secrets(id) ON DELETE SET NULL,
  secret_key       TEXT NOT NULL,
  folder_path      TEXT NOT NULL,
  policy_snapshot  TEXT NOT NULL DEFAULT '',
  run_key          TEXT NOT NULL UNIQUE,
  trigger          TEXT NOT NULL CHECK (trigger IN ('schedule','manual','retry')),
  status           TEXT NOT NULL CHECK (status IN (
    'due','awaiting_generation','awaiting_distribution','awaiting_manual',
    'awaiting_external','overdue','succeeded','failed','skipped','canceled'
  )),
  due_at           INTEGER NOT NULL,
  grace_until      INTEGER NOT NULL,
  old_version      INTEGER NOT NULL DEFAULT 0,
  new_version      INTEGER,
  owner_user_id    TEXT NOT NULL DEFAULT '',
  actor_id         TEXT NOT NULL DEFAULT '',
  actor_kind       TEXT NOT NULL DEFAULT '',
  attempts         INTEGER NOT NULL DEFAULT 0,
  last_error       TEXT NOT NULL DEFAULT '',
  evidence_json    TEXT NOT NULL DEFAULT '{}',
  created_at       TEXT NOT NULL DEFAULT (datetime('now')),
  started_at       TEXT NOT NULL DEFAULT '',
  finished_at      TEXT NOT NULL DEFAULT '',
  updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX rotation_runs_schedule_key
  ON rotation_runs(policy_id, due_at) WHERE trigger = 'schedule';
CREATE UNIQUE INDEX rotation_runs_open_policy
  ON rotation_runs(policy_id)
  WHERE status IN ('due','awaiting_generation','awaiting_distribution','awaiting_manual','awaiting_external','overdue');
CREATE INDEX rotation_runs_secret_idx ON rotation_runs(secret_id, created_at);
CREATE INDEX rotation_runs_status_idx ON rotation_runs(status, grace_until);

CREATE TABLE worker_heartbeats (
  worker_id     TEXT PRIMARY KEY,
  role          TEXT NOT NULL,
  hostname      TEXT NOT NULL DEFAULT '',
  version       TEXT NOT NULL DEFAULT '',
  started_at    INTEGER NOT NULL,
  last_seen_at  INTEGER NOT NULL,
  last_sweep_at INTEGER NOT NULL DEFAULT 0,
  claimed       INTEGER NOT NULL DEFAULT 0,
  errors        INTEGER NOT NULL DEFAULT 0
);
