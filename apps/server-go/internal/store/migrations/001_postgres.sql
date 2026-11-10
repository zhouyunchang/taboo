-- PostgreSQL production baseline. Applied transactionally under an advisory lock.

CREATE TABLE IF NOT EXISTS users (
  id               TEXT PRIMARY KEY,
  email            TEXT NOT NULL UNIQUE,
  name             TEXT NOT NULL DEFAULT '',
  password_hash    TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'active',
  auth_source      TEXT NOT NULL DEFAULT 'local',
  external_sub     TEXT NOT NULL DEFAULT '',
  totp_pending_enc TEXT,
  totp_secret_enc  TEXT,
  totp_enabled     BIGINT NOT NULL DEFAULT 0,
  totp_last_step   BIGINT NOT NULL DEFAULT 0,
  created_at       TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

CREATE TABLE IF NOT EXISTS orgs (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  slug          TEXT NOT NULL UNIQUE,
  dek_encrypted TEXT NOT NULL,
  kind          TEXT NOT NULL DEFAULT 'organization',
  owner_user_id TEXT REFERENCES users(id),
  created_at    TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  CONSTRAINT orgs_kind_check CHECK (kind IN ('personal','organization'))
);
CREATE UNIQUE INDEX IF NOT EXISTS orgs_one_personal_owner ON orgs(owner_user_id) WHERE kind = 'personal';

CREATE TABLE IF NOT EXISTS org_members (
  org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  restricted BIGINT NOT NULL DEFAULT 0,
  joined_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS projects (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  slug       TEXT NOT NULL,
  approval_required INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  UNIQUE (org_id, slug)
);

-- 多级文件夹：物化路径 /a/b/
CREATE TABLE IF NOT EXISTS folders (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  parent_id  TEXT REFERENCES folders(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  path       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  UNIQUE (project_id, path)
);


CREATE TABLE IF NOT EXISTS secrets (
  id             TEXT PRIMARY KEY,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  folder         TEXT NOT NULL DEFAULT '/',
  folder_id      TEXT REFERENCES folders(id),
  key            TEXT NOT NULL,
  comment        TEXT NOT NULL DEFAULT '',
  tags           TEXT NOT NULL DEFAULT '[]',
  latest_version BIGINT NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  updated_at     TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  UNIQUE (project_id, folder, key)
);

CREATE TABLE IF NOT EXISTS secret_versions (
  id         TEXT PRIMARY KEY,
  secret_id  TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
  version    BIGINT NOT NULL,
  ciphertext TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  UNIQUE (secret_id, version)
);

CREATE TABLE IF NOT EXISTS audit_logs (
  rowid BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  actor_name  TEXT NOT NULL DEFAULT '',
  actor_type  TEXT NOT NULL DEFAULT 'user',
  action      TEXT NOT NULL,
  resource    TEXT NOT NULL,
  metadata    TEXT NOT NULL DEFAULT '{}',
  ip          TEXT NOT NULL DEFAULT '',
  user_agent  TEXT NOT NULL DEFAULT '',
  request_id  TEXT NOT NULL DEFAULT '',
  prev_hash   TEXT NOT NULL DEFAULT '',
  entry_hash  TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_audit_org_time ON audit_logs(org_id, created_at);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at BIGINT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

CREATE TABLE IF NOT EXISTS machine_identities (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  auth_type   TEXT NOT NULL DEFAULT 'client_credentials',
  client_id   TEXT NOT NULL UNIQUE,
  secret_hash TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  token_ttl   BIGINT NOT NULL DEFAULT 900,
  created_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

-- 作用域：显式 (项目, 权限)，禁止通配
CREATE TABLE IF NOT EXISTS identity_scopes (
  identity_id TEXT NOT NULL REFERENCES machine_identities(id) ON DELETE CASCADE,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  permission  TEXT NOT NULL CHECK (permission IN ('read','reveal','write')),
  PRIMARY KEY (identity_id, project_id)
);

-- TOTP 2FA（M2 #4）：恢复码仅存 sha256 哈希，用后即废
CREATE TABLE IF NOT EXISTS recovery_codes (
  id         TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL UNIQUE,
  used_at    TEXT,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

-- Secret Sync（M5 #11）：同步目标，平台凭证 config 经 Master Key 信封加密
CREATE TABLE IF NOT EXISTS sync_targets (
  id           TEXT PRIMARY KEY,
  org_id       TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  platform     TEXT NOT NULL CHECK (platform IN ('github','vercel','cloudflare')),
  project_id   TEXT REFERENCES projects(id) ON DELETE CASCADE,
  config_enc   TEXT NOT NULL,
  enabled      BIGINT NOT NULL DEFAULT 1,
  created_at   TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_sync_targets_org ON sync_targets(org_id);

-- 同步运行记录（推送内容指纹 sha256，不记明文）
CREATE TABLE IF NOT EXISTS sync_runs (
  id           TEXT PRIMARY KEY,
  target_id    TEXT NOT NULL REFERENCES sync_targets(id) ON DELETE CASCADE,
  org_id       TEXT NOT NULL,
  event_id     TEXT NOT NULL DEFAULT '',
  secret_key   TEXT NOT NULL,
  action       TEXT NOT NULL,
  fingerprint  TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','dead')),
  trigger_type TEXT NOT NULL DEFAULT 'event',
  attempts     BIGINT NOT NULL DEFAULT 0,
  next_attempt_at BIGINT NOT NULL DEFAULT 0,
  error        TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  updated_at   TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_sync_runs_target ON sync_runs(target_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sync_due ON sync_runs(status, next_attempt_at);

-- Webhooks（M5 #12）：订阅；签名密钥 secret 经 Master Key 加密
CREATE TABLE IF NOT EXISTS webhooks (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,
  secret_enc  TEXT NOT NULL,
  events      TEXT NOT NULL DEFAULT '[]',
  active      BIGINT NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_webhooks_org ON webhooks(org_id);

-- Webhook 投递记录（至少一次 + 指数退避，payload 不含明文）
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id             TEXT PRIMARY KEY,
  webhook_id     TEXT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
  org_id         TEXT NOT NULL,
  event_id       TEXT NOT NULL DEFAULT '',
  event          TEXT NOT NULL,
  payload        TEXT NOT NULL,
  status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','dead')),
  http_code      BIGINT NOT NULL DEFAULT 0,
  error          TEXT NOT NULL DEFAULT '',
  attempts       BIGINT NOT NULL DEFAULT 0,
  next_attempt_at BIGINT NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  delivered_at   TEXT
);
CREATE INDEX IF NOT EXISTS idx_whd_due ON webhook_deliveries(status, next_attempt_at);

-- OIDC SSO（M5 #13）：组织级 IdP 配置；client_secret 经 Master Key 加密
CREATE TABLE IF NOT EXISTS oidc_providers (
  id                TEXT PRIMARY KEY,
  org_id            TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name              TEXT NOT NULL,
  issuer            TEXT NOT NULL,
  client_id         TEXT NOT NULL,
  client_secret_enc TEXT NOT NULL,
  scopes            TEXT NOT NULL DEFAULT 'openid,email,profile',
  role_claim        TEXT NOT NULL DEFAULT 'groups',
  role_map          TEXT NOT NULL DEFAULT '{}',
  default_role      TEXT NOT NULL DEFAULT 'viewer',
  username_claim    TEXT NOT NULL DEFAULT 'email',
  public_login      BIGINT NOT NULL DEFAULT 0,
  autocreate        BIGINT NOT NULL DEFAULT 1,
  sync_role         BIGINT NOT NULL DEFAULT 1,
  enabled           BIGINT NOT NULL DEFAULT 1,
  created_at        TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_oidc_org ON oidc_providers(org_id);

-- 审计导出任务（M5 #14）：大结果集异步生成 + 下载链接
CREATE TABLE IF NOT EXISTS audit_exports (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  actor_id    TEXT NOT NULL,
  format      TEXT NOT NULL CHECK (format IN ('csv','jsonl')),
  filters     TEXT NOT NULL DEFAULT '{}',
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','failed')),
  file_path   TEXT NOT NULL DEFAULT '',
  row_count   BIGINT NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_exports_org ON audit_exports(org_id);

-- 动态密钥（M4 #9）：高权限连接串 Master Key 加密自举；lease 绑定机器身份
CREATE TABLE IF NOT EXISTS dynamic_engines (
  id             TEXT PRIMARY KEY,
  org_id         TEXT NOT NULL,
  project_id     TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  type           TEXT NOT NULL DEFAULT 'postgres' CHECK (type IN ('postgres','mysql')),
  conn_encrypted TEXT NOT NULL,
  database       TEXT NOT NULL DEFAULT 'postgres',
  default_ttl    BIGINT NOT NULL DEFAULT 3600,
  max_ttl        BIGINT NOT NULL DEFAULT 86400,
  created_at     TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

CREATE TABLE IF NOT EXISTS dynamic_leases (
  id                 TEXT PRIMARY KEY,
  engine_id          TEXT NOT NULL REFERENCES dynamic_engines(id) ON DELETE CASCADE,
  identity_id        TEXT NOT NULL,
  username           TEXT NOT NULL UNIQUE,
  password_encrypted TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','revoked','failed','revoking')),
  reclaim_fails      BIGINT NOT NULL DEFAULT 0,
  expires_at         BIGINT NOT NULL,
  created_at         TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS')),
  revoked_at         TEXT
);

CREATE TABLE IF NOT EXISTS org_invites (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  email       TEXT NOT NULL,
  role        TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  token_hash  TEXT NOT NULL UNIQUE,
  expires_at  BIGINT NOT NULL,
  created_by  TEXT NOT NULL,
  created_at  TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);

CREATE TABLE IF NOT EXISTS project_grants (
  org_id     TEXT NOT NULL,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL CHECK (role IN ('owner','admin','developer','viewer')),
  PRIMARY KEY (project_id, user_id)
);

CREATE TABLE IF NOT EXISTS outbox_events (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL,
  event_id   TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'secret',
  payload    TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','done','dead')),
  error      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox_events(status, created_at);

CREATE TABLE IF NOT EXISTS audit_checkpoints (
  id         TEXT PRIMARY KEY,
  org_id     TEXT NOT NULL,
  last_id    TEXT NOT NULL,
  last_hash  TEXT NOT NULL,
  hmac       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (to_char(timezone('UTC', clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'))
);
CREATE INDEX IF NOT EXISTS idx_audit_cp_org ON audit_checkpoints(org_id, created_at);

CREATE TABLE IF NOT EXISTS external_identities (
 issuer TEXT NOT NULL, subject TEXT NOT NULL,
 user_id TEXT NOT NULL UNIQUE REFERENCES users(id),
 PRIMARY KEY(issuer,subject)
);
CREATE TABLE IF NOT EXISTS federation_admissions (
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(id),
 last_role TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(org_id,user_id)
);
CREATE TABLE IF NOT EXISTS oidc_login_requests (
 state_hash TEXT PRIMARY KEY, browser_hash TEXT NOT NULL,
 provider_id TEXT NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
 config_hash TEXT NOT NULL, verifier_enc TEXT NOT NULL, nonce TEXT NOT NULL,
 redirect_uri TEXT NOT NULL, expires_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oidc_request_exp ON oidc_login_requests(expires_at);
CREATE TABLE IF NOT EXISTS browser_sessions (
 token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
 config_hash TEXT NOT NULL, origin TEXT NOT NULL,
 expires_at BIGINT NOT NULL, created_at BIGINT NOT NULL,
 FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_browser_session_exp ON browser_sessions(expires_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_secrets_project_folder_key ON secrets(project_id, folder_id, key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_external ON users(auth_source,external_sub) WHERE external_sub <> '';
CREATE TABLE IF NOT EXISTS schema_version (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton), version BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS audit_heads (org_id TEXT PRIMARY KEY, revision BIGINT NOT NULL DEFAULT 0);
ALTER TABLE projects ADD CONSTRAINT projects_id_org UNIQUE(id,org_id);
ALTER TABLE folders ADD CONSTRAINT folders_id_project UNIQUE(id,project_id);
ALTER TABLE folders ADD CONSTRAINT folders_parent_project FOREIGN KEY(parent_id,project_id) REFERENCES folders(id,project_id);
ALTER TABLE secrets ADD CONSTRAINT secrets_folder_project FOREIGN KEY(folder_id,project_id) REFERENCES folders(id,project_id);
ALTER TABLE project_grants ADD CONSTRAINT grants_project_org FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE;
ALTER TABLE project_grants ADD CONSTRAINT grants_member_org FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE;
ALTER TABLE dynamic_engines ADD CONSTRAINT engines_project_org FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE;
ALTER TABLE sync_targets ADD CONSTRAINT targets_project_org FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE;
ALTER TABLE sync_targets ADD CONSTRAINT targets_id_org UNIQUE(id,org_id);
ALTER TABLE sync_runs ADD CONSTRAINT runs_target_org FOREIGN KEY(target_id,org_id) REFERENCES sync_targets(id,org_id) ON DELETE CASCADE;
ALTER TABLE webhooks ADD CONSTRAINT webhooks_id_org UNIQUE(id,org_id);
ALTER TABLE webhook_deliveries ADD CONSTRAINT deliveries_webhook_org FOREIGN KEY(webhook_id,org_id) REFERENCES webhooks(id,org_id) ON DELETE CASCADE;

CREATE FUNCTION check_identity_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM projects p JOIN machine_identities m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND m.id=NEW.identity_id) THEN
  RAISE EXCEPTION 'invalid resource ownership' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER integrity_identity_scopes BEFORE INSERT OR UPDATE ON identity_scopes FOR EACH ROW EXECUTE FUNCTION check_identity_scope();

CREATE FUNCTION immutable_ownership() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF to_jsonb(NEW)->>'id' IS DISTINCT FROM to_jsonb(OLD)->>'id' OR to_jsonb(NEW)->>TG_ARGV[0] IS DISTINCT FROM to_jsonb(OLD)->>TG_ARGV[0] THEN
  RAISE EXCEPTION 'resource ownership is immutable' USING ERRCODE='23503';
 END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER immutable_projects BEFORE UPDATE ON projects FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
CREATE TRIGGER immutable_folders BEFORE UPDATE ON folders FOR EACH ROW EXECUTE FUNCTION immutable_ownership('project_id');
CREATE TRIGGER immutable_secrets BEFORE UPDATE ON secrets FOR EACH ROW EXECUTE FUNCTION immutable_ownership('project_id');
CREATE TRIGGER immutable_machine_identities BEFORE UPDATE ON machine_identities FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
CREATE TRIGGER immutable_sync_targets BEFORE UPDATE ON sync_targets FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
CREATE TRIGGER immutable_webhooks BEFORE UPDATE ON webhooks FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
CREATE TRIGGER immutable_dynamic_engines BEFORE UPDATE ON dynamic_engines FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');

ALTER TABLE sync_runs ADD COLUMN event_payload TEXT NOT NULL DEFAULT '{}';
ALTER TABLE sync_runs ADD COLUMN lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_runs ADD COLUMN lease_until BIGINT NOT NULL DEFAULT 0;
ALTER TABLE webhook_deliveries ADD COLUMN lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE webhook_deliveries ADD COLUMN lease_until BIGINT NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS mutation_receipts (org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE, actor_id TEXT NOT NULL, request_key TEXT NOT NULL, request_hash TEXT NOT NULL, response TEXT NOT NULL, PRIMARY KEY(org_id,actor_id,request_key));
