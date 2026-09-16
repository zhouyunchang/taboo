# taboo（禁制）

> 传说中有大价值的地方，都有禁制保护。taboo 就是保护你密钥的那道禁制。

开源自部署密钥管理平台 —— Infisical / HashiCorp Vault 的轻量平替。**密钥永不落盘明文、永不进 Git 仓库、全操作可审计。** 完全 MIT，无 open-core 付费墙。

详细设计见 [doc/OpenVault_密钥管理平台_功能与详细设计.md](doc/OpenVault_密钥管理平台_功能与详细设计.md)（本仓库架构的唯一事实源，代号 OpenVault，产品名 taboo）。

## 为什么叫 taboo（禁制）

修仙小说里，藏有重宝的洞府必有禁制：外人不可见、不可触，擅动者必被察觉。密钥管理的本质一样——**高价值数据 + 严格访问控制 + 全程留痕**。

## 当前状态：v1.0（Go 后端，功能全量落地）

> 当前版本为 **Go 后端 + React 前端**（设计文档 §3 目标架构），单二进制 embed.FS 交付；
> Node MVP 保留在 `apps/server` 仅作契约回归参考。v1.0 发布说明见 [docs/releases/v1.0.0.md](docs/releases/v1.0.0.md)。

功能总览（对齐设计文档）：

- [x] 信封加密三层结构：Root Key (KEK) → 组织 DEK → Secret Value（AES-256-GCM，nonce 12B 随机不复用）
- [x] 密钥 CRUD + 版本历史 + 回滚 + reveal 分离（RBAC 二次校验 + 审计）
- [x] 机器身份（client_credentials + 显式 scope）、TOTP 2FA、多级文件夹
- [x] CLI / Go SDK / MCP Server / OpenAPI 3.1 契约（含路由漂移检查）
- [x] 动态密钥：PostgreSQL 短期账号 lease（到期自动回收）
- [x] M5：Secret Sync（GitHub/Vercel/Cloudflare）、Webhooks（HMAC 签名）、OIDC SSO（JIT 入组）、审计导出 CSV/JSONL、Docker/Helm/GoReleaser 发布
- [x] 注册即建个人组织 + 默认项目 + dev/staging/prod 三环境；组织/项目/环境模型
- [x] 密钥 CRUD + 版本历史 + 回滚（`secret_versions` 只追加）
- [x] 用户认证：**Argon2id**（m=64MB,t=3,p=4）密码哈希（旧 scrypt 登录透明迁移）+ JWT 15min + refresh 旋转 + 登录限流 5 次/分钟/IP
- [x] RBAC-lite：owner / admin / developer / viewer；**reveal（取明文）与 read（看元数据）分离**；developer 禁写 prod
- [x] 审计日志：append-only，谁何时对哪个密钥做了什么（读/写/删/回滚/导出）
- [x] REST API：`/api/v1/auth/*`、`/orgs/*`、`/projects/*/secrets`、`export`、`/audit`
- [x] Web Dashboard：登录注册、项目/环境切换、密钥表（掩码/显示/复制 20s 自清除）、版本抽屉+回滚、审计筛选
- [x] CLI：`login / set / get / list / export / run`（`run` 对齐设计文档 §7.1 注入流程）+ `scan` 泄漏扫描（151 条规则，含 .tabooignore / 行内 taboo:ignore / baseline 抑制、--staged、--json、pre-commit 钩子安装）
- [x] **Go 后端（apps/server-go，M1）**：Chi 路由、模块化单体（internal/{config,server,auth,secret,crypto,org,store,apperr}）、`embed.FS` 单二进制、同一套 14 项冒烟测试全过
- [x] **机器身份（M2 #3，Go 版）**：client_credentials 换短期 JWT（TTL 可配 ≤1h，临期 CLI 自动重换）；scope 显式 (项目+环境+read|write) **禁止通配**；吊销立即生效；审计 actor 标识 `identity:xxx`；Web 管理页（创建向导/一次性 secret 展示/吊销）+ 22 项专项冒烟全过
- [x] **多级文件夹（M2 #5，Go 版）**：物化路径 `/a/b/`、`folders` 表 + `secrets.folder_id`（存量幂等迁移）；创建自动补中间节点、移动/重命名级联子孙、仅空目录可删、根目录保护、路径穿越拒绝；密钥按目录隔离（同 key 不同目录互不冲突）；前端文件夹树 + 面包屑 + 目录过滤；24 项专项冒烟全过
- [x] **TOTP 2FA（M2 #4，Go 版）**：RFC 6238（SHA1/30s/6 位，stdlib 实现）+ otpauth 二维码；登录 `totp_required` → 5min challenge 二次验证换 token；secret 加密落库、时间窗防重放（last_step 单调）；10 个恢复码（仅存哈希、用后即废）；disable 需密码确认；前端登录 2FA 步 + 设置抽屉（扫码/密钥/恢复码）；23 项专项冒烟全过
- [x] **OpenAPI 3.1 契约（M3 #7）**：`api/openapi.yaml` 为 REST 唯一事实源（auth/TOTP/orgs/identities/folders/secrets 全量端点 + `{code,message,details}` 错误模型）；前端 TS 类型由契约生成（`npm run types -w @taboo/web`）；Go 契约漂移测试（chi 路由表 vs 契约，漂移即失败）；Scalar 文档页 `/api/docs`
- [x] **Go SDK（M3 #8，`packages/sdk-go`）**：原生手写薄封装 —— 登录/2FA 二次验证/refresh 旋转、机器身份 token 临期自动重换、secrets CRUD / reveal / versions / rollback / export、folders；`examples/basic` 集成示例已对真实服务跑通
- [x] **MCP Server（M4 #10，`apps/mcp`）**：stdio JSON-RPC 最小实现，仅暴露 `secrets.list` / `secrets.get` 两 Tool（默认掩码、显式 reveal 记审计）；机器身份 scope 限定（dev 身份访问 prod 即拒）、吊销立即断连；11 项验收冒烟全过（含吊销后拒绝 + 审计留痕）；Claude Code / Kimi / Cursor 接入文档
- [x] **动态密钥（M4 #9，Go 版）**：PostgreSQL 动态引擎 —— 机器身份申请短期账号 lease（`taboo_` 前缀随机用户名，CREATE USER … VALID UNTIL + 最小只读授权），到期/吊销自动 DROP（worker 20s 扫描）；lease 明文密码仅此一次下发（库中 Master Key 加密）；身份吊销联动回收其活跃 lease；引擎连接串加密落库不回显；身份需在本项目有 scope 方可申请；17 项 mock 冒烟 + **10 项真实 PostgreSQL 联调全过**（podman 容器 `taboo-pg-test`，pg_roles 侧验证创建/登录/续期/DROP；修复 lib/pq 无占位语句误传参导致回收失败的问题）
- [ ] Secret Sync、Webhooks、OIDC → 见路线图

