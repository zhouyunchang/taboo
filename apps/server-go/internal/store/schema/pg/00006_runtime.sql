-- 动态密钥、轮换、扫描与审批
-- +goose Up


-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- dynamic_engines
CREATE TABLE dynamic_engines (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  name               text NOT NULL,
  type               text NOT NULL DEFAULT 'postgres'::text,
  conn_encrypted     text NOT NULL,
  database           text NOT NULL DEFAULT 'postgres'::text,
  default_ttl        bigint NOT NULL DEFAULT 3600,
  max_ttl            bigint NOT NULL DEFAULT 86400,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT dynamic_engines_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT dynamic_engines_type_check CHECK ((type = ANY (ARRAY['postgres'::text, 'mysql'::text]))),
  CONSTRAINT engines_project_org FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE
);

-- dynamic_leases
CREATE TABLE dynamic_leases (
  id                 text NOT NULL,
  engine_id          text NOT NULL,
  identity_id        text NOT NULL,
  username           text NOT NULL,
  password_encrypted text NOT NULL,
  status             text NOT NULL DEFAULT 'active'::text,
  reclaim_fails      bigint NOT NULL DEFAULT 0,
  expires_at         bigint NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  revoked_at         text,
  lease_token        text NOT NULL DEFAULT ''::text,
  lease_until        bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT dynamic_leases_engine_id_fkey FOREIGN KEY (engine_id) REFERENCES dynamic_engines(id) ON DELETE CASCADE,
  CONSTRAINT dynamic_leases_status_check CHECK ((status = ANY (ARRAY['active'::text, 'expired'::text, 'revoked'::text, 'failed'::text, 'revoking'::text]))),
  CONSTRAINT dynamic_leases_username_key UNIQUE (username)
);

-- rotation_policies
CREATE TABLE rotation_policies (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  secret_id          text NOT NULL,
  interval_seconds   integer NOT NULL DEFAULT 0,
  cron               text NOT NULL DEFAULT ''::text,
  enabled            integer NOT NULL DEFAULT 1,
  next_run_at        bigint NOT NULL,
  last_run_at        bigint NOT NULL DEFAULT 0,
  last_event_id      text NOT NULL DEFAULT ''::text,
  lease_token        text NOT NULL DEFAULT ''::text,
  lease_until        bigint NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  updated_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT rotation_policies_secret_id_fkey FOREIGN KEY (secret_id) REFERENCES secrets(id) ON DELETE CASCADE,
  CONSTRAINT rotation_policies_secret_id_key UNIQUE (secret_id)
);

-- scan_reports
CREATE TABLE scan_reports (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  created_by         text NOT NULL,
  actor_name         text NOT NULL DEFAULT ''::text,
  source             text NOT NULL DEFAULT ''::text,
  finding_count      integer NOT NULL DEFAULT 0,
  critical_count     integer NOT NULL DEFAULT 0,
  warning_count      integer NOT NULL DEFAULT 0,
  findings_json      text NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT scan_reports_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);

-- approval_requests
CREATE TABLE approval_requests (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  folder             text NOT NULL DEFAULT '/'::text,
  secret_key         text NOT NULL,
  action             text NOT NULL,
  payload_enc        text NOT NULL DEFAULT ''::text,
  payload_mac        text NOT NULL DEFAULT ''::text,
  meta_json          text NOT NULL DEFAULT '{}'::text,
  status             text NOT NULL,
  requester_id       text NOT NULL,
  requester_kind     text NOT NULL,
  approver_id        text NOT NULL DEFAULT ''::text,
  reason             text NOT NULL DEFAULT ''::text,
  decision_note      text NOT NULL DEFAULT ''::text,
  expires_at         bigint NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  decided_at         text NOT NULL DEFAULT ''::text,
  PRIMARY KEY (id),
  CONSTRAINT approval_requests_action_check CHECK ((action = ANY (ARRAY['upsert'::text, 'delete'::text, 'rollback'::text]))),
  CONSTRAINT approval_requests_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT approval_requests_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'applied'::text, 'rejected'::text, 'expired'::text, 'cancelled'::text])))
);

-- access_requests
CREATE TABLE access_requests (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  action             text NOT NULL,
  requester_id       text NOT NULL,
  reason             text NOT NULL DEFAULT ''::text,
  status             text NOT NULL,
  approver_id        text NOT NULL DEFAULT ''::text,
  decision_note      text NOT NULL DEFAULT ''::text,
  ttl_seconds        integer NOT NULL,
  expires_at         bigint NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  decided_at         text NOT NULL DEFAULT ''::text,
  PRIMARY KEY (id),
  CONSTRAINT access_requests_action_check CHECK ((action = ANY (ARRAY['reveal'::text, 'export'::text, 'write'::text, 'delete'::text, 'rollback'::text]))),
  CONSTRAINT access_requests_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT access_requests_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'expired'::text, 'revoked'::text])))
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_access_active ON access_requests (org_id, requester_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_approval_org ON approval_requests (org_id, status, expires_at);
CREATE INDEX IF NOT EXISTS idx_rotation_due ON rotation_policies (enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_scan_reports_org ON scan_reports (org_id, created_at);
