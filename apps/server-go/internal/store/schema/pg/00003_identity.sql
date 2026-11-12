-- 机器身份与外部工作负载
-- +goose Up


-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- machine_identities
CREATE TABLE machine_identities (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  auth_type          text NOT NULL DEFAULT 'client_credentials'::text,
  client_id          text NOT NULL,
  secret_hash        text NOT NULL,
  status             text NOT NULL DEFAULT 'active'::text,
  token_ttl          bigint NOT NULL DEFAULT 900,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  allowed_cidrs      text NOT NULL DEFAULT ''::text,
  list_scan_reports  integer NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT identities_id_org UNIQUE (id, org_id),
  CONSTRAINT machine_identities_client_id_key UNIQUE (client_id),
  CONSTRAINT machine_identities_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT machine_identities_status_check CHECK ((status = ANY (ARRAY['active'::text, 'revoked'::text])))
);

-- identity_scopes
CREATE TABLE identity_scopes (
  identity_id        text NOT NULL,
  project_id         text NOT NULL,
  permission         text NOT NULL,
  PRIMARY KEY (identity_id, project_id),
  CONSTRAINT identity_scopes_identity_id_fkey FOREIGN KEY (identity_id) REFERENCES machine_identities(id) ON DELETE CASCADE,
  CONSTRAINT identity_scopes_permission_check CHECK ((permission = ANY (ARRAY['read'::text, 'reveal'::text, 'write'::text]))),
  CONSTRAINT identity_scopes_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- oauth_trusts
CREATE TABLE oauth_trusts (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  issuer             text NOT NULL,
  audience           text NOT NULL,
  metadata_mode      text NOT NULL,
  token_profile      text NOT NULL,
  client_id          text NOT NULL DEFAULT ''::text,
  client_secret_enc  text NOT NULL DEFAULT ''::text,
  max_lifetime       integer NOT NULL DEFAULT 900,
  enabled            integer NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  CONSTRAINT oauth_trusts_enabled_check CHECK ((enabled = ANY (ARRAY[0, 1]))),
  CONSTRAINT oauth_trusts_id_org_id_key UNIQUE (id, org_id),
  CONSTRAINT oauth_trusts_max_lifetime_check CHECK (((max_lifetime >= 60) AND (max_lifetime <= 900))),
  CONSTRAINT oauth_trusts_metadata_mode_check CHECK ((metadata_mode = ANY (ARRAY['oauth'::text, 'oidc'::text]))),
  CONSTRAINT oauth_trusts_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT oauth_trusts_token_profile_check CHECK ((token_profile = ANY (ARRAY['JWT_RFC9068'::text, 'INTROSPECTION_RFC7662'::text])))
);

-- workload_bindings
CREATE TABLE workload_bindings (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  trust_id           text NOT NULL,
  subject            text NOT NULL,
  client_id          text NOT NULL,
  enabled            integer NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  CONSTRAINT workload_bindings_enabled_check CHECK ((enabled = ANY (ARRAY[0, 1]))),
  CONSTRAINT workload_bindings_id_fkey FOREIGN KEY (id) REFERENCES machine_identities(id) ON DELETE CASCADE,
  CONSTRAINT workload_bindings_id_org_id_key UNIQUE (id, org_id),
  CONSTRAINT workload_bindings_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT workload_bindings_trust_id_org_id_fkey FOREIGN KEY (trust_id, org_id) REFERENCES oauth_trusts(id, org_id) ON DELETE CASCADE,
  CONSTRAINT workload_bindings_trust_id_subject_client_id_key UNIQUE (trust_id, subject, client_id),
  CONSTRAINT workloads_machine_org FOREIGN KEY (id, org_id) REFERENCES machine_identities(id, org_id) ON DELETE CASCADE
);

-- workload_grants
CREATE TABLE workload_grants (
  binding_id         text NOT NULL,
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  action             text NOT NULL,
  required_scope     text NOT NULL,
  PRIMARY KEY (binding_id, project_id, action),
  CONSTRAINT workload_grants_action_check CHECK ((action = ANY (ARRAY['read'::text, 'reveal'::text, 'write'::text, 'export'::text, 'rollback'::text, 'delete'::text]))),
  CONSTRAINT workload_grants_binding_id_fkey FOREIGN KEY (binding_id) REFERENCES workload_bindings(id) ON DELETE CASCADE,
  CONSTRAINT workload_grants_binding_id_org_id_fkey FOREIGN KEY (binding_id, org_id) REFERENCES workload_bindings(id, org_id) ON DELETE CASCADE,
  CONSTRAINT workload_grants_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT workload_grants_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT workload_grants_required_scope_check CHECK ((required_scope <> ''::text)),
  CONSTRAINT workloads_project_org FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE
);
