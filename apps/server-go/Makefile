# taboo Go server (M1) — 构建与运行
.PHONY: web build run smoke tidy version

# 把前端构建产物 + API 契约拷入 embed 目录（main 包内）
web:
	cd ../web && npm run build
	rm -rf cmd/server/web/assets
	cp -r ../web/dist/* cmd/server/web/
	cp ../../api/openapi.yaml cmd/server/web/

# 版本信息（可覆盖：make build TABOO_VERSION=0.2.0 TABOO_COMMIT=abc1234）
BI_PKG        := github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo
TABOO_VERSION ?= $(shell node -p "require('../../package.json').version" 2>/dev/null || echo dev)
TABOO_COMMIT  ?= $(shell git rev-parse --short=7 HEAD 2>/dev/null)$(shell git diff --quiet HEAD -- 2>/dev/null || echo -dirty)
BUILD_TIME    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS       := -X $(BI_PKG).version=$(TABOO_VERSION) -X $(BI_PKG).commit=$(TABOO_COMMIT) -X $(BI_PKG).buildTime=$(BUILD_TIME)

build: web
	go build -ldflags "$(LDFLAGS)" -o bin/taboo-server ./cmd/server

version:
	@echo "$(TABOO_VERSION) $(TABOO_COMMIT)"

run:
	go run ./cmd/server

tidy:
	go mod tidy

# 契约回归：Node 版同一套 15 项冒烟检查（含清理）
smoke:
	cd ../server && node scripts/smoke.js
# M5 冒烟：Webhooks（HMAC 验签）/ Secret Sync / 审计导出 / OIDC 端点
smoke-m5:
	cd ../server && node scripts/smoke-m5.js
