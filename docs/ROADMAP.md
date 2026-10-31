# taboo 开发路线图（2026-10-31 起）

> 本文件是 2026-10-31 之后的执行路线图，由 Phase 0 评估产出。历史里程碑 M0–M5 与“标准身份联邦 P0”见 README 与 `docs/design/standard-federation-acceptance.zh-CN.md`。
> 原则：编码与调试在本机（macOS arm64，可用本机 Docker）完成；每个阶段完成后部署到开发服务器 dev122（`ssh dev122-root`，x86_64）并在 `https://taboo-dev122.swetcc.com` 上验证。
> 凭据只引用位置，不写入仓库：dev122 `/home/cloudzh/dev-services-credentials.txt`、`/var/snap/docker/common/swetcc-dev/`、taboo 部署目录 `/var/snap/docker/common/swetcc-dev/taboo/.env`（0600）。旧目录 `/home/cloudzh/taboo-dev122/` 保留不动。

## 0. 现状评估（Phase 0，2026-10-31）

### 0.1 产品与架构

- taboo（禁制）：开源自部署密钥管理平台（Infisical / Vault 轻量替代），MIT。
- 后端：Go 1.27 模块化单体 `apps/server-go`（chi、`modernc.org/sqlite` 或 PostgreSQL 18 + FORCE RLS、`lib/pq`），前端经 `embed.FS` 打进单二进制；附带 `taboo-migrate`（verify / envelopes / import-sqlite）。
- 前端：React 19 + TS + Vite（`apps/web`），中文管理台，深色主题（`DESIGN.md`、`UX-CONTRACT.md`）。
- 其他：Node CLI（`apps/cli`，含 151 条规则 scan）、MCP Server（`apps/mcp`，Go）、Go SDK（`packages/sdk-go`）、OpenAPI 3.1 契约（`api/openapi.yaml`，62 个路径，路由漂移测试）、Node MVP 参考后端（`apps/server`，仅作契约回归）。
- 部署工具：`apps/server-go/Dockerfile`（多阶段源码构建）、`deploy/Dockerfile.release`（GoReleaser 用）、`docker-compose.yml`（SQLite 单容器）、`deploy/compose.postgres.yml` + `deploy/postgres/initialize.sh`（PG 18 control/runtime 角色）、Helm chart `deploy/helm/taboo`、`.goreleaser.yaml`（多平台、cosign、SBOM）、GitHub Actions `ci.yml`（8 个 job）/ `release.yml`。

### 0.2 已实现（按 README 与验收记录，代码已核对）

信封加密（KEK→组织 DEK→值，AES-256-GCM，版本化 AAD）；组织/项目/环境/多级文件夹；密钥 CRUD、版本、回滚、reveal 分离、CAS + 幂等键；Argon2id、JWT + refresh 旋转、登录限流、TOTP 2FA；RBAC 四角色 + 六项独立秘密动作授权；机器身份（client_credentials）与外部 OAuth 工作负载（RFC 9068 / 7662）；OIDC BFF 登录、会话策略、身份替换；成员与邀请；审计（append-only、验链、CSV/JSONL 导出）；动态密钥（PostgreSQL lease + 回收）；Secret Sync（GitHub/Vercel/Cloudflare）、Webhooks（HMAC）；`--check` 自检；`backup` 子命令；持久 outbox / 租约 / 死信。

### 0.3 设计文档中尚未实现（对照 `doc/OpenVault_…md` §2/§8/§9）

| 项 | 设计优先级 | 现状 |
|---|---|---|
| 密钥引用 `${project:env:KEY}` 运行时解析 | Must | 未实现 |
| SDK：Python、Node.js | Must | 仅 Go SDK；计划由 OpenAPI 生成 |
| 版本对比 diff 视图 | 前端页面清单 | 未实现（仅版本抽屉 + 回滚） |
| 密钥扫描报告页（CLI 结果上传汇总） | 前端页面清单 | 未实现（README 标注遗留） |
| 环境级权限矩阵 UI | 前端页面清单 | 有成员动作授权，矩阵视图待评估 |
| 导入：.env / Doppler / 1Password | Should | 未实现 |
| 动态密钥 MySQL 引擎 | Should | 仅 PostgreSQL |
| 密钥轮换策略（定时触发外部轮换器） | Should | 未实现 |
| SAML SSO | Should | 仅 OIDC |
| 日志中间件自动脱敏 `[REDACTED]`、机器 token IP 白名单 | §8 安全清单 | 未发现实现，待核对 |
| 审批流、访问请求（临时权限） | Could | 未实现 |
| SCIM、AuthZEN、Back-Channel Logout、个人 CLI 浏览器登录 | 联邦 P1 | 明确不在 P0 |
| 多副本 HA | Could | PG 模式支持，未生产验证 |

