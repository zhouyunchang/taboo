-- 项目、目录、密钥与权限基线

-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- projects
DROP TABLE IF EXISTS projects CASCADE;
CREATE TABLE projects (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  slug               text NOT NULL,
  approval_required  integer NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT projects_id_org UNIQUE (id, org_id),
  CONSTRAINT projects_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT projects_org_id_slug_key UNIQUE (org_id, slug)
);

-- folders
DROP TABLE IF EXISTS folders CASCADE;
CREATE TABLE folders (
  id                 text NOT NULL,
  project_id         text NOT NULL,
  parent_id          text,
  name               text NOT NULL,
  path               text NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT folders_id_project UNIQUE (id, project_id),
  CONSTRAINT folders_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES folders(id) ON DELETE CASCADE,
  CONSTRAINT folders_parent_project FOREIGN KEY (parent_id, project_id) REFERENCES folders(id, project_id),
  CONSTRAINT folders_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT folders_project_id_path_key UNIQUE (project_id, path)
);

-- secrets
DROP TABLE IF EXISTS secrets CASCADE;
CREATE TABLE secrets (
  id                 text NOT NULL,
  project_id         text NOT NULL,
  folder             text NOT NULL DEFAULT '/'::text,
  folder_id          text,
  key                text NOT NULL,
  comment            text NOT NULL DEFAULT ''::text,
  tags               text NOT NULL DEFAULT '[]'::text,
  latest_version     bigint NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  updated_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT secrets_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES folders(id),
  CONSTRAINT secrets_folder_project FOREIGN KEY (folder_id, project_id) REFERENCES folders(id, project_id),
  CONSTRAINT secrets_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT secrets_project_id_folder_key_key UNIQUE (project_id, folder, key)
);

-- secret_versions
DROP TABLE IF EXISTS secret_versions CASCADE;
CREATE TABLE secret_versions (
  id                 text NOT NULL,
  secret_id          text NOT NULL,
  version            bigint NOT NULL,
  ciphertext         text NOT NULL,
  created_by         text NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT secret_versions_secret_id_fkey FOREIGN KEY (secret_id) REFERENCES secrets(id) ON DELETE CASCADE,
  CONSTRAINT secret_versions_secret_id_version_key UNIQUE (secret_id, version)
);

-- org_security
DROP TABLE IF EXISTS org_security CASCADE;
CREATE TABLE org_security (
  org_id             text NOT NULL,
  login_policy       text NOT NULL DEFAULT 'LOCAL_OR_EXTERNAL'::text,
  PRIMARY KEY (org_id),
  CONSTRAINT org_security_login_policy_check CHECK ((login_policy = ANY (ARRAY['LOCAL_OR_EXTERNAL'::text, 'EXTERNAL_REQUIRED'::text]))),
  CONSTRAINT org_security_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE
);

-- member_action_grants
DROP TABLE IF EXISTS member_action_grants CASCADE;
CREATE TABLE member_action_grants (
  org_id             text NOT NULL,
  user_id            text NOT NULL,
  project_id         text NOT NULL,
  action             text NOT NULL,
  PRIMARY KEY (org_id, user_id, project_id, action),
  CONSTRAINT member_action_grants_action_check CHECK ((action = ANY (ARRAY['read'::text, 'write'::text, 'reveal'::text, 'export'::text, 'rollback'::text, 'delete'::text]))),
  CONSTRAINT member_action_grants_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT member_action_grants_org_id_user_id_fkey FOREIGN KEY (org_id, user_id) REFERENCES org_members(org_id, user_id) ON DELETE CASCADE,
  CONSTRAINT member_action_grants_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT member_action_grants_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT member_actions_project_org FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE
);

-- project_grants
DROP TABLE IF EXISTS project_grants CASCADE;
CREATE TABLE project_grants (
  org_id             text NOT NULL,
  project_id         text NOT NULL,
  user_id            text NOT NULL,
  role               text NOT NULL,
  PRIMARY KEY (project_id, user_id),
  CONSTRAINT grants_member_org FOREIGN KEY (org_id, user_id) REFERENCES org_members(org_id, user_id) ON DELETE CASCADE,
  CONSTRAINT grants_project_org FOREIGN KEY (project_id, org_id) REFERENCES projects(id, org_id) ON DELETE CASCADE,
  CONSTRAINT project_grants_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
  CONSTRAINT project_grants_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'developer'::text, 'viewer'::text]))),
  CONSTRAINT project_grants_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- org_invites
DROP TABLE IF EXISTS org_invites CASCADE;
CREATE TABLE org_invites (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  email              text NOT NULL,
  role               text NOT NULL,
  token_hash         text NOT NULL,
  expires_at         bigint NOT NULL,
  created_by         text NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT org_invites_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT org_invites_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'developer'::text, 'viewer'::text]))),
  CONSTRAINT org_invites_token_hash_key UNIQUE (token_hash)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_folders_project_path ON folders (project_id, path);
CREATE INDEX IF NOT EXISTS idx_secrets_project_key ON secrets (project_id, folder, key);
CREATE UNIQUE INDEX IF NOT EXISTS idx_secrets_project_folder_key ON secrets (project_id, folder_id, key);
