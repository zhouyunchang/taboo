-- SCIM 2.0 用户与组
-- +goose Up


-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- scim_tokens
CREATE TABLE scim_tokens (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  name               text NOT NULL,
  token_hash         text NOT NULL,
  created_by         text NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  last_used_at       bigint NOT NULL DEFAULT 0,
  expires_at         bigint NOT NULL DEFAULT 0,
  revoked_at         bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT scim_tokens_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT scim_tokens_token_hash_key UNIQUE (token_hash)
);

-- scim_users
CREATE TABLE scim_users (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  user_id            text NOT NULL,
  external_id        text NOT NULL DEFAULT ''::text,
  user_name          text NOT NULL,
  active             integer NOT NULL DEFAULT 1,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  updated_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT scim_users_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT scim_users_org_id_user_name_key UNIQUE (org_id, user_name)
);

-- scim_groups
CREATE TABLE scim_groups (
  id                 text NOT NULL,
  org_id             text NOT NULL,
  display_name       text NOT NULL,
  external_id        text NOT NULL DEFAULT ''::text,
  role               text NOT NULL DEFAULT ''::text,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  updated_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT scim_groups_org_id_display_name_key UNIQUE (org_id, display_name),
  CONSTRAINT scim_groups_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT scim_groups_role_check CHECK ((role = ANY (ARRAY[''::text, 'viewer'::text, 'developer'::text, 'admin'::text])))
);

-- scim_group_members
CREATE TABLE scim_group_members (
  org_id             text NOT NULL,
  group_id           text NOT NULL,
  user_scim_id       text NOT NULL,
  PRIMARY KEY (group_id, user_scim_id)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_scim_members_user ON scim_group_members (org_id, user_scim_id);
CREATE INDEX IF NOT EXISTS idx_scim_tokens_org ON scim_tokens (org_id);
CREATE INDEX IF NOT EXISTS idx_scim_users_user ON scim_users (org_id, user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scim_groups_external ON scim_groups (org_id, external_id) WHERE (external_id <> ''::text);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scim_users_external ON scim_users (org_id, external_id) WHERE (external_id <> ''::text);
