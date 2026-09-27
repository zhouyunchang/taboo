#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
checkout=$(mktemp -d)
server_pid=''
cleanup() { if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi; rm -rf "$checkout"; }
trap cleanup EXIT
# Copy committed public sources only; no node_modules, data, private workspaces or untracked files.
git clone --quiet --no-local --single-branch --branch "$(git branch --show-current)" . "$checkout/repo"
git -C "$checkout/repo" remote set-url origin https://github.com/zhouyunchang/taboo.git
cd "$checkout/repo"
npm ci --no-audit --no-fund
npm run types
git diff --exit-code -- apps/web/src/api-types.ts
npm exec --workspace=@taboo/web -- tsc --noEmit
for module in apps/server-go packages/sdk-go apps/mcp; do
 go -C "$module" test -race ./...
 go -C "$module" vet ./...
done
node --test apps/cli/oauth.test.js
make -C apps/server-go build
mkdir -p "$checkout/data"
TABOO_DATA_DIR="$checkout/data" TABOO_PORT=17112 TABOO_LOGIN_RATE_LIMIT=10000 ./apps/server-go/bin/taboo-server >"$checkout/server.log" 2>&1 &
server_pid=$!
ready=false
for _ in $(seq 1 30); do if curl -fsS http://127.0.0.1:17112/api/v1/readyz >/dev/null 2>&1; then ready=true;break;fi;sleep 1;done
if [ "$ready" != true ];then echo 'Clean checkout server failed readiness';exit 1;fi
TABOO_BASE=http://127.0.0.1:17112 node apps/server/scripts/smoke.js
./scripts/test-consumers.sh
echo 'Clean committed checkout: dependency install, generated contract, types, all Go modules/race/vet, CLI, binary build, legacy smoke and real consumers passed.'