### 0.4 本机构建与测试结果（macOS arm64，Node 25.9.0，Go 1.26.3 自动切换 1.27 工具链，Docker 29.8.2）

| 检查 | 结果 |
|---|---|
| `npm ci` | 通过（副作用：`apps/cli/taboo.js` 被加上可执行位，已还原） |
| `npm run build`（web） | 通过 |
| `tsc --noEmit` | 通过 |
| `make -C apps/server-go build` | 通过 |
| `go vet` / `go test ./...`（server-go） | 通过 |
| `go test`（sdk-go、mcp） | 通过 |
| `node --test apps/cli/oauth.test.js` | 通过 |
| 14 项端到端冒烟（Go server，SQLite） | 全部通过 |
| Keycloak/浏览器/PG/容器升级等重型套件（`scripts/test-*.sh`） | Phase 1 已在本机运行，全部通过（见 1.4） |

本机未安装 goreleaser、helm。Git：`develop` 分支，比 `origin/develop` 超前 1 个提交（c4cfe39 文档），工作区干净。

### 0.5 dev122 现状（只读检查）

- 主机 x86_64、8 核、30 GB 内存、磁盘余量充足；Docker 29.8.0。
- 2026-10-08 环境重建删除了旧容器和卷：**当前没有任何 taboo 容器或卷在运行**。残留：镜像 `taboo:dev-b4c39c6`（旧版本）、目录 `/home/cloudzh/taboo-dev122/`（README、`.env` 根密钥材料、`bootstrap-owner.json`、初始备份 dump；缺 compose 文件）。旧部署只有一个引导账号和一个已删除的验证密钥，没有业务数据。
- 可复用基础服务（Docker 网络 `swetcc`）：
  - `swetcc-postgres`：PostgreSQL 18.6，`postgres:5432`，强制 TLS（`hostssl` + scram，内部 CA `/var/snap/docker/common/swetcc-dev/infra/tls/ca.crt`）。约定每个服务独立数据库与运行账号。
  - `nginx-proxy-manager` 2.16（80/443/81，绑定 10.0.30.122），证书统一用 Let's Encrypt **DNS-01（Aliyun）**，无需公网可达；管理账号位置见 `infra/npm/admin.json`。
  - `swetcc-mailpit`（SMTP 捕获）、`identity-v2-api`（OIDC 提供方，`identity-dev122.swetcc.com`，平台管理员尚未初始化）、`woodpecker-server`（CI，`ci-dev122.swetcc.com`）、本地 registry / athens（Go 代理）/ verdaccio（npm 代理）/ devpi。
- taboo 主机名（原计划 `taboo.swetcc.com`，2026-10-31 改为 `taboo-dev122.swetcc.com`）：NPM 中无代理主机、无证书；公网与内网 DNS 都没有记录。其他 `*-dev122.swetcc.com` 同样没有 DNS 记录，只靠 Mac 与 dev122 的 `/etc/hosts` 指向 10.0.30.122。

## 阶段总览

| 阶段 | 目标 |
|---|---|
| Phase 1 | 当前 develop 在 dev122 重新部署，复用共享 PG 与 NPM，`https://taboo-dev122.swetcc.com` 可用并经过端到端验证 |
| Phase 2 | 部署可重复、可回滚、可恢复：仓库内 dev122 部署脚本、备份/恢复演练、版本可见、重型测试套件本机常态化 |
| Phase 3 | 「使用 GitHub 登录」（GitHub OAuth App，OAuth2 授权码）：准入策略、账号绑定/JIT、审计与撤权；dev122 默认关闭，凭据由用户在 dev122 自行写入后启用 |
| Phase 4 | 补齐设计文档 Must 缺口：密钥引用、.env 导入/导出增强、版本 diff、扫描报告页、Node/Python SDK |
| Phase 5 | Should 能力：MySQL 动态引擎、轮换策略、Doppler/1Password 导入、安全清单补齐（日志脱敏、IP 白名单） |
| Phase 6 | Could / 联邦 P1：审批流、访问请求、SCIM、Back-Channel Logout、CLI 浏览器登录、HA 验证 |

