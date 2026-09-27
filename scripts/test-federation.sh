#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Public, disposable fixtures only. No private services or credentials.
image='quay.io/keycloak/keycloak@sha256:9b0330756022422149aa6502eb2def8cd47c6e1b000c7c65cdb13e7c0133e992'
port="${TABOO_TEST_KEYCLOAK_PORT:-18081}"
container="taboo-keycloak-test-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$container" -p "127.0.0.1:$port:8080" \
  -v "$PWD/tests/federation/keycloak-realm.json:/opt/keycloak/data/import/taboo-test.json:ro" \
  "$image" start-dev --import-realm --hostname "http://127.0.0.1:$port" >/dev/null
export TABOO_TEST_KEYCLOAK_ISSUER="http://127.0.0.1:$port/realms/taboo-test"
export TABOO_REQUIRE_KEYCLOAK=1
ready=false
for _ in $(seq 1 60); do
  if curl -fsS "$TABOO_TEST_KEYCLOAK_ISSUER/.well-known/openid-configuration" >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
if [ "$ready" != true ]; then docker logs --tail 30 "$container"; exit 1; fi
(cd apps/server-go && go test -race ./internal/server -run 'Test(Federation|Keycloak)' -count=1 -timeout 120s)
if [ "${TABOO_TEST_BROWSER:-0}" = 1 ]; then
  npm exec --workspace=@taboo/web -- playwright test
fi
