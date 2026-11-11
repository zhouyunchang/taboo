CREATE TABLE IF NOT EXISTS oauth_trusts (
 id TEXT PRIMARY KEY,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 issuer TEXT NOT NULL,
 audience TEXT NOT NULL,
 metadata_mode TEXT NOT NULL CHECK(metadata_mode IN ('oauth','oidc')),
 token_profile TEXT NOT NULL CHECK(token_profile IN ('JWT_RFC9068','INTROSPECTION_RFC7662')),
 client_id TEXT NOT NULL DEFAULT '',
 client_secret_enc TEXT NOT NULL DEFAULT '',
 max_lifetime INTEGER NOT NULL DEFAULT 900 CHECK(max_lifetime BETWEEN 60 AND 900),
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 UNIQUE(id,org_id)
);
CREATE TABLE IF NOT EXISTS workload_bindings (
 id TEXT PRIMARY KEY REFERENCES machine_identities(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 trust_id TEXT NOT NULL,
 subject TEXT NOT NULL,
 client_id TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
 UNIQUE(trust_id,subject,client_id),
 UNIQUE(id,org_id),
 FOREIGN KEY(trust_id,org_id) REFERENCES oauth_trusts(id,org_id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS workload_grants (
 binding_id TEXT NOT NULL REFERENCES workload_bindings(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN ('read','reveal','write','export','rollback','delete')),
 required_scope TEXT NOT NULL CHECK(required_scope<>''),
 PRIMARY KEY(binding_id,project_id,action),
 FOREIGN KEY(binding_id,org_id) REFERENCES workload_bindings(id,org_id) ON DELETE CASCADE
);
