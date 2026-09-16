# taboo Go server (M1) — 构建与运行
.PHONY: web build run smoke tidy

# 把前端构建产物 + API 契约拷入 embed 目录（main 包内）
web:
	cd ../web && npm run build
	rm -rf cmd/server/web/assets
	cp -r ../web/dist/* cmd/server/web/
	cp ../../api/openapi.yaml cmd/server/web/

build: web
	go build -o bin/taboo-server ./cmd/server

run:
	go run ./cmd/server

tidy:
	go mod tidy

# 契约回归：Node 版同一套 14 项冒烟检查
smoke:
	cd ../../server && node scripts/smoke.js