每个阶段的统一流程：从 `develop` 切分支 `phase-N-*` → 本机开发 → 本机全量验证（见 §通用验证）→ 构建 linux/amd64 镜像 → 部署 dev122 → 线上验证 → 更新本文件“状态”与验收记录 → 提交并 push 到 `origin/develop`（合并 `main` 需用户确认）。

---

## Phase 1：恢复 dev122 部署并启用 HTTPS

**目标**：当前 `develop`（schema 15）以 PostgreSQL 模式运行在 dev122，复用 `swetcc-postgres` 与 NPM，通过 `https://taboo-dev122.swetcc.com`（Let's Encrypt）访问，冒烟与浏览器验证通过。

### 1.1 本机准备

1. 运行重型套件确认基线：`./scripts/test-postgres.sh`、`./scripts/test-consumers.sh`、`TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh`、`docker build -f apps/server-go/Dockerfile`、`node scripts/test-container-recovery.mjs`。失败项先修复或记录。
2. 冒烟脚本可指向任意地址：`apps/server/scripts/smoke.js` 已支持 `TABOO_BASE`，确认可以对 HTTPS 地址运行（自签/LE 证书、Cookie/CSRF 不影响 Bearer 流程）。
3. 构建 amd64 镜像：`docker buildx build --platform linux/amd64 -f apps/server-go/Dockerfile -t taboo:dev-<sha> --load .`（本机 arm64，需 QEMU/buildx；如太慢，改为把 `git archive` 传到 dev122 原生构建，构建时使用 dev122 的 athens / verdaccio 代理）。
4. 新增 `deploy/dev122/`（不含任何秘密）：
   - `compose.yaml`：只有 `taboo` 服务，加入外部网络 `swetcc`，`127.0.0.1:7100` 发布（仅供主机调试），镜像用 `${TABOO_IMAGE}` 固定 ID；挂载 CA 只读到 `/etc/taboo/ca.crt`，数据卷 `taboo_data:/data`。
   - `env.example`：列出变量名（`TABOO_DATABASE_URL`、`TABOO_DATABASE_RUNTIME_ROLE`、`TABOO_MASTER_KEY`、`TABOO_JWT_SECRET`、`TABOO_PUBLIC_ORIGIN=https://taboo-dev122.swetcc.com`、`TABOO_CORS_ORIGIN`、`TABOO_DISABLE_REGISTER` 等）。
   - `init-db.sql`：在共享 PG 创建 `taboo` 库、`taboo_control`（LOGIN，NOSUPERUSER，NOBYPASSRLS）与 `taboo_runtime`（NOLOGIN），等价 `deploy/postgres/initialize.sh`。
   - `deploy.sh`：传镜像（`docker save | ssh docker load`）、同步 compose、`docker compose up -d`、等待 readyz、运行冒烟。
   - `README.md`：运维说明。

### 1.2 dev122 部署

1. ~~备份残留目录~~（用户决定：旧目录 `/home/cloudzh/taboo-dev122/` 原样保留，不复制、不删除；新部署用独立目录 `/var/snap/docker/common/swetcc-dev/taboo`）。
2. 用共享 PG 管理员（凭据见 credentials 文件）执行 `init-db.sql`；`taboo_control` 密码随机生成（URI 安全），写入 dev122 上 0600 的 `.env`。DSN：`postgres://taboo_control:***@swetcc-postgres:5432/taboo?sslmode=verify-full&sslrootcert=/etc/taboo/pg-ca.crt`（实测：`swetcc` 网络上 `postgres` 别名同时属于 `swetcc-powerdata-postgres`（无 TLS），必须用 `swetcc-postgres`；证书 SAN 含该名）。
3. 根密钥：默认**全新生成** `TABOO_MASTER_KEY` / `TABOO_JWT_SECRET`（旧部署无业务数据）；旧 `.env` 保留在备份目录。
4. `docker compose up -d`；确认 `readyz`、`taboo-server --check`，日志中 schema 15、RLS 角色检查通过。
5. NPM：
   - 证书：Let's Encrypt via DNS（Aliyun），域名 `taboo-dev122.swetcc.com`，复用 NPM 已保存的 Aliyun DNS 凭据配置方式（与 `npm-13` 相同）；传播等待 300 秒。
   - Proxy Host：`taboo-dev122.swetcc.com` → `http://taboo:7100`（swetcc 网络），开启 Force SSL、HTTP/2、HSTS、WebSocket。