## 双后端说明

| | `apps/server`（Node MVP） | `apps/server-go`（Go，M1 起主推） |
|---|---|---|
| 定位 | 设计契约验证，功能参考实现 | 目标架构（设计文档 §3），持续演进 |
| 存储 | `node:sqlite` | `modernc.org/sqlite`（纯 Go 免 CGO） |
| 密码哈希 | scrypt | Argon2id（含 scrypt 透明迁移） |
| 前端托管 | 运行时读 `apps/web/dist` | 构建期 `embed.FS` 进二进制 |
| 启动 | `npm start` | `make -C apps/server-go build && ./apps/server-go/bin/taboo-server` |

两后端共用同一套 API 契约（`apps/server/scripts/smoke.js` 14 项端到端检查对两者均可运行）。

## 快速开始

要求：Node.js ≥ 22.5（使用内置 `node:sqlite`，无需任何外部服务）

```bash
npm install                       # 安装全部 workspace 依赖（一次即可）

# 1. 启动 API + Web（同一端口，前端已构建时自动托管 apps/web/dist）
npm start                         # http://localhost:7100

# 2. 前端开发模式（Vite，/api 自动代理到 7100）
npm run dev

# 3. CLI
npm run cli -- login you@example.com your-password
npm run cli -- set DB_PASS s3cret --env dev
npm run cli -- run -- npm run dev
```

冒烟测试（14 项端到端检查）：

```bash
npm start &        # 先起服务
npm run smoke
```

## 配置

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `TABOO_PORT` | `7100` | 服务端口 |
| `TABOO_DATA_DIR` | `apps/server/data` | SQLite + master key 目录 |
| `TABOO_MASTER_KEY` | 自动生成文件 | 32B hex/base64；不设置则生成 `master.key`（0600） |
| `TABOO_JWT_SECRET` | 随机（重启失效） | 生产必须显式设置 |
| `TABOO_CORS_ORIGIN` | `*` | 开发跨域来源 |
| `TABOO_LOGIN_RATE_LIMIT` | `5` | 登录类接口（login / totp/login / identities/token）每 IP 每窗口上限 |

