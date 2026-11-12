-- +goose Up

CREATE TABLE device_authorizations (
  id TEXT PRIMARY KEY,
  device_hash TEXT NOT NULL UNIQUE,
  user_code_hash TEXT NOT NULL UNIQUE,
  client_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','approved','denied','consumed','expired')),
  user_id TEXT NOT NULL DEFAULT '',
  org_id TEXT NOT NULL DEFAULT '',
  interval_seconds INTEGER NOT NULL,
  expires_at BIGINT NOT NULL,
  last_polled_at BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL
);

CREATE TABLE cli_auth_codes (
  code_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  org_id TEXT NOT NULL,
  challenge TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  expires_at BIGINT NOT NULL,
  consumed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE rate_counters (
  bucket TEXT NOT NULL,
  window_start BIGINT NOT NULL,
  hits INTEGER NOT NULL,
  PRIMARY KEY (bucket, window_start)
);

CREATE TABLE cluster_leases (
  name TEXT PRIMARY KEY,
  holder TEXT NOT NULL,
  lease_until BIGINT NOT NULL
);

CREATE INDEX idx_device_exp ON device_authorizations(expires_at);
