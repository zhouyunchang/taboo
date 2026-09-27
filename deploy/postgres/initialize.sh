#!/usr/bin/env sh
set -eu
# Disposable/fresh installation only. Database administrators own role creation.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --set=control_password="$TABOO_CONTROL_PASSWORD" <<'SQL'
CREATE ROLE taboo_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE taboo_control LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'control_password';
GRANT taboo_runtime TO taboo_control;
ALTER DATABASE taboo OWNER TO taboo_control;
GRANT CREATE ON SCHEMA public TO taboo_control;
SQL
