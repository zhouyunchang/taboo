#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
just build
fixture_dir=$(mktemp -d)
server_pid=''
cleanup() { if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi; rm -rf "$fixture_dir"; }
trap cleanup EXIT
export TABOO_DATA_DIR="$fixture_dir"
export TABOO_PORT=18888
export TABOO_PUBLIC_ORIGIN='http://127.0.0.1:18888'
export TABOO_OIDC_SESSION_PROFILE=BOUNDED
export TABOO_OIDC_ISSUER_ALLOWLIST="${TABOO_TEST_KEYCLOAK_ISSUER:-http://127.0.0.1:18081/realms/taboo-test}"
export TABOO_LOGIN_RATE_LIMIT=1000
./apps/server-go/bin/taboo-server &
server_pid=$!
wait "$server_pid"
