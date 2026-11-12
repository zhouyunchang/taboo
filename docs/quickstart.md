# 快速开始

5 分钟跑起一个本地 taboo 实例。

## Docker（推荐）

```bash
docker run -d --name taboo -p 7100:7100 \
  -v taboo-data:/data \
  -e TABOO_JWT_SECRET=$(openssl rand -hex 32) \
  ghcr.io/zhouyunchang/taboo:latest

open http://localhost:7100
```

首次打开 → 注册账号（自动创建个人组织 + 默认项目 + dev/staging/prod 三环境）→ 进入密钥工作台。

## docker compose

```bash
git clone https://github.com/zhouyunchang/taboo.git && cd taboo
docker compose up -d --build          # SQLite 内嵌单容器
docker compose --profile pg up -d     # 附带 PostgreSQL 16（动态密钥联调）
```

## 本地开发

需安装 [Just](https://just.systems)（`winget install Casey.Just` / `brew install just`）。

```bash
npm ci
npm run dev            # 前端 vite (5173)；可另开后端
just web && just run   # 后端 go run（:7100，embed 静态 UI）
just build             # 前端 embed + 编译 bin/taboo-server
just smoke             # 14 项契约冒烟检查（服务需已启动）
(cd apps/server-go && go test ./...)  # 契约漂移检查（路由 vs openapi.yaml）
```

## 启动自检

```bash
taboo-server --check
```

校验：Master Key 加解密往返、Argon2id 哈希路径、DB schema 版本与关键表存在性。K8s 探活用 `/api/v1/healthz` 与 `/api/v1/readyz`，不要把 `--check`（含 Argon2）当 liveness。

## 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| `TABOO_PORT` | 监听端口 | `7100` |
| `TABOO_DATA_DIR` | 数据目录（SQLite + 导出文件） | `./data` |
| `TABOO_MASTER_KEY` | 32B hex/base64；缺省自动生成到 `$DATA_DIR/master.key` | 自动生成 |
| `TABOO_JWT_SECRET` | 会话签名密钥；**生产必须显式设置** | 随机（重启即失效） |
| `TABOO_CORS_ORIGIN` | CORS Origin | `*` |
| `TABOO_LOGIN_RATE_LIMIT` | 登录限流（次/分钟/IP） | `5` |

## 标准身份联邦与 PostgreSQL

本分支的标准身份、多租户和迁移能力由 Go 后端提供。Node MVP 仅保留原本地模式。

1. 使用上面的 SQLite 镜像或 `deploy/compose.postgres.yml` 建立实例。PostgreSQL 示例需通过环境设置 `POSTGRES_PASSWORD`、URI 安全的 `TABOO_CONTROL_PASSWORD`、`TABOO_MASTER_KEY`（32 字节十六进制）、`TABOO_JWT_SECRET`。所有值自行生成并安全保存；不要提交 `.env`。
2. `docker compose -f deploy/compose.postgres.yml up -d --build`。示例自动建立非超级用户的 control 登录和独立 NOLOGIN runtime 角色；只有容器内数据库网络使用 `sslmode=disable`。外部数据库根据运维 TLS 策略配置 DSN。
3. 设置 `TABOO_PUBLIC_ORIGIN` 为浏览器访问的精确 origin（非回环地址必须 HTTPS）；在 `TABOO_ISSUER_ALLOWLIST` 登记允许的完整 issuer，以逗号分隔。该 issuer 必须同时能被浏览器和服务端访问，不能用容器内的 `localhost` 代替外部身份提供方。
4. 首次用本地账号建立组织。在组织设置添加 OpenID Connect 连接，将显示的 callback URL 精确登记到提供方。需要 Code + S256、RS256、client_secret_basic、auth_time；无需私有角色或租户 Claim。默认不开启任意认证用户自动加入组织。
5. 要把本地 owner 转为外部身份，使用“登录身份”完成原密码（含已启用 TOTP）和新提供方双重验证。角色和秘密不变，旧登录方式失效。也可由 owner 明确管理已经准入的外部成员。按项目和环境分别授予 read/write/reveal/export/rollback/delete；组织角色不隐含外部秘密操作。
6. 在“会话策略”选择 BOUNDED（最多 900 秒）或 ONLINE_REQUIRED（逐请求标准内省，故障拒绝），配置可验证的 ACR/AMR 与敏感操作期限。支持的提供方可启用 RP-Initiated Logout。最后在“登录策略”开启组织级 EXTERNAL_REQUIRED。
7. 自动登记连接可用 `TABOO_OIDC_ORG/ISSUER/CLIENT_ID/CLIENT_SECRET`；组织需已存在，issuer 仍需 allowlist。推荐通过 secret manager 或 Kubernetes Secret 注入。幂等引导不会覆盖控制台已有设置；全局关闭注册/密码前先验证外部 owner 可用。

机器接入使用“OAuth 工作负载”，分别登记资源信任、精确 subject/client_id、项目环境操作与必需 scope。JWT 只接受 RFC 9068 profile；其他 access token 使用 RFC 7662。SDK、CLI、MCP 的配置与迁移见 [OAuth 工作负载说明](design/standard-oauth-workloads.zh-CN.md)。

部署参数：

| 变量 | 用途 |
|---|---|
| `TABOO_DATABASE_URL` | PostgreSQL control DSN；不设则 SQLite |
| `TABOO_DATABASE_RUNTIME_ROLE` | 预建的独立运行角色，默认 `taboo_runtime` |
| `TABOO_PUBLIC_ORIGIN` | 精确回调和 CSRF Origin |
| `TABOO_ISSUER_ALLOWLIST` | 操作员允许的完整 OIDC/OAuth issuer |
| `TABOO_TRUSTED_PROXIES` | 可设置 `X-Real-IP`/`X-Forwarded-For` 的直连代理 IP/CIDR，逗号分隔；默认本机与私网，`none` 表示不采信 |
| `TABOO_DISABLE_REGISTER` / `TABOO_DISABLE_PASSWORD` | 全局关闭本地注册 / 密码登录，默认 false |
| `TABOO_MASTER_KEY` / `TABOO_JWT_SECRET` | PG 必须显式配置，所有副本相同 |

SQLite 只运行一个实例。PostgreSQL 多实例共享数据库、上述根材料和审计导出文件目录；使用同一发行版本，后台任务以数据库租约协调。Helm 会拒绝 SQLite 多副本，以及未配置 ReadWriteMany 数据卷的 PG 多副本。看 [升级恢复手册](runbooks/standard-federation-release.zh-CN.md) 后再升级已有数据。
