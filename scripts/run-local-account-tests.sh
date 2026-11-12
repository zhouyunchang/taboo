#!/usr/bin/env bash
# 本地账号（无 Keycloak）跑 Playwright：只起 Go server + 本地夹具数据。
# 与 serve-federation-web-test.sh 的唯一差别是不设置任何 OIDC 环境变量，
# 因此 /api/v1/auth/session 返回 404，前端走本地账号（localStorage token）路径。
set -euo pipefail
cd "$(dirname "$0")/.."
just build
fixture_dir=$(mktemp -d)
server_pid=''
cleanup() {
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$fixture_dir"
}
trap cleanup EXIT
export TABOO_DATA_DIR="$fixture_dir"
export TABOO_PORT=18888
export TABOO_PUBLIC_ORIGIN='http://127.0.0.1:18888'
export TABOO_LOGIN_RATE_LIMIT=1000
unset TABOO_OIDC_SESSION_PROFILE TABOO_OIDC_ISSUER_ALLOWLIST TABOO_OIDC_ISSUER TABOO_OIDC_CLIENT_ID \
      TABOO_OIDC_CLIENT_SECRET TABOO_OIDC_ORG TABOO_OIDC_AUTOCREATE 2>/dev/null || true
./apps/server-go/bin/taboo-server &
server_pid=$!
# 等到 readyz 再交给 Playwright
for _ in $(seq 1 100); do
  if curl -fsS "http://127.0.0.1:${TABOO_PORT}/api/v1/readyz" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
echo "[local-test] server ready on :${TABOO_PORT}"
wait "$server_pid"
