-- 账号、组织、成员与会话令牌

-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- users
DROP TABLE IF EXISTS users CASCADE;
CREATE TABLE users (
  id                 text NOT NULL,
  email              text NOT NULL,
  name               text NOT NULL DEFAULT ''::text,
  password_hash      text NOT NULL,
  status             text NOT NULL DEFAULT 'active'::text,
  auth_source        text NOT NULL DEFAULT 'local'::text,
  external_sub       text NOT NULL DEFAULT ''::text,
  totp_pending_enc   text,
  totp_secret_enc    text,
  totp_enabled       bigint NOT NULL DEFAULT 0,
  totp_last_step     bigint NOT NULL DEFAULT 0,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  handle             text NOT NULL DEFAULT ''::text,
  handle_confirmed   integer NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  CONSTRAINT users_email_key UNIQUE (email)
);

-- orgs
DROP TABLE IF EXISTS orgs CASCADE;
CREATE TABLE orgs (
  id                 text NOT NULL,
  name               text NOT NULL,
  slug               text NOT NULL,
  dek_encrypted      text NOT NULL,
  kind               text NOT NULL DEFAULT 'organization'::text,
  owner_user_id      text,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT orgs_kind_check CHECK ((kind = ANY (ARRAY['personal'::text, 'organization'::text]))),
  CONSTRAINT orgs_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES users(id),
  CONSTRAINT orgs_slug_key UNIQUE (slug)
);

-- org_members
DROP TABLE IF EXISTS org_members CASCADE;
CREATE TABLE org_members (
  org_id             text NOT NULL,
  user_id            text NOT NULL,
  role               text NOT NULL,
  restricted         bigint NOT NULL DEFAULT 0,
  joined_at          text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  scim_managed       integer NOT NULL DEFAULT 0,
  PRIMARY KEY (org_id, user_id),
  CONSTRAINT org_members_org_id_fkey FOREIGN KEY (org_id) REFERENCES orgs(id) ON DELETE CASCADE,
  CONSTRAINT org_members_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'developer'::text, 'viewer'::text]))),
  CONSTRAINT org_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- refresh_tokens
DROP TABLE IF EXISTS refresh_tokens CASCADE;
CREATE TABLE refresh_tokens (
  id                 text NOT NULL,
  user_id            text NOT NULL,
  token_hash         text NOT NULL,
  expires_at         bigint NOT NULL,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT refresh_tokens_token_hash_key UNIQUE (token_hash),
  CONSTRAINT refresh_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- recovery_codes
DROP TABLE IF EXISTS recovery_codes CASCADE;
CREATE TABLE recovery_codes (
  id                 text NOT NULL,
  user_id            text NOT NULL,
  code_hash          text NOT NULL,
  used_at            text,
  created_at         text NOT NULL DEFAULT to_char(timezone('UTC'::text, clock_timestamp()), 'YYYY-MM-DD HH24:MI:SS'::text),
  PRIMARY KEY (id),
  CONSTRAINT recovery_codes_code_hash_key UNIQUE (code_hash),
  CONSTRAINT recovery_codes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- path_codes
DROP TABLE IF EXISTS path_codes CASCADE;
CREATE TABLE path_codes (
  code               text NOT NULL,
  kind               text NOT NULL,
  ref_id             text NOT NULL,
  PRIMARY KEY (code),
  CONSTRAINT path_codes_kind_check CHECK ((kind = ANY (ARRAY['user'::text, 'organization'::text])))
);

-- 索引
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_external ON users (auth_source, external_sub) WHERE (external_sub <> ''::text);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_handle ON users (handle) WHERE (handle <> ''::text);
CREATE UNIQUE INDEX IF NOT EXISTS orgs_one_personal_owner ON orgs (owner_user_id) WHERE (kind = 'personal'::text);
