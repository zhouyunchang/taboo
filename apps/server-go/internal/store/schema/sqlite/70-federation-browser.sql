CREATE TABLE IF NOT EXISTS provider_security (
 provider_id TEXT PRIMARY KEY REFERENCES oidc_providers(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 configuration TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS browser_credentials (
 token_hash TEXT PRIMARY KEY REFERENCES browser_sessions(token_hash) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 ciphertext TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1,
 refresh_state TEXT NOT NULL DEFAULT '' CHECK(refresh_state IN ('','inflight'))
);