## Monorepo 结构

本项目以 **npm workspaces** 管理单仓，根目录统一依赖与脚本：

```
taboo/                       # 根 workspace（private）
├── package.json             # workspaces: ["apps/*"] + 统一脚本
├── doc/                     # 设计文档（OpenVault 功能与详细设计）
├── apps/
│   ├── server/              # @taboo/server — API 服务（Node MVP，契约参考实现）
│   ├── server-go/           # Go 后端（M1 起主推）：cmd/server + internal/*（auth/crypto/org/secret/identity/folder/store…）
│   ├── web/                 # @taboo/web — React 19 + TS + Vite Dashboard
│   ├── cli/                 # @taboo/cli — bin: taboo（login/set/get/list/export/run/scan，支持机器身份）
│   └── mcp/                 # MCP Server（M4 #10）：stdio JSON-RPC，secrets.list / secrets.get
├── packages/
│   └── sdk-go/              # Go SDK（M3 #8）：原生手写薄封装 REST 契约 + examples/basic
├── api/
│   └── openapi.yaml         # OpenAPI 3.1 契约（M3 #7，前后端与 SDK 唯一事实源）
└── README.md
```

### 根目录常用命令

```bash
npm install          # 一次安装全部 workspace 依赖（提升到根 node_modules）
npm start            # = @taboo/server，起 API + 托管 web/dist（:7100）
npm run dev          # = @taboo/web Vite 开发模式（/api 代理到 7100）；参数透传：npm run dev -- --port 7100
npm run build        # 构建 @taboo/web → apps/web/dist
npm run smoke        # 14 项端到端冒烟检查（需先 npm start）
npm run cli -- login you@example.com your-password
```

单 workspace 命令形如 `npm run <script> -w @taboo/server`。未来共享代码（SDK、常量、OpenAPI 生成物）放 `packages/`，已预留 workspace 通配。

## 路线图（对齐设计文档 §10）

| 阶段 | 内容 |
|---|---|
| **M0（已完成）** | Node.js MVP：验证加密架构、API 契约、RBAC、审计 |
| **M1（进行中）** | Go 后端已落地（Chi + modernc.org/sqlite、Argon2id、embed.FS 单二进制，冒烟全过）；剩余：sqlc 代码生成、PostgreSQL 主模式 |
| M2 | 机器身份（✅ #3）、文件夹多级路径（✅ #5）、TOTP 2FA（✅ #4）—— **M2 全部完成** |
| M3 | CLI 全命令（scan ✅ #6，Dashboard 报告页遗留）、OpenAPI 契约（✅ #7）、Go SDK（✅ #8）；Python/Node SDK 待 OpenAPI 生成器接入 |
| M4 | 动态密钥（✅ #9，PostgreSQL lease + worker 回收 + 身份联动）、MCP Server（✅ #10）—— **M4 全部完成** |
| M5 | **全部完成**：Secret Sync（✅ #11）、Webhooks（✅ #12）、OIDC SSO（✅ #13）、审计导出 + Docker/Helm/GoReleaser（✅ #14）—— **v1.0 发布** |

## 文档

- [快速开始](docs/quickstart.md) · [部署指南](docs/deployment.md) · [API 参考](docs/api.md) · [安全模型](docs/security.md)
- 运行时 API 文档：`GET /api/docs`（Scalar，契约本体 [api/openapi.yaml](api/openapi.yaml)）

## 部署

单镜像多阶段构建（前端 Vite 构建 → Go 编译，前端经 `embed.FS` 打进单二进制；SQLite 内嵌，无需外部依赖）：

```bash
docker build -f apps/server-go/Dockerfile -t taboo .          # 仓库根目录为上下文
docker run -d --name taboo -p 7100:7100 -v taboo-data:/data \
  -e TABOO_JWT_SECRET=$(openssl rand -hex 32) taboo
```

或 docker compose（`--profile pg` 附带动态密钥联调用 PostgreSQL 16）：

```bash
docker compose up -d --build
```

数据（SQLite + master.key）持久化在 `taboo-data` 卷，**master.key 即全部密钥的 KEK，务必备份**。生产环境务必设置 `TABOO_JWT_SECRET`（不设则重启后会话失效）。运行时以非 root 用户（uid 10001）执行，带 HEALTHCHECK。

## 许可

MIT（v1.0 正式确定；当前代码以 MIT 精神完全开放，无付费墙）
