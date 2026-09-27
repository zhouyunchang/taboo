package store

// Additive schema: existing organizations and secrets retain their IDs. A
// tombstoned admission is retained so JIT cannot recreate revoked membership.
func migrateFederation(db migrationDB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS mutation_receipts (org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE, actor_id TEXT NOT NULL, request_key TEXT NOT NULL, request_hash TEXT NOT NULL, response TEXT NOT NULL, PRIMARY KEY(org_id,actor_id,request_key));
CREATE TABLE IF NOT EXISTS audit_heads (org_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0);
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
 redirect_uri TEXT NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oidc_request_exp ON oidc_login_requests(expires_at);
CREATE TABLE IF NOT EXISTS browser_sessions (
 token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
 config_hash TEXT NOT NULL, origin TEXT NOT NULL,
 expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
 FOREIGN KEY(org_id,user_id) REFERENCES org_members(org_id,user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_browser_session_exp ON browser_sessions(expires_at);
`)
	return err
}
