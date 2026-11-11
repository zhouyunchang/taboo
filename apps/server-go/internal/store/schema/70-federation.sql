-- OIDC / 浏览器会话 / 外部身份

-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- oidc_providers
DROP TABLE IF EXISTS oidc_providers CASCADE;
CREATE TABLE oidc_providers (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  issuer             text NOT NULL,
  client_id          text NOT NULL,
  client_secret_enc  text NOT NULL,
  scopes             text NOT NULL DEFAULT 'openid,email,profile'::text,
  role_claim         text NOT NULL DEFAULT 'groups'::text,
  role_map           text NOT NULL DEFAULT '{}'::text,
  default_role       text NOT NULL DEFAULT 'viewer'::text,
  username_claim     text NOT NULL DEFAULT 'email'::text,
  public_login       bigint NOT NULL DEFAULT 0,
  autocreate         bigint NOT NULL DEFAULT 1,
  sync_role          bigint NOT NULL DEFAULT 1,
  enabled            bigint NOT NULL DEFAULT 1,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT oidc_providers_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT provider_id_org UNIQUE (id, org_id)
);

-- provider_security
DROP TABLE IF EXISTS provider_security CASCADE;
CREATE TABLE provider_security (
  provider_id        text NOT NULL,
  org_id             text NOT NULL,
  configuration      text NOT NULL,
  PRIMARY KEY (provider_id),
  CONSTRAINT provider_security_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT provider_security_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES oidc_providers(id) ON DELETE CASCADE,
  CONSTRAINT security_provider_org FOREIGN KEY (provider_id, org_id) REFERENCES oidc_providers(id, org_id) ON DELETE CASCADE
);

-- external_identities
DROP TABLE IF EXISTS external_identities CASCADE;
CREATE TABLE external_identities (
  issuer             text NOT NULL,
  subject            text NOT NULL,
  user_id            text NOT NULL,
  PRIMARY KEY (issuer, subject),
  CONSTRAINT external_identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT external_identities_user_id_key UNIQUE (user_id)
);

-- federation_admissions
DROP TABLE IF EXISTS federation_admissions CASCADE;
CREATE TABLE federation_admissions (
  org_id             text NOT NULL,
  provider_id        text NOT NULL,
  user_id            text NOT NULL,
  last_role          text NOT NULL DEFAULT ''::text,
  PRIMARY KEY (org_id, user_id),
  CONSTRAINT federation_admissions_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT federation_admissions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id)
);

-- oidc_login_requests
DROP TABLE IF EXISTS oidc_login_requests CASCADE;
CREATE TABLE oidc_login_requests (
  state_hash         text NOT NULL,
  browser_hash       text NOT NULL,
  provider_id        text NOT NULL,
  config_hash        text NOT NULL,
  verifier_enc       text NOT NULL,
  nonce              text NOT NULL,
  redirect_uri       text NOT NULL,
  expires_at         bigint NOT NULL,
  PRIMARY KEY (state_hash),
  CONSTRAINT oidc_login_requests_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES oidc_providers(id) ON DELETE CASCADE
);

-- browser_sessions
DROP TABLE IF EXISTS browser_sessions CASCADE;
CREATE TABLE browser_sessions (
  token_hash         text NOT NULL,
  user_id            text NOT NULL,
  org_id             text NOT NULL,
  provider_id        text NOT NULL,
  config_hash        text NOT NULL,
  origin             text NOT NULL,
  expires_at         bigint NOT NULL,
  created_at         bigint NOT NULL,
  sid                text NOT NULL DEFAULT ''::text,
  subject            text NOT NULL DEFAULT ''::text,
  PRIMARY KEY (token_hash),
  CONSTRAINT browser_sessions_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT browser_sessions_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_members(org_id, user_id) ON DELETE CASCADE,
  CONSTRAINT browser_sessions_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES oidc_providers(id) ON DELETE CASCADE,
  CONSTRAINT browser_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT browser_token_org UNIQUE (token_hash, org_id)
);

-- browser_credentials
DROP TABLE IF EXISTS browser_credentials CASCADE;
CREATE TABLE browser_credentials (
  token_hash         text NOT NULL,
  org_id             text NOT NULL,
  ciphertext         text NOT NULL,
  revision           integer NOT NULL DEFAULT 1,
  refresh_state      text NOT NULL DEFAULT ''::text,
  PRIMARY KEY (token_hash),
  CONSTRAINT browser_credentials_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT browser_credentials_refresh_state_check CHECK ((refresh_state = ANY (ARRAY[''::text, 'inflight'::text]))),
  CONSTRAINT browser_credentials_token_hash_fkey FOREIGN KEY (token_hash) REFERENCES browser_sessions(token_hash) ON DELETE CASCADE,
  CONSTRAINT credentials_session_org FOREIGN KEY (token_hash, org_id) REFERENCES browser_sessions(token_hash, org_id) ON DELETE CASCADE
);

-- logout_replays
DROP TABLE IF EXISTS logout_replays CASCADE;
CREATE TABLE logout_replays (
  token_hash         text NOT NULL,
  provider_id        text NOT NULL,
  expires_at         bigint NOT NULL,
  PRIMARY KEY (token_hash)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_browser_logout_sid ON browser_sessions (provider_id, sid);
CREATE INDEX IF NOT EXISTS idx_browser_logout_sub ON browser_sessions (provider_id, subject);
CREATE INDEX IF NOT EXISTS idx_browser_session_exp ON browser_sessions (expires_at);
CREATE INDEX IF NOT EXISTS idx_oidc_org ON oidc_providers (org_id);
CREATE INDEX IF NOT EXISTS idx_oidc_request_exp ON oidc_login_requests (expires_at);
