#!/usr/bin/env bash
# 本机全量验证（docs/ROADMAP.md §通用验证），一条命令跑完并输出汇总。
#   ./scripts/verify-all.sh            # 全量（含 Keycloak/PG/浏览器重型套件，约 10+ 分钟，需要 Docker）
#   ./scripts/verify-all.sh --quick    # 跳过重型套件
#   ./scripts/verify-all.sh --container  # 额外：docker build + 容器恢复测试
# 每步日志：$VERIFY_LOG_DIR（默认 mktemp）。任一步失败最终退出码非 0，但会继续跑完其余步骤。
set -uo pipefail
root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"; cd "$root"
QUICK=0; CONTAINER=0
for a in "$@"; do case "$a" in --quick) QUICK=1;; --container) CONTAINER=1;; *) echo "unknown option $a"; exit 2;; esac; done
LOGS="${VERIFY_LOG_DIR:-$(mktemp -d -t taboo-verify)}"; mkdir -p "$LOGS"
results=(); fails=0; n=0

step() { # NAME CMD...
  local name="$1"; shift; n=$((n+1))
  local f; f="$LOGS/$(printf '%02d' $n)-$(echo "$name" | tr -c 'a-zA-Z0-9\n' '-').log"
  printf '==> [%02d] %s ... ' "$n" "$name"
  local t0=$SECONDS
  if ( "$@" ) >"$f" 2>&1; then results+=("PASS  $(printf '%4ss' $((SECONDS-t0)))  $name"); echo "PASS ($((SECONDS-t0))s)"
  else results+=("FAIL  $(printf '%4ss' $((SECONDS-t0)))  $name  → $f"); fails=$((fails+1)); echo "FAIL ($((SECONDS-t0))s) → $f"; tail -15 "$f" | sed 's/^/    /'; fi
}

# npm ci 会改 apps/cli/taboo.js 的可执行位。只恢复索引里的权限，不覆盖工作区内容。
npm_ci() {
  npm ci
  if git ls-files -s apps/cli/taboo.js | grep -q '^100755'; then chmod +x apps/cli/taboo.js; else chmod -x apps/cli/taboo.js; fi
}
# 与工作区当前文件比较（未提交的改动也要和 openapi.yaml 一致）
types_drift() { local t; t="$(mktemp)"; cp apps/web/src/api-types.ts "$t"; npm run types && diff -u "$t" apps/web/src/api-types.ts; }
go_server() { go -C apps/server-go vet ./... && go -C apps/server-go test -race ./...; }
go_libs() { go -C packages/sdk-go test -race ./... && go -C apps/mcp test -race ./...; }

# 起一个临时 SQLite 服务：注册模式冒烟 + CLI 建号/启停 + 登录模式冒烟 + 浏览器 1280/390 检查
e2e_local() {
  local dir port pid pw rc=0
  dir="$(mktemp -d -t taboo-e2e)"; port="${VERIFY_PORT:-7199}"
  export TABOO_DATA_DIR="$dir" TABOO_JWT_SECRET="verify-$(openssl rand -hex 16)" TABOO_LOGIN_RATE_LIMIT=50 TABOO_PORT="$port"
  ./apps/server-go/bin/taboo-server --version
  ./apps/server-go/bin/taboo-server & pid=$!
  trap 'kill $pid 2>/dev/null; rm -rf "$dir"' RETURN
  for _ in $(seq 1 50); do curl -fsS "http://localhost:$port/api/v1/readyz" >/dev/null 2>&1 && break; sleep 0.2; done
  curl -fsS "http://localhost:$port/api/v1/healthz"; echo
  export TABOO_BASE="http://localhost:$port"
  (cd apps/server && node scripts/smoke.js) || rc=1
  pw="$(openssl rand -hex 16)"
  echo "$pw" | ./apps/server-go/bin/taboo-server user create verify@local.test --name Verify --disabled || rc=1
  ./apps/server-go/bin/taboo-server user enable verify@local.test || rc=1
  printf '{"email":"verify@local.test","password":"%s"}' "$pw" > "$dir/creds.json"
  (cd apps/server && TABOO_SMOKE_CREDENTIALS="$dir/creds.json" node scripts/smoke.js) || rc=1
  OUT="$LOGS/screens" node scripts/browser-check.mjs < "$dir/creds.json" || rc=1
  ./apps/server-go/bin/taboo-server user disable verify@local.test || rc=1
  return $rc
}

step "npm ci" npm_ci
step "OpenAPI types drift" types_drift
step "web tsc --noEmit" npm exec --workspace=@taboo/web -- tsc --noEmit
step "web build" npm run build
step "server-go vet + test -race" go_server
step "sdk-go + mcp test -race" go_libs
step "cli oauth test" node --test apps/cli/oauth.test.js apps/cli/phase4.test.js apps/cli/phase6.test.js
step "server build (version ldflags)" just build
step "local e2e: smoke x2 + user CLI + browser 1280/390" e2e_local
if [ "$QUICK" = 0 ]; then
  step "consumers suite" ./scripts/test-consumers.sh
  step "federation + browser suite" env TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh
  step "postgres + keycloak suite" env TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh
fi
if [ "$CONTAINER" = 1 ]; then
  step "docker build" docker build -f apps/server-go/Dockerfile -t taboo:standard-federation-test --build-arg TABOO_VERSION="$(node -p "require('./package.json').version")" --build-arg TABOO_COMMIT="$(git rev-parse --short=7 HEAD)" .
  step "container recovery" node scripts/test-container-recovery.mjs
fi

echo; echo "=== verify-all summary ($(git rev-parse --short=7 HEAD), logs: $LOGS) ==="
printf '%s\n' "${results[@]}"
[ "$fails" = 0 ] && echo "ALL $n STEPS PASSED" || echo "$fails of $n STEPS FAILED"
exit $((fails > 0))
