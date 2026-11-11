-- +goose Up

CREATE TABLE oidc_providers (
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
  public_login      INTEGER NOT NULL DEFAULT 0,
  autocreate        INTEGER NOT NULL DEFAULT 1,
  sync_role         INTEGER NOT NULL DEFAULT 1,
  enabled           INTEGER NOT NULL DEFAULT 1,
  created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE external_identities (
 issuer TEXT NOT NULL, subject TEXT NOT NULL,
 user_id TEXT NOT NULL UNIQUE REFERENCES users(id),
 PRIMARY KEY(issuer,subject)
);

CREATE TABLE federation_admissions (
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL,
 user_id TEXT NOT NULL REFERENCES users(id),
 last_role TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(org_id,user_id)
);

CREATE TABLE oidc_login_requests (
 state_hash TEXT PRIMARY KEY, browser_hash TEXT NOT NULL,
 provider_id TEXT NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
 config_hash TEXT NOT NULL, verifier_enc TEXT NOT NULL, nonce TEXT NOT NULL,
 redirect_uri TEXT NOT NULL, expires_at INTEGER NOT NULL
);

CREATE TABLE browser_sessions (
 token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
 config_hash TEXT NOT NULL, origin TEXT NOT NULL,
 expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL, sid TEXT NOT NULL DEFAULT '', subject TEXT NOT NULL DEFAULT '',
 FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE
);

CREATE TABLE provider_security (
 provider_id TEXT PRIMARY KEY REFERENCES oidc_providers(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 configuration TEXT NOT NULL
);

CREATE TABLE browser_credentials (
 token_hash TEXT PRIMARY KEY REFERENCES browser_sessions(token_hash) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 ciphertext TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 refresh_state TEXT NOT NULL DEFAULT '' CHECK(refresh_state IN ('','inflight'))
);

CREATE TABLE logout_replays (
  token_hash TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL,
  expires_at BIGINT NOT NULL
);

CREATE INDEX idx_oidc_org ON oidc_providers(org_id);

CREATE INDEX idx_oidc_request_exp ON oidc_login_requests(expires_at);

CREATE INDEX idx_browser_session_exp ON browser_sessions(expires_at);

CREATE TRIGGER integrity_provider_security_INSERT BEFORE INSERT ON provider_security WHEN NOT (EXISTS(SELECT 1 FROM oidc_providers p WHERE p.id=NEW.provider_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_provider_security_UPDATE BEFORE UPDATE ON provider_security WHEN NOT (EXISTS(SELECT 1 FROM oidc_providers p WHERE p.id=NEW.provider_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_browser_credentials_INSERT BEFORE INSERT ON browser_credentials WHEN NOT (EXISTS(SELECT 1 FROM browser_sessions s WHERE s.token_hash=NEW.token_hash AND s.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_browser_credentials_UPDATE BEFORE UPDATE ON browser_credentials WHEN NOT (EXISTS(SELECT 1 FROM browser_sessions s WHERE s.token_hash=NEW.token_hash AND s.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE INDEX idx_browser_logout_sid ON browser_sessions(provider_id, sid);

CREATE INDEX idx_browser_logout_sub ON browser_sessions(provider_id, subject);