6. 名称解析：在 dev122 的 `/etc/hosts` 增加 `10.0.30.122 taboo-dev122.swetcc.com`（与现有 `*-dev122` 一致）；Mac 的 `/etc/hosts` 需要 sudo，由用户添加（与其他 `*-dev122` 相同，不加 DNS 记录）。
7. 引导 owner：调用注册接口创建 owner（凭据写入 dev122 0600 文件），然后设置 `TABOO_DISABLE_REGISTER=true` 并重启。

### 1.3 验证

- `curl -I https://taboo-dev122.swetcc.com` 返回 200，证书由 Let's Encrypt 签发，HTTP 跳转 HTTPS。
- `TABOO_BASE=https://taboo-dev122.swetcc.com npm run smoke`（14 项；如果注册已关闭，在关闭前先运行）。
- 重启 taboo 容器后原密钥仍能解密；PG 中 `taboo_runtime` 下 FORCE RLS 表数量与验收记录一致。
- 浏览器：登录页、登录、项目/环境切换、密钥创建/reveal/复制、审计页在 `https://taboo-dev122.swetcc.com` 上无控制台错误；390px 窄屏检查。
- `pg_dump` 一次初始备份到 `/var/snap/docker/common/swetcc-dev/taboo/backups/`（0600）。

**交付**：`deploy/dev122/*`、本文件 Phase 1 状态更新、部署记录（不含秘密）。

### 1.4 执行记录（2026-10-31，完成）

实际做法与计划的差异：镜像改为在 dev122 原生构建（`deploy.sh` 用 `git ls-files` 打包源码传过去，`docker build` 走 dev122 镜像缓存；不用 buildx/QEMU）；DB 初始化用 `init-db.sh`（经容器本地 socket 以管理员执行，幂等），不是 `init-db.sql`；NPM 证书与 Proxy Host 由 `npm-proxy.py` 经 NPM API 创建。

| 项 | 结果 |
|---|---|
| 本机 `TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh`（-race，PG 18 + Keycloak） | 通过 |
| 本机 `./scripts/test-consumers.sh` | 通过 |
| 本机 `TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh`（Playwright 7 项） | 7/7 通过 |
| 本机 `docker build` + `node scripts/test-container-recovery.mjs` | 通过（fresh install、重启、静默恢复、解密校验、缺失/错误根密钥拒绝） |
| dev122 镜像 | `taboo:dev-c4cfe39`（linux/amd64，56 MB） |
| 容器 | `taboo`，网络 `swetcc`，`127.0.0.1:7100`，healthy |
| 数据库 | `swetcc-postgres` 上的 `taboo` 库；`taboo_control`/`taboo_runtime`；连接 TLSv1.3（`pg_stat_ssl` 确认）；39 张表中 38 张 FORCE RLS（唯一例外 `schema_version`） |
| `taboo-server --check` | all checks passed（schema 15） |
| 证书 | NPM cert #14（`taboo.swetcc.com`），Let's Encrypt（YE2），DNS-01 aliyun；主机名更换后已删除，现为 cert #15，见 3.4 |
| Proxy Host | NPM #15（`taboo.swetcc.com`）→ `taboo:7100`，Force SSL、HTTP/2、HSTS、WebSocket；HTTP 301 → HTTPS；主机名更换后已删除，现为 #16，见 3.4 |
| HTTPS 冒烟（dev122 上经 hosts，`TABOO_BASE=https://taboo-dev122.swetcc.com`） | 14/14 通过（在关闭注册之前执行，留下一个 `smoke-*@taboo.dev` 测试账号） |
| 引导 owner | 已创建，凭据在 dev122 `/var/snap/docker/common/swetcc-dev/taboo/bootstrap-owner.json`（0600）；之后 `TABOO_DISABLE_REGISTER=true`，注册返回 403 |
| 重启后解密 | owner 登录写入密钥 → `docker restart taboo` → 登录并 reveal，值一致 |
| 浏览器（Playwright，1280px 与 390px） | 登录、工作区、密钥列表正常，控制台无错误 |
| 备份 | `backups/taboo-20261031-p1.dump`（0600，pg_restore -l 可读） |
| 名称解析 | 只用 hosts（与其他 `*-dev122` 一致，无 DNS 记录）；dev122 `/etc/hosts` 已有 `10.0.30.122 taboo-dev122.swetcc.com`（备份 `/etc/hosts.bak-taboo-20261031`、`/etc/hosts.bak-taboo-dev122-20261031`） |

