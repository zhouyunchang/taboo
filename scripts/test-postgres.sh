#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Disposable public PostgreSQL 18 fixture. The app never uses a superuser login.
image='postgres@sha256:cbe15165195f7f2d63885b4d990fdec7b602248533cb05bd992284a45a58fed3'
port="${TABOO_TEST_POSTGRES_PORT:-15439}"
container="taboo-postgres-test-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$container" -e POSTGRES_PASSWORD=fixture-admin -e POSTGRES_DB=taboo \
  -p "127.0.0.1:$port:5432" "$image" >/dev/null
ready=false
for _ in $(seq 1 60); do
  if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres -d taboo >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
if [ "$ready" != true ]; then docker logs --tail 30 "$container"; exit 1; fi
docker exec "$container" psql -U postgres -d taboo -v ON_ERROR_STOP=1 -c \
  "CREATE ROLE taboo_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS; CREATE ROLE taboo_control LOGIN PASSWORD 'fixture-control' NOSUPERUSER NOBYPASSRLS; GRANT taboo_runtime TO taboo_control; ALTER DATABASE taboo OWNER TO taboo_control; GRANT CREATE ON SCHEMA public TO taboo_control;" >/dev/null
export TABOO_TEST_POSTGRES_DSN="postgres://taboo_control:fixture-control@127.0.0.1:$port/taboo?sslmode=disable"
export TABOO_REQUIRE_POSTGRES=1
# Separate target engine fixture; never used by the application storage pool.
export TABOO_TEST_DYNAMIC_POSTGRES_DSN="postgres://postgres:fixture-admin@127.0.0.1:$port/taboo?sslmode=disable"
(cd apps/server-go && go test -race ./... -count=1 -timeout 180s)
if [ "${TABOO_TEST_WITH_KEYCLOAK:-0}" = 1 ]; then
  ./scripts/test-federation.sh
fi
