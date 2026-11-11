-- +goose Up

CREATE TABLE machine_identities (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  auth_type   TEXT NOT NULL DEFAULT 'client_credentials',
  client_id   TEXT NOT NULL UNIQUE,
  secret_hash TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  token_ttl          INTEGER NOT NULL DEFAULT 900,
  allowed_cidrs      TEXT NOT NULL DEFAULT '',
  list_scan_reports  INTEGER NOT NULL DEFAULT 0,
  created_at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE identity_scopes (
  identity_id TEXT NOT NULL REFERENCES machine_identities(id) ON DELETE CASCADE,
  project_id  TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  permission  TEXT NOT NULL CHECK (permission IN ('read','reveal','write')),
  PRIMARY KEY (identity_id, project_id)
);

CREATE TABLE oauth_trusts (
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

CREATE TABLE workload_bindings (
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

CREATE TABLE workload_grants (
 binding_id TEXT NOT NULL REFERENCES workload_bindings(id) ON DELETE CASCADE,
 org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 action TEXT NOT NULL CHECK(action IN ('read','reveal','write','export','rollback','delete')),
 required_scope TEXT NOT NULL CHECK(required_scope<>''),
 PRIMARY KEY(binding_id,project_id,action),
 FOREIGN KEY(binding_id,org_id) REFERENCES workload_bindings(id,org_id) ON DELETE CASCADE
);

CREATE TRIGGER integrity_workload_bindings_INSERT BEFORE INSERT ON workload_bindings WHEN NOT (EXISTS(SELECT 1 FROM machine_identities m WHERE m.id=NEW.id AND m.org_id=NEW.org_id AND m.auth_type='external_oauth')) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_workload_bindings_UPDATE BEFORE UPDATE ON workload_bindings WHEN NOT (EXISTS(SELECT 1 FROM machine_identities m WHERE m.id=NEW.id AND m.org_id=NEW.org_id AND m.auth_type='external_oauth')) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_workload_grants_INSERT BEFORE INSERT ON workload_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_workload_grants_UPDATE BEFORE UPDATE ON workload_grants WHEN NOT (EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_identity_scopes_INSERT BEFORE INSERT ON identity_scopes WHEN NOT (EXISTS(SELECT 1 FROM projects p JOIN machine_identities m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND m.id=NEW.identity_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER integrity_identity_scopes_UPDATE BEFORE UPDATE ON identity_scopes WHEN NOT (EXISTS(SELECT 1 FROM projects p JOIN machine_identities m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND m.id=NEW.identity_id)) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END;

CREATE TRIGGER immutable_machine_identities BEFORE UPDATE ON machine_identities WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_workload_bindings BEFORE UPDATE ON workload_bindings WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_oauth_trusts BEFORE UPDATE ON oauth_trusts WHEN NEW.id<>OLD.id OR NEW.org_id<>OLD.org_id BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END;

CREATE TRIGGER immutable_machine_auth BEFORE UPDATE ON machine_identities WHEN NEW.auth_type<>OLD.auth_type BEGIN SELECT RAISE(ABORT,'authentication source is immutable'); END;

CREATE TRIGGER immutable_workload_binding_trust BEFORE UPDATE ON workload_bindings WHEN NEW.trust_id<>OLD.trust_id BEGIN SELECT RAISE(ABORT,'workload trust is immutable'); END;