遗留问题（转入 Phase 2，均已在 Phase 2 解决）：
- ~~390px 窄屏下密钥表格的操作按钮横向溢出（页面可横向滚动）~~ → ≤640px 卡片布局。
- ~~列表“更新时间”显示 UTC，未按浏览器时区显示~~ → `apps/web/src/time.ts` 按浏览器时区显示，悬停显示 UTC。
- ~~镜像未注入版本号~~ → ldflags + OCI 标签。
- ~~冒烟测试遗留 `smoke-*@taboo.dev` 账号~~ → 已禁用（未删除）；以后冒烟使用专用账号且结束后禁用。

---

## Phase 2：可重复部署、备份恢复与工程基线

**目标**：任何人从本机一条命令即可升级 dev122 并能回滚、恢复；重型测试本机常态化。

任务：
1. `deploy/dev122/deploy.sh` 完善：参数 `--image`/`--rollback`；升级前自动停机备份（`pg_dump` + `/data` + 记录镜像 ID），按 `docs/runbooks/standard-federation-release.zh-CN.md` 顺序执行；保留最近 N 份。
2. 版本可见：`/api/v1/healthz` 或新端点返回 `version`/`commit`（Dockerfile 注入 `-X main.version`），前端页脚显示；OpenAPI 同步更新并通过漂移测试。
3. 备份恢复演练：在 dev122 临时库 `taboo_restore_test` 恢复最新 dump + 原根密钥，用 `taboo-migrate verify` 校验，演练结果写入 runbook。
4. 定时备份：dev122 cron 或 systemd timer 每日 `pg_dump`，保留 7 份。
5. 本机脚本 `scripts/verify-all.sh`：串起 §通用验证 全部命令，输出汇总。
6. 可选：Woodpecker（`ci-dev122`）流水线，在 `develop` push 时构建 amd64 镜像并推本地 registry（需用户确认是否启用及仓库授权）。

验证：一次完整的“升级 → 冒烟 → 回滚 → 冒烟 → 再升级”；恢复演练数据一致；版本号在 UI/接口中可见。部署：用新脚本把 Phase 2 版本发到 dev122。

### 2.1 执行记录（2026-10-31，完成）

| 项 | 结果 |
|---|---|
| 版本可见 | `internal/buildinfo`；`taboo-server --version`；`/api/v1/healthz` 返回 `version/commit/build_time`（OpenAPI + api-types 同步）；镜像 OCI 标签；前端侧栏与登录页 `taboo v0.1.0 · d7093f4` |
| 运维脚本 | `deploy/dev122/taboo-ctl.sh`：status / upgrade / rollback / history / backup / backups / restore-drill / restore / smoke / user / install-timers；`deploy.sh --image`、`--rollback` |
| 账号管理 | `taboo-server user create|status|enable|disable`（审计 `admin.user.*`，禁用即吊销会话）；登录拒绝非 active 用户 |
| 本机全量验证 | `scripts/verify-all.sh --container`：14 步全部 PASS（含 consumers、federation+browser、postgres+keycloak、docker build、容器恢复） |
| dev122 升级 | `taboo:dev-c4cfe39` → `taboo:dev-d7093f4`，升级前备份 `20261031-144553-pre-upgrade` |
| 回滚演练 | 升级 → 冒烟 15/15 → 回滚到 c4cfe39 → 冒烟 15/15（SQL 兜底启停账号）→ 再升级 d7093f4 → 冒烟 15/15；`releases.log` 记录 baseline/upgrade/rollback/upgrade |
| 浏览器检查 | `scripts/browser-check.mjs` 经 HTTPS 对 dev122：1280 与 390 均通过（版本显示、更新时间为 Asia/Shanghai 本地时间、无横向溢出、操作按钮完整可见） |
| 恢复演练 | `20261031-144859-pre-upgrade` → `taboo_restore_test`：39 表 154 行一致，`taboo-migrate verify` 通过，临时实例 readyz OK，PASS（见 `docs/runbooks/dev122-operations.zh-CN.md`） |
| 定时器 | `taboo-backup.timer` 每日 03:30（保留 7 份），`taboo-restore-drill.timer` 每周日 04:15；已手动触发一次 daily 备份成功 |
| 遗留账号 | `smoke-1793427846712@taboo.dev` 已禁用（未删除） |

