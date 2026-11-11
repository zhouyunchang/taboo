-- Secret Sync 与 Webhook

-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- sync_targets
DROP TABLE IF EXISTS sync_targets CASCADE;
CREATE TABLE sync_targets (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  platform           text NOT NULL,
  project_id         text,
  config_enc         text NOT NULL,
  enabled            bigint NOT NULL DEFAULT 1,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT sync_targets_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT sync_targets_platform_check CHECK ((platform = ANY (ARRAY['github'::text, 'vercel'::text, 'cloudflare'::text]))),
  CONSTRAINT sync_targets_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT targets_id_org UNIQUE (id, org_id),
  CONSTRAINT targets_project_org FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE
);

-- sync_runs
DROP TABLE IF EXISTS sync_runs CASCADE;
CREATE TABLE sync_runs (
  id                 text NOT NULL,
  target_id          text NOT NULL,
  org_id             text NOT NULL,
  event_id           text NOT NULL DEFAULT ''::text,
  secret_key         text NOT NULL,
  action             text NOT NULL,
  fingerprint        text NOT NULL DEFAULT ''::text,
  status             text NOT NULL DEFAULT 'pending'::text,
  trigger_type       text NOT NULL DEFAULT 'event'::text,
  attempts           bigint NOT NULL DEFAULT 0,
  next_attempt_at    bigint NOT NULL DEFAULT 0,
  error              text NOT NULL DEFAULT ''::text,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  updated_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  event_payload      text NOT NULL DEFAULT '{}'::text,
  lease_token        text NOT NULL DEFAULT ''::text,
  lease_until        bigint NOT NULL DEFAULT 0,
  event_sequence     bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT runs_target_org FOREIGN KEY (target_id, org_id) REFERENCES sync_targets(id, org_id) ON DELETE CASCADE,
  CONSTRAINT sync_runs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'success'::text, 'failed'::text, 'dead'::text]))),
  CONSTRAINT sync_runs_target_id_fkey FOREIGN KEY (target_id) REFERENCES sync_targets(id) ON DELETE CASCADE
);

-- webhooks
DROP TABLE IF EXISTS webhooks CASCADE;
CREATE TABLE webhooks (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  url                text NOT NULL,
  secret_enc         text NOT NULL,
  events             text NOT NULL DEFAULT '[]'::text,
  active             bigint NOT NULL DEFAULT 1,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT webhooks_id_org UNIQUE (id, org_id),
  CONSTRAINT webhooks_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);

-- webhook_deliveries
DROP TABLE IF EXISTS webhook_deliveries CASCADE;
CREATE TABLE webhook_deliveries (
  id                 text NOT NULL,
  webhook_id         text NOT NULL,
  org_id             text NOT NULL,
  event_id           text NOT NULL DEFAULT ''::text,
  event              text NOT NULL,
  payload            text NOT NULL,
  status             text NOT NULL DEFAULT 'pending'::text,
  http_code          bigint NOT NULL DEFAULT 0,
  error              text NOT NULL DEFAULT ''::text,
  attempts           bigint NOT NULL DEFAULT 0,
  next_attempt_at    bigint NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  delivered_at       text,
  lease_token        text NOT NULL DEFAULT ''::text,
  lease_until        bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT deliveries_webhook_org FOREIGN KEY (webhook_id, org_id) REFERENCES webhooks(id, org_id) ON DELETE CASCADE,
  CONSTRAINT webhook_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'success'::text, 'failed'::text, 'dead'::text]))),
  CONSTRAINT webhook_deliveries_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES webhooks(id) ON DELETE CASCADE
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_sync_due ON sync_runs (status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_sync_runs_target ON sync_runs (target_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sync_targets_org ON sync_targets (org_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_org ON webhooks (org_id);
CREATE INDEX IF NOT EXISTS idx_whd_due ON webhook_deliveries (status, next_attempt_at);
