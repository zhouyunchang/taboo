#!/usr/bin/env bash
# Runs ON dev122. Idempotently creates the taboo database and its two roles on the
# shared swetcc-postgres container (admin access via the container's local socket).
# Never drops or alters anything that does not belong to taboo.
set -euo pipefail
cd "$(dirname "$0")"
: "${PG_CONTAINER:=swetcc-postgres}"
set -a; . ./.env; set +a
: "${TABOO_CONTROL_PASSWORD:?missing in .env}"
psql_admin() { docker exec -i "$PG_CONTAINER" psql -v ON_ERROR_STOP=1 -U postgres -qtA "$@"; }
psql_admin -d postgres --set=pw="$TABOO_CONTROL_PASSWORD" <<'SQL'
SELECT 'CREATE ROLE taboo_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS'
  WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taboo_runtime') \gexec
SELECT 'CREATE ROLE taboo_control LOGIN NOSUPERUSER NOBYPASSRLS'
  WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'taboo_control') \gexec
ALTER ROLE taboo_control WITH LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE PASSWORD :'pw';
ALTER ROLE taboo_runtime WITH NOLOGIN NOSUPERUSER NOBYPASSRLS;
GRANT taboo_runtime TO taboo_control;
SELECT 'CREATE DATABASE taboo OWNER taboo_control'
  WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'taboo') \gexec
SQL
psql_admin -d taboo <<'SQL'
ALTER DATABASE taboo OWNER TO taboo_control;
GRANT CREATE ON SCHEMA public TO taboo_control;
REVOKE ALL ON DATABASE taboo FROM PUBLIC;
GRANT CONNECT ON DATABASE taboo TO taboo_control;
SQL
echo "taboo database and roles ready"