与计划的差异：
- 备份为在线 `pg_dump`（一致性快照），未停机；回滚仅切换镜像，数据回退走 `restore`。
- 冒烟改为专用账号 `smoke@taboo.swetcc.com`（平时禁用，冒烟时临时启用），冒烟共 15 项（新增清理步骤）。
- Woodpecker CI 跳过（可选项，待用户决定）。
- 备份仍在 dev122 本机，异地拷贝待安排。

---

## Phase 3：使用 GitHub 登录（GitHub OAuth App）

**决定（2026-10-31，用户）**：第三方人员登录使用 GitHub OAuth App，替代原计划的 Identity V2 / Keycloak 接入。GitHub 是 OAuth2（无 ID Token / discovery / JWKS），
因此不复用组织级 OIDC 联邦（BFF 会话、上游刷新、issuer 白名单），而是实现为**实例级、运维配置**的登录方式，成功后签发普通的本地 Taboo 会话（access 15 分钟 + 旋转 refresh）。
组织级标准 OIDC 联邦保持不变。

**现状核对**：原 OIDC 联邦代码要求 discovery + ID Token 验签，GitHub 不支持；需新增提供方。

### 3.1 设计

| 方面 | 做法 |
|---|---|
| 协议 | 授权码 + `state`（常量时间比较）+ PKCE S256（GitHub 已支持）；`allow_signup=false`；scope `read:user user:email`，仅当配置组织/团队准入时加 `read:org` |
| 流程状态 | 不改 schema：流程参数（state、verifier、模式、绑定用户、到期）用 master key 派生密钥加密进 HttpOnly cookie `taboo.github.flow`（Path `/api/v1/auth/github`，SameSite=Lax，10 分钟）；回调成功后发 60 秒一次性 `taboo.github.ticket`（SameSite=Strict），前端 `POST /auth/github/session`（校验 Origin）换 token，token 不出现在 URL |
| 身份 | `GET /user` + `GET /user/emails`，必须存在 primary 且 verified 的邮箱；按 `external_identities(issuer=https://github.com, subject=<GitHub 数字 ID>)` 绑定，改名不影响；不按邮箱合并 |
| 准入 | `TABOO_GITHUB_ALLOWED_USERS` / `_ALLOWED_ORGS`（`/user/memberships/orgs/{org}` state=active）/ `_ALLOWED_TEAMS`（`org/team`）任一满足；`_ALLOW_ANY=true` 显式放开；配置了凭据但无策略时服务拒绝启动（fail closed）。每次登录与绑定都重新检查 |
| 账号 | 默认必须先由本地账号在「GitHub」面板绑定（`not_linked`）；`TABOO_GITHUB_AUTOCREATE=true` 时 JIT 创建无密码本地账号（个人组织 + 默认项目 + 三环境），邮箱冲突拒绝（`email_in_use`），可选 `JOIN_ORG` + `JOIN_ROLE`（viewer/developer，禁止 owner/admin） |
| 2FA | 已开 TOTP 的账号经 GitHub 登录后仍需动态码（复用 challenge 流程） |
| 撤权 | GitHub token 回调内一次使用后立即调用 `DELETE /applications/{client_id}/token` 吊销，不落库；移出名单/组织/团队下一次登录即拒绝；`taboo-server user disable` 立即使 access/refresh/浏览器会话失效；解绑时吊销该用户全部 refresh token；无密码账号禁止解绑（防锁死） |
| 审计 | `auth.github.login`、`auth.github.provision`、`auth.github.link`、`auth.github.unlink`、`auth.github.denied`（meta.reason，如 state_mismatch / not_allowed / not_linked / email_in_use / account_disabled） |
| 配置 | `TABOO_GITHUB_CLIENT_ID`、`TABOO_GITHUB_CLIENT_SECRET`（或 `_SECRET_FILE`）及上面的策略变量；回调固定为 `<TABOO_PUBLIC_ORIGIN>/api/v1/auth/github/callback` |

### 3.2 任务

