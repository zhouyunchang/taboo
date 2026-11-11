-- 设备授权、限流与集群租约
-- +goose Up


-- 由 schema_version 23 的真实库结构整理；任何环境执行后应得到同一结构。

-- device_authorizations
CREATE TABLE device_authorizations (
  id                 text NOT NULL,
  device_hash        text NOT NULL,
  user_code_hash     text NOT NULL,
  client_id          text NOT NULL,
  status             text NOT NULL,
  user_id            text NOT NULL DEFAULT ''::text,
  org_id             text NOT NULL DEFAULT ''::text,
  interval_seconds   integer NOT NULL,
  expires_at         bigint NOT NULL,
  last_polled_at     bigint NOT NULL DEFAULT 0,
  created_at         bigint NOT NULL,
  PRIMARY KEY (id),
  CONSTRAINT device_authorizations_device_hash_key UNIQUE (device_hash),
  CONSTRAINT device_authorizations_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'denied'::text, 'consumed'::text, 'expired'::text]))),
  CONSTRAINT device_authorizations_user_code_hash_key UNIQUE (user_code_hash)
);

-- cli_auth_codes
CREATE TABLE cli_auth_codes (
  code_hash          text NOT NULL,
  user_id            text NOT NULL,
  org_id             text NOT NULL,
  challenge          text NOT NULL,
  redirect_uri       text NOT NULL,
  expires_at         bigint NOT NULL,
  consumed           integer NOT NULL DEFAULT 0,
  PRIMARY KEY (code_hash)
);

-- rate_counters
CREATE TABLE rate_counters (
  bucket             text NOT NULL,
  window_start       bigint NOT NULL,
  hits               integer NOT NULL,
  PRIMARY KEY (bucket, window_start)
);

-- cluster_leases
CREATE TABLE cluster_leases (
  name               text NOT NULL,
  holder             text NOT NULL,
  lease_until        bigint NOT NULL,
  PRIMARY KEY (name)
);

-- 索引
CREATE INDEX IF NOT EXISTS idx_device_exp ON device_authorizations (expires_at);
