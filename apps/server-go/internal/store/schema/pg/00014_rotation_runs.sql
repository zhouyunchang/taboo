-- 轮换重设计：策略状态、生成档位、轮次与 worker 心跳。
-- +goose Up

ALTER TABLE rotation_policies
  ADD COLUMN created_by text NOT NULL DEFAULT '',
  ADD COLUMN mode text NOT NULL DEFAULT 'manual',
  ADD COLUMN owner_user_id text NOT NULL DEFAULT '',
  ADD COLUMN grace_seconds integer NOT NULL DEFAULT 604800,
  ADD COLUMN status text NOT NULL DEFAULT 'paused',
  ADD COLUMN fixed_reason text NOT NULL DEFAULT '',
  ADD COLUMN fixed_by text NOT NULL DEFAULT '',
  ADD COLUMN fixed_at text NOT NULL DEFAULT '',
  ADD COLUMN generation_profile text NOT NULL DEFAULT '',
  ADD COLUMN generation_length integer NOT NULL DEFAULT 0,
  ADD COLUMN generation_prefix text NOT NULL DEFAULT '',
  ADD COLUMN auto_generate integer NOT NULL DEFAULT 0,
  ADD COLUMN last_run_id text NOT NULL DEFAULT '',
  ADD COLUMN version integer NOT NULL DEFAULT 1;

-- 旧策略：enabled=1 → active，enabled=0 → paused；没有策略的 secret 保持未设置。
UPDATE rotation_policies
SET status = CASE WHEN enabled = 1 THEN 'active' ELSE 'paused' END,
    created_by = COALESCE(created_by, '');
UPDATE rotation_policies
SET next_run_at = 0
WHERE status <> 'active';
UPDATE rotation_policies p
SET owner_user_id = COALESCE((
  SELECT om.user_id
  FROM projects pr
  JOIN org_members om ON om.org_id = pr.org_id
  WHERE pr.id = p.project_id AND om.role IN ('owner', 'admin')
  ORDER BY CASE om.role WHEN 'owner' THEN 0 ELSE 1 END, om.user_id
  LIMIT 1
), '');

ALTER TABLE rotation_policies
  ADD CONSTRAINT rotation_policies_status_check
  CHECK (status IN ('draft', 'active', 'paused', 'fixed'));

CREATE TABLE rotation_runs (
  id               text NOT NULL,
  org_id           text NOT NULL,
  project_id       text NOT NULL,
  policy_id        text,
  secret_id        text,
  secret_key       text NOT NULL,
  folder_path      text NOT NULL,
  policy_snapshot  text NOT NULL DEFAULT '',
  run_key          text NOT NULL,
  trigger          text NOT NULL,
  status           text NOT NULL,
  due_at           bigint NOT NULL,
  grace_until      bigint NOT NULL,
  old_version      integer NOT NULL DEFAULT 0,
  new_version      integer,
  owner_user_id    text NOT NULL DEFAULT '',
  actor_id         text NOT NULL DEFAULT '',
  actor_kind       text NOT NULL DEFAULT '',
  attempts         integer NOT NULL DEFAULT 0,
  last_error       text NOT NULL DEFAULT '',
  evidence_json    text NOT NULL DEFAULT '{}',
  created_at       text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  started_at       text NOT NULL DEFAULT '',
  finished_at      text NOT NULL DEFAULT '',
  updated_at       text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT rotation_runs_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES rotation_policies(id) ON DELETE SET NULL,
  CONSTRAINT rotation_runs_secret_id_fkey FOREIGN KEY (secret_id) REFERENCES secrets(id) ON DELETE SET NULL,
  CONSTRAINT rotation_runs_trigger_check CHECK (trigger IN ('schedule', 'manual', 'retry')),
  CONSTRAINT rotation_runs_status_check CHECK (status IN (
    'due', 'awaiting_generation', 'awaiting_distribution', 'awaiting_manual',
    'awaiting_external', 'overdue', 'succeeded', 'failed', 'skipped', 'canceled'
  ))
);

CREATE UNIQUE INDEX rotation_runs_schedule_key
  ON rotation_runs (policy_id, due_at)
  WHERE trigger = 'schedule';
CREATE UNIQUE INDEX rotation_runs_open_policy
  ON rotation_runs (policy_id)
  WHERE status IN ('due', 'awaiting_generation', 'awaiting_distribution', 'awaiting_manual', 'awaiting_external', 'overdue');
CREATE INDEX rotation_runs_secret_idx ON rotation_runs (secret_id, created_at);
CREATE INDEX rotation_runs_status_idx ON rotation_runs (status, grace_until);

CREATE TABLE worker_heartbeats (
  worker_id     text PRIMARY KEY,
  role          text NOT NULL,
  hostname      text NOT NULL DEFAULT '',
  version       text NOT NULL DEFAULT '',
  started_at    bigint NOT NULL,
  last_seen_at  bigint NOT NULL,
  last_sweep_at bigint NOT NULL DEFAULT 0,
  claimed       integer NOT NULL DEFAULT 0,
  errors        integer NOT NULL DEFAULT 0
);

ALTER TABLE rotation_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE rotation_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON rotation_runs FOR ALL
  USING (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))))
  WITH CHECK (((CURRENT_USER = '{owner}') OR (org_id = NULLIF(current_setting('taboo.org_id'::text, true), ''::text))));
GRANT SELECT, INSERT, UPDATE, DELETE ON rotation_runs TO {role};
-- worker_heartbeats 是全局基础设施表，不含租户数据，不加 RLS；运行角色可读写。
GRANT SELECT, INSERT, UPDATE, DELETE ON worker_heartbeats TO {role};