1. `internal/github`：配置解析、授权 URL、回调、session 换取、绑定状态/发起绑定/解绑。
2. 路由 `/api/v1/auth/github/{login,callback,session,link}`（限流），`/auth/sources` 增加 `github_enabled`；OpenAPI + `npm run types`。
3. 前端：登录页「使用 GitHub 登录」按钮、回调落地（ok/error 中文原因）、顶栏「GitHub」绑定面板。
4. 测试：模拟 GitHub（token 端点校验 client 凭据、redirect_uri、PKCE verifier；/user、/user/emails、组织/团队成员、token 吊销），覆盖关闭态、绑定→登录→解绑、各类拒绝、组织/团队准入、JIT 与邮箱冲突、停用账号。SQLite 与 PG（RLS）都跑。
5. dev122：`env.example` / README / 运维手册第 6 节；`taboo-ctl.sh reload`；部署时**不配置**凭据（保持关闭），验证 `github_enabled:false`、`/auth/github/login` 404。
6. 用户创建 OAuth App 并在 dev122 `.env` 写入凭据后：真实登录联调，补记执行记录。

验证：`./scripts/verify-all.sh --container`；dev122 `taboo-ctl.sh smoke`；启用后用真实 GitHub 账号完成 绑定 / 登录 / 被拒（不在名单）/ 解绑 并核对审计。

### 3.3 执行记录（2026-10-31，代码完成，待凭据）

- 实现见 `apps/server-go/internal/github/`，测试 `internal/server/github_test.go`（5 组，模拟 GitHub 端点）。
- dev122 已部署本阶段镜像，GitHub 登录处于关闭态；启用步骤见 `docs/runbooks/dev122-operations.zh-CN.md` §6。
- OAuth App 参数：Homepage `https://taboo-dev122.swetcc.com`，Authorization callback `https://taboo-dev122.swetcc.com/api/v1/auth/github/callback`。
- 原 Phase 3 的真实 OIDC 提供方（Identity V2 / Keycloak）联调移入 Phase 6 备选，不再阻塞。

### 3.4 主机名更换（2026-10-31）

用户要求开发环境主机名统一为 `*-dev122` 形式：`taboo.swetcc.com` → `taboo-dev122.swetcc.com`。

- NPM：新证书 cert #15（Let's Encrypt，DNS-01 aliyun），新 Proxy Host #16 → `taboo:7100`（Force SSL、HTTP/2、HSTS、WebSocket）；旧 Proxy Host #15、旧证书 #14 已删除，其他主机未改动。
- dev122 `.env`：`TABOO_PUBLIC_ORIGIN` / `TABOO_CORS_ORIGIN` 改为 `https://taboo-dev122.swetcc.com`；GitHub 回调随之变为 `https://taboo-dev122.swetcc.com/api/v1/auth/github/callback`。
- 名称解析：与其他 `*-dev122` 一样只用 `/etc/hosts`（公网 DNS 无任何 `*-dev122` 记录）。
- 账号邮箱 `smoke@taboo.swetcc.com`、`owner@taboo.swetcc.com` 是已存在账号的标识，不随主机名变化。

---

## Phase 4：设计文档 Must 缺口

**目标**：补齐 v1.0 设计文档中仍缺的开发者体验能力。

| 子项 | 关键改动 | 验证 |
|---|---|---|
| 4.1 密钥引用 `${project:env:KEY}` | `internal/secret` 增加解析器（同项目 `${env:KEY}`、跨项目 `${project:env:KEY}`），只在 reveal/export/CLI run 时解析；逐个引用重新授权并记审计；循环/深度限制；OpenAPI 增加 `resolve` 参数 | Go 单测（循环、越权、缺失）、冒烟扩展、CLI `run` 注入解析后值 |
| 4.2 .env 导入 | `POST /projects/{pid}/import`（dotenv 解析、冲突策略 skip/overwrite、单事务 + 审计）；CLI `taboo import .env`；前端导入对话框（native Modal） | 单测 + 浏览器场景 + 冒烟 |
| 4.3 版本 diff | 前端版本抽屉选两个版本对比（需 reveal 权限，值默认掩码），不新增明文存储 | Playwright：权限不足不可见、Escape/焦点恢复 |
| 4.4 扫描报告页 | `POST /orgs/{slug}/scan-reports`（只上传规则 ID/文件/行号/指纹，不上传命中原文）；CLI `scan --upload`；前端报告列表 | 单测确认不存原文；浏览器场景 |
| 4.5 Node/Python SDK | `packages/sdk-node`（openapi-typescript + 手写薄封装）、`packages/sdk-python`（openapi-python-client 或手写）；与 Go SDK 同样的 consumers 测试 | `scripts/test-consumers.sh` 扩展到三种 SDK |

