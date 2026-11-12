# taboo（禁制）— 命令入口（Just）。安装：https://just.systems
#   just                 # 列出配方
#   just build           # 前端 embed + 编译 Go 服务
#   just start           # 构建并启动 :7100
#   just run             # go run 开发模式
#   just web             # 仅构建前端并写入 embed 目录
#   just smoke           # 契约冒烟（服务需已启动）

set dotenv-load := true
set ignore-comments := true

[unix]
set shell := ["bash", "-cu"]

[windows]
set shell := ["powershell.exe", "-NoLogo", "-Command"]

exe := if os() == "windows" { ".exe" } else { "" }

default:
    @just --list

# 构建前端并把产物 + OpenAPI 拷入 Go embed 目录
web:
    npm run build -w @taboo/web
    node scripts/embed-web.mjs

# 清理构建产物：清掉后旧页面里引用的 /assets/index-<hash>.js 会 404（白屏/操作失败）。
# 排查“刷新前能用、just start 重建后报错”这类问题时先清一次再 just start。
clean:
    rm -rf apps/web/dist apps/server-go/cmd/server/web/assets apps/server-go/cmd/server/web/index.html

# 前端 embed + 编译 bin/taboo-server（TABOO_VERSION / TABOO_COMMIT / BUILD_TIME 可覆盖）
build: web
    node scripts/build-go.mjs

# 打印 version 与 commit（与 ldflags 同源）
version:
    node scripts/ldflags.mjs --meta

# go run 开发模式（静态 UI 需先 just web；或另开 Vite：npm run dev）
[working-directory: 'apps/server-go']
run:
    go run ./cmd/server

# 构建并以前台方式启动服务
start: build
    ./apps/server-go/bin/taboo-server{{exe}}

[working-directory: 'apps/server-go']
tidy:
    go mod tidy

# 契约冒烟（默认连 http://127.0.0.1:7100，可用 TABOO_BASE 覆盖）
[working-directory: 'apps/server']
smoke:
    node scripts/smoke.js

[working-directory: 'apps/server']
smoke-m5:
    node scripts/smoke-m5.js