每个子项都同步 `api/openapi.yaml` → `npm run types` → 契约漂移测试。阶段结束部署 dev122，并在线上运行扩展后的冒烟。

---

## Phase 5：Should 能力与安全清单

1. MySQL 动态引擎：`internal/dynamic` 抽象 `Engine` 接口，新增 MySQL 实现（`CREATE USER … PASSWORD EXPIRE`、到期 DROP）；dev122 临时 MySQL 容器联调，测试后删除。
2. 轮换策略：按密钥配置 cron/间隔，到期发出 outbox 事件 → webhook 通知外部轮换器回写新版本；审计。
3. 导入扩展：Doppler（JSON 导出）、1Password CLI（`op` JSON）格式适配器，复用 4.2 管道。
4. 安全清单：请求日志脱敏中间件（password/secret/token/value 字段）、机器身份 token 可选 IP/CIDR 白名单、响应 `Cache-Control: no-store` 全覆盖检查。
5. 评估 SAML（可能留待 Phase 6）。

验证：各自单测 + 真实 MySQL 联调 + 冒烟扩展；部署 dev122。

---

## Phase 6：Could / 联邦 P1

（备选）真实 OIDC 提供方（Identity V2 / Keycloak）在 dev122 联调；审批流（修改 prod 需二次批准）、访问请求（限时提权、到期自动回收）、SCIM 用户同步、Back-Channel Logout、AuthZEN、CLI 浏览器登录（device/PKCE loopback）、PG 多副本 + 共享导出卷 HA 验证。开始前单独写设计文档并与用户确认范围。

---

## 通用验证（每阶段必须通过）

一条命令：`./scripts/verify-all.sh --container`（`--quick` 跳过重型套件；`VERIFY_LOG_DIR` 指定日志目录）。等价于：

```sh
npm ci && git checkout -- apps/cli/taboo.js 2>/dev/null || true   # npm ci 会改可执行位
npm run types && git diff --exit-code -- apps/web/src/api-types.ts
npm exec --workspace=@taboo/web -- tsc --noEmit
npm run build
go -C apps/server-go vet ./... && go -C apps/server-go test -race ./...
go -C packages/sdk-go test -race ./... && go -C apps/mcp test -race ./...
node --test apps/cli/oauth.test.js
make -C apps/server-go build && (TABOO_PORT=7199 TABOO_DATA_DIR=$(mktemp -d) ./apps/server-go/bin/taboo-server &) && TABOO_BASE=http://localhost:7199 npm run smoke
./scripts/test-consumers.sh
TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh
TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh
```

线上：`ssh dev122-root /var/snap/docker/common/swetcc-dev/taboo/taboo-ctl.sh smoke`（专用账号，15 项）+ `scripts/browser-check.mjs`（1280/390）。

## 需用户决定 / 风险

1. ~~名称解析~~：已决定，主机名 `taboo-dev122.swetcc.com`，与其他 `*-dev122` 一样只用 `/etc/hosts`（不加 DNS 记录）；Mac 需用户用 sudo 添加 `10.0.30.122 taboo-dev122.swetcc.com`。
2. ~~dev122 旧部署~~：已决定，全新初始化，旧目录保留不动。
3. ~~Phase 3 提供方~~：已决定，使用 GitHub OAuth App；待用户在 GitHub 创建 OAuth App 并在 dev122 `.env` 写入凭据。
4. Git：已决定每阶段 push 到 `origin/develop`；是否合并 `main`、是否启用 Woodpecker CI 待定。
5. ~~架构差异~~：Phase 1 采用 dev122 原生构建（`deploy/dev122/deploy.sh`）。

## 状态

| 阶段 | 状态 | 日期 |
|---|---|---|
| Phase 0 评估与计划 | 完成 | 2026-10-31 |
| Phase 1 dev122 部署与 HTTPS | 完成（主机名已改为 taboo-dev122.swetcc.com） | 2026-10-31 |
| Phase 2 可重复部署、备份恢复与工程基线 | 完成（Woodpecker 跳过） | 2026-10-31 |
| Phase 3 GitHub OAuth 登录 | 代码完成并部署（待 OAuth App 凭据） | 2026-10-31 |
| Phase 4 | 未开始 | |
| Phase 5 | 未开始 | |
| Phase 6 | 未开始 | |
