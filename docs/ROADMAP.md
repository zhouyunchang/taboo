# taboo 开发路线图（2026-10-31 起，更新至 2026-11-12）

> 本文件是 2026-10-31 之后的执行路线图，由 Phase 0 评估产出。历史里程碑 M0–M5 与“标准身份联邦 P0”见 README 与 `docs/design/standard-federation-acceptance.zh-CN.md`。
> **当前状态（2026-11-12）**：Phase 0–6 已全部完成。Phase 7（个人空间/组织模型 + 控制台重设计）功能代码完成并 push（`d6af762`），但 CI、dev122 部署、文档与全量验证尚未收口；先看下文 Phase 7 的收尾任务。
> 原则：编码与调试在本机（macOS arm64，可用本机 Docker）完成；每个阶段完成后部署到开发服务器 dev122（`ssh dev122-root`，x86_64）并在 `https://taboo-dev122.swetcc.com` 上验证。
> 凭据只引用位置，不写入仓库：dev122 `/home/cloudzh/dev-services-credentials.txt`、`/var/snap/docker/common/swetcc-dev/`、taboo 部署目录 `/var/snap/docker/common/swetcc-dev/taboo/.env`（0600）。旧目录 `/home/cloudzh/taboo-dev122/` 保留不动。

## 0. 现状评估（Phase 0，2026-10-31）

### 0.1 产品与架构

- taboo（禁制）：开源自部署密钥管理平台（Infisical / Vault 轻量替代），MIT。
- 后端：Go 1.27 模块化单体 `apps/server-go`（chi、`modernc.org/sqlite` 或 PostgreSQL 18 + FORCE RLS、`lib/pq`），前端经 `embed.FS` 打进单二进制；运维命令经 `taboo-server migrate`（verify / envelopes / import-sqlite）。
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
| `just build` | 通过 |
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
| Phase 6 | Could / 联邦 P1：审批流、访问请求、SCIM、Back-Channel Logout、CLI 浏览器登录、PostgreSQL 多副本。SAML 与 AuthZEN 继续延期。已部署 dev122（schema 18，`taboo:dev-6beaa0c`） |
| Phase 7 | 个人空间/组织模型与控制台重设计：取消环境、locator 寻址、结构基线重建、墨黑+朱砂控制台；把 `develop` 拉回可发布基线 |

每个阶段的统一流程：从 `develop` 切分支 `phase-N-*` → 本机开发 → 本机全量验证（见 §通用验证）→ 构建 linux/amd64 镜像 → 部署 dev122 → 线上验证 → 更新本文件“状态”与验收记录 → 提交并 push 到 `origin/develop`（合并 `main` 需用户确认）。

Phase 7 是控制台重设计带出的模型调整，直接在 `develop` 上迭代，未走 `phase-N-*` 分支；发布前的收尾仍按同一套验证与部署流程验收。

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
3. 备份恢复演练：在 dev122 临时库 `taboo_restore_test` 恢复最新 dump + 原根密钥，用 `taboo-server migrate verify` 校验，演练结果写入 runbook。
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
| 恢复演练 | `20261031-144859-pre-upgrade` → `taboo_restore_test`：39 表 154 行一致，`taboo-server migrate verify` 通过，临时实例 readyz OK，PASS（见 `docs/runbooks/dev122-operations.zh-CN.md`） |
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

### 3.3 执行记录（2026-10-31 代码完成；2026-11-01 端到端验证，完成）

- 实现见 `apps/server-go/internal/github/`，测试 `internal/server/github_test.go`（5 组，模拟 GitHub 端点）。
- dev122 已部署本阶段镜像，GitHub 登录处于关闭态；启用步骤见 `docs/runbooks/dev122-operations.zh-CN.md` §6。
- OAuth App 参数：Homepage `https://taboo-dev122.swetcc.com`，Authorization callback `https://taboo-dev122.swetcc.com/api/v1/auth/github/callback`。
- 原 Phase 3 的真实 OIDC 提供方（Identity V2 / Keycloak）联调移入 Phase 6 备选，不再阻塞。
- 2026-11-01 11:07–11:09 CST 用真实 GitHub 账号在 dev122 端到端验证：审计依次为 `auth.github.denied`（`not_linked`）、`auth.github.link`、`auth.github.login`，GitHub 用户 `zhouyunchang` 已绑定到 owner 账号。Phase 3 完成。

### 3.4 主机名更换（2026-10-31）

用户要求开发环境主机名统一为 `*-dev122` 形式：`taboo.swetcc.com` → `taboo-dev122.swetcc.com`。

- NPM：新证书 cert #15（Let's Encrypt，DNS-01 aliyun），新 Proxy Host #16 → `taboo:7100`（Force SSL、HTTP/2、HSTS、WebSocket）；旧 Proxy Host #15、旧证书 #14 已删除，其他主机未改动。
- dev122 `.env`：`TABOO_PUBLIC_ORIGIN` / `TABOO_CORS_ORIGIN` 改为 `https://taboo-dev122.swetcc.com`；GitHub 回调随之变为 `https://taboo-dev122.swetcc.com/api/v1/auth/github/callback`。
- 名称解析：与其他 `*-dev122` 一样只用 `/etc/hosts`（公网 DNS 无任何 `*-dev122` 记录）。
- 账号邮箱 `smoke@taboo.swetcc.com`、`owner@taboo.swetcc.com` 是已存在账号的标识，不随主机名变化。

---

## Phase 4：设计文档 Must 缺口

**目标**：补齐 v1.0 设计文档中仍缺的开发者体验能力。

**状态（2026-11-01）**：完成，已部署 dev122（`taboo:dev-967bdbd`，schema 16）。引用只在 `resolve=true` 的 reveal/export（CLI `get` / `export` / `run` 默认开启，`--raw` 关闭）时展开；目标固定为该环境根目录的 key。

| 子项 | 实现 | 验证 |
|---|---|---|
| 4.1 密钥引用 | `internal/secret/ref.go`：`${env:KEY}`、`${project:env:KEY}`。每个目标重新做 reveal 鉴权并写 `secrets.reference` 审计，不把目标明文写入审计。无权限返回 `REFERENCE_FORBIDDEN`（不区分目标是否存在）。有权限但缺失返回 `REFERENCE_NOT_FOUND` 并带上引用原文。循环 `REFERENCE_CYCLE`，深度 8，单值最多 64 次加载 | `ref_test.go`、`phase4_test.go`、三种 SDK consumer、CLI `get`/`run` |
| 4.2 .env 导入 | `POST /projects/{pid}/import`：注释、`export`、引号；`preview` 不写库；已有 key 未给策略时 `409 IMPORT_CONFLICT`；`skip` / `overwrite` 单事务 + 一条 `secrets.import` 审计。CLI `taboo import`，界面「导入 .env」 | dotenv 单测、`phase4_test.go`、浏览器导入对话框 |
| 4.3 版本 diff | `GET .../versions/{version}` 按 reveal 取该版本明文，不新增明文列。版本抽屉默认只显示增删行和长度，点「显示明文」才放入 DOM。无 reveal 权限不渲染对比控件 | `apps/web/tests/phase4.spec.ts`、`scripts/browser-check.mjs`（1280/390） |
| 4.4 扫描报告 | `scan_reports`（schema 16，PostgreSQL RLS）。`POST/GET /orgs/{slug}/scan-reports` 只接受规则、相对路径、行号、sha256 指纹；未知字段（含命中原文）直接 400。CLI `scan --upload`。控制台「扫描报告」 | `TestScanReportStoresNoSecretText`、CLI consumer 上传后回读 |
| 4.5 Node/Python SDK | `packages/sdk-node`（`@taboo/sdk`）、`packages/sdk-python`（`taboo.py`）手写对齐 Go SDK 的方法、幂等、机器身份和 OAuth client credentials，而不是生成另一套客户端 | `scripts/test-consumers.sh` 依次跑 Go、Node、Python |

OpenAPI、`apps/web/src/api-types.ts` 与 chi 路由一起更新。

**评审修正**：导入与扫描报告接口的 500 不再把内部错误原文返回给客户端（改为日志记录 + `internal error`）；PostgreSQL 测试的 FORCE RLS 断言加入 `scan_reports`；restore-drill 记录迁移后的 schema 版本；浏览器检查的扫描报告页不再假设为空。

**遗留（不阻塞）**：
- CLI 指纹为 `sha256(rule|file|line|match)`，未加盐；低熵命中可被能读报告的组织成员暴力还原。后续改为按组织密钥的 HMAC 指纹（Phase 5 安全清单）。
- 导入的文件夹在事务外创建，冲突时可能留下空文件夹；版本 reveal 审计不记录是否 resolve；扫描报告无删除接口；机器身份/工作负载可列出组织全部扫描报告。
- `secret.go` 中原有的 `err.Error()` 500 尚未统一处理。

**部署记录（2026-11-01，不含秘密）**：
- 本机 `./scripts/verify-all.sh --container` 14/14 通过。
- `deploy/dev122/deploy.sh` → `taboo:dev-967bdbd` healthy；升级前备份 `backups/20261101-104636-pre-upgrade`；`schema_version`=16，`scan_reports` RLS enable+force，39/40 张表 FORCE RLS，`taboo-server --check` 通过。
- `taboo-ctl.sh smoke` 15/15；`scripts/browser-check.mjs` 1280/390 全部通过（含导入对话框、版本对比遮罩、扫描报告页）。
- 线上接口核对 20 项通过：引用原文/展开/缺失 404、导出 resolve（坏引用 fail-closed、修复后展开）、导入 preview/409 不写库/skip、版本 v1/v2、扫描报告 201、含 `match` 与非 sha256 指纹 400、审计有事件且无明文。
- restore-drill（schema 15 的升级前备份）PASS：39 张表 3807 行一致，verify OK，临时实例迁移到 schema 16 后 readyz OK。

---

## Phase 5：Should 能力与安全清单

**状态（2026-11-01）**：完成，schema 17，已部署 dev122（`taboo:dev-d1c0cc0`）。

| 子项 | 实现 | 验证 |
|---|---|---|
| 5.1 MySQL 动态引擎 | `Engine` 按 `type` 分发。MySQL：`CREATE USER … IDENTIFIED BY … PASSWORD EXPIRE INTERVAL n DAY`、`GRANT SELECT`，续期走 `ALTER USER`，回收先 `ACCOUNT LOCK`，再 `KILL` 会话，最后 `DROP USER`。`PASSWORD EXPIRE` 最少 1 天，精确到期仍由既有 lease worker 删除账号。连接串接受 `mysql://user:pass@host:3306/db` 或 `user:pass@tcp(host:port)/db`，只保留用户、密码、TCP 地址、库名、TLS 名称，强制关闭 `allowAllFiles`、明文/旧密码认证、`multiStatements` | `TestMySQLDSN`、`TestMySQLDSNDropsDangerousFlags`、`TestMySQLGrantAndRevoke`（本机 Docker `mysql:8.4`）、`TestPhase5ImportRotationAndScanBoundaries` |
| 5.2 轮换策略 | `rotation_policies`。`PUT/GET/DELETE …/secrets/{key}/rotation`，间隔（最少 300 秒）或五段 UTC cron（两次触发至少间隔 5 分钟）。到期写入 outbox `rotation_due`，webhook 事件名 `secret.rotation_due`，不含明文。同步器忽略该事件。审计 `secrets.rotation.update/delete/due` | `TestCronNextAndMinimumGap`、Phase 5 集成测试、冒烟 |
| 5.3 导入 | `format=dotenv\|doppler\|1password`。Doppler 扁平 JSON 或 `secrets.*.computed`，跳过 `DOPPLER_*`。1Password `op` JSON 的 `fields[].value`，标签转成环境变量名。文件夹在导入事务内创建，冲突或审计失败会一起回滚 | `TestDopplerAndOnePasswordImport`、`TestImportFolderRollsBackWithAudit`、控制台格式选择 |
| 5.4 安全清单 | 访问日志脱敏中间件；机器身份 `allowed_cidrs`（签发和后续请求都检查，工作负载同样）；`Cache-Control: no-store` 放进安全响应头。SAML 留在 Phase 6：现有登录是 OIDC 与 GitHub OAuth，没有 dev122 身份源要求 SAML | `TestJSONRedactsSecretFields`、`TestAccessLogKeepsLargeBodyIntact`、`TestRealIPTrust`、`TestMachineTokenAllowlistAndScanVisibility`、冒烟检查 no-store |

**Phase 4 评审修正（并入本阶段）**：

- 新扫描报告的指纹是 `HMAC-SHA256(组织指纹密钥, rule ‖ file ‖ line ‖ match)`。组织指纹密钥由 Master Key 和 `taboo/scan-fingerprint/v1`、组织 ID 做 HMAC 得出，不离开服务端。`match` 是只写字段，响应和 `findings_json` 都没有它。已存在的报告无法重算，读取时 `alg=sha256`。
- 导入文件夹改到事务内。版本 reveal 的审计带 `resolved`。`DELETE /orgs/{slug}/scan-reports/{id}` 仅 owner/admin，并写 `scan_report.delete`。
- 机器身份和工作负载默认只能列出、读取自己上传的扫描报告；`list_scan_reports=true` 才可看整个组织。
- `secret.go` 的 500 不再把 `err.Error()` 返回给客户端。

OpenAPI、`apps/web/src/api-types.ts` 与 chi 路由一起更新。schema 17 新增 `rotation_policies`（PostgreSQL 上 ENABLE + FORCE RLS），`machine_identities.allowed_cidrs` / `list_scan_reports`，并把 `dynamic_engines.type` 放宽到 `postgres` 或 `mysql`。

**Phase 5 评审修正（2026-11-01，`d1c0cc0`）**：

- 客户端地址：去掉 chi `middleware.RealIP`（它无条件采信 `True-Client-IP`，NPM 会透传，HTTPS 上可伪造绕过 IP 白名单）。改为 `httpx.RealIP`：只有直连对端在 `TABOO_TRUSTED_PROXIES` 内才取 `X-Real-IP`（其次 `X-Forwarded-For` 最右一跳），忽略 `True-Client-IP`。dev122 compose 默认只信任 NPM `172.18.0.4`。
- MySQL DSN 分支原样保留 `allowAllFiles` 等开关，项目管理员可指向恶意 MySQL 经 `LOAD DATA LOCAL` 读取容器内本地文件（如 `/data` 下的数据）。现两种写法都按白名单重建配置，拒绝 unix socket。
- 访问日志：字符串值默认脱敏，只保留 name/env/role 等元数据字段；修正 `connection_string`、`new_password`、批量 `{KEY: value}`、webhook/sync 配置未脱敏的问题。正文只窥视 64KB，处理器收到的正文不再被 1MB 截断。
- 500 INTERNAL：`apperr` 序列化时统一为 `internal error`，原文只写日志（覆盖其余 60+ 处 `err.Error()`）。

**部署记录（2026-11-01 11:51–11:55 CST）**：`deploy/dev122/deploy.sh` 自动备份 `backups/20261101-115152-pre-upgrade`（40 表），`taboo:dev-967bdbd → taboo:dev-d1c0cc0`，schema 16→17。核对：`schema_version`=17，41 张表 40 张 FORCE RLS，`rotation_policies` 有 `tenant_scope`，`--check` 通过；冒烟 21 项、HTTPS 浏览器检查全部通过。实测（假值）：IP 白名单签发/已有 token 均即时生效，`True-Client-IP`/`X-Forwarded-For` 伪造被拒，非法 CIDR 400；1Password 预览；unix socket DSN 被拒；`docker logs` 中无假值、`client_secret`/`content` 为 `[REDACTED]`。测试数据已清理（含 Phase 4 遗留的 2 份 `live-check` 扫描报告）。升级前备份恢复演练 PASS（4158 行一致，迁移后 schema 17）。dev122 无 MySQL 实例，MySQL 动态凭据只在本机 `mysql:8.4` 测试。

**遗留风险**：NPM 全局 `nginx.conf` 对 10/8、172.16/12、192.168/16 及 CDN 段 `set_real_ip_from` + `real_ip_header X-Real-IP` + `real_ip_recursive on`，局域网客户端自带 `X-Real-IP` 会被 NPM 当成真实地址再转给 taboo（实测从 10.0.30.117 伪造成功）。需 NPM 负责人收窄该配置；在此之前 IP 白名单对局域网客户端不可靠。

---

## Phase 6：Could / 联邦 P1

**状态（2026-11-01）**：完成并部署 dev122（schema 18，`taboo:dev-6beaa0c`）。设计见 [docs/design/phase6.zh-CN.md](design/phase6.zh-CN.md)。

| 子项 | 实现 | 验证 |
|---|---|---|
| 审批 | `environments.approval_required`。新建项目的 prod 默认为开，已有环境保持关。写入、删除、回滚变成 `approval_requests`，明文以 `approval` 用途密封。另一名 owner/admin 批准后才在同一事务里落库。导入直接拒绝。默认 24 小时过期 | `TestPhase6ApprovalAccessAndSCIM` |
| 限时访问 | `access_requests`。动作限于 reveal/export/write/delete/rollback，1 分钟到 4 小时。授权检查读 `expires_at`，到期立即失效；清扫只改状态 | 同上 |
| SCIM | `/api/v1/scim/v2` 的 Users/Groups 和发现端点。令牌 `taboo_scim_` 只存哈希、只显示一次。不按邮箱并入账号，组角色最高到 admin，owner 和手工改过的角色不被覆盖 | 同上 |
| Back-Channel Logout | `POST /api/v1/auth/oidc/{org}/backchannel-logout?pid=`。校验 RS256 logout token，拒绝 nonce，有 sid 只结束该会话，否则按 sub。`logout_replays` 防重放 | `TestPhase6DevicePKCEAndBackChannel` |
| CLI 浏览器登录 | `taboo login --device`（RFC 8628）和 `taboo login --browser`（127.0.0.1 PKCE，路径必须是 `/callback`）。确认页是 `/device` 和 `/cli/login` | 同上，`apps/cli/phase6.test.js` |
| 多副本 | outbox 在认领事务里扇出。`cluster_leases` 让审计检查点和过期清扫同时只有一个副本在跑。登录限流写入 `rate_counters`，库失败返回 503。SQLite 仍是单副本。导出文件要共享卷 | `TestPostgresTwoReplicas`（本机 Docker PostgreSQL，两个连接池） |

**继续延期**

- SAML：登录仍是 OIDC 和 GitHub OAuth，没有身份源要求 SAML。半套 SP 会变成新的认证入口。
- AuthZEN：没有调用方，单独的决策接口会多一条探测途径。
- dev122 上的真实 OIDC 联调：本地测试提供方已覆盖协议。真实 Keycloak 需要运维凭据和变更窗口，不在这次代码里。

OpenAPI、`apps/web/src/api-types.ts` 与 chi 路由一起更新。schema 18 增加审批、限时访问、SCIM、设备码、PKCE、logout replay、限流计数和副本租约。组织范围的新表在 PostgreSQL 上 ENABLE + FORCE RLS；设备码、回放、计数和租约只允许控制角色访问。


**安全复核（2026-11-01，提交 6beaa0c）**，已修：

- 审批竞态：未带 `expectedVersion` 的写入/删除/回滚在排队时钉住当时的 `latest_version`，期间有人改过则批准返回 409 `VERSION_CONFLICT`，不覆盖并发值；删除/回滚不存在的密钥直接 404。
- 密封载荷留存：批准落库、拒绝、撤销、过期后都把 `payload_enc` 清空。
- SCIM：`userName`/邮箱不再写进全局 `users.email`，一律合成 `scim-<sha256>@external.invalid`，防止抢注别人的邮箱；按 IP 加失败计数（20 次/分钟后 429，正确令牌也拒）和请求上限（600 次/分钟），库故障 503。
- 设备码：`/auth/device/token` 限流从 5/分钟放到 30/分钟（原值会让 5 秒轮询的 CLI 被 429 打断），`slow_down` 判定改成原子 UPDATE；开始响应不再给 `verification_uri_complete`，确认页不再从链接预填代码，需手动输入并勾选“代码来自我自己的终端”，附防钓鱼提示。
- 回环登录：只允许 `127.0.0.1` / `::1` 和纯数字端口（去掉 `localhost`）；CLI 回调服务对非 `GET /callback` 请求回 404 而不中止登录；CLI 把 429 当 `slow_down`。
- 新测试：`TestPhase6ApprovalPinsVersionAndDropsPayload`、`TestPhase6SCIMSyntheticEmailAndThrottle`、`TestPhase6DeviceStartHasNoCompleteURI`、`device/loopback_test.go` 新增拒绝项、`apps/cli/phase6.test.js` 回调用例。

复核确认无问题：back-channel logout 按 `provider_id` 限定、会话保存 sid/sub（≤256）、新表 RLS、租约与限流计数、PKCE S256、5xx 不回显内部错误、审批界面不显示载荷。

**部署记录（2026-11-01 12:54 CST）**：`verify-all.sh --container` 14 步全过。`deploy.sh` 自动备份 `backups/20261101-125433-pre-upgrade`，`taboo:dev-d1c0cc0 → taboo:dev-6beaa0c`，schema 17→18；52 张表 51 张 FORCE RLS（`schema_version` 除外），`--check` 通过；已有环境（dev/staging/prod）`approval_required` 全为关。冒烟 21 项、HTTPS 浏览器检查通过。线上实测（临时用户与第二管理员，假值）：写入排队 202 且未落库、自批 403 `SELF_APPROVAL`、列表无明文、他人批准后落库、重复批准 409、并发改动后旧审批 409 `VERSION_CONFLICT`、撤销正常、终态载荷长度 0；JIT：viewer 403 → 申请人不能自批 → 管理员批准后 reveal 200（prod 与写入仍 403）→ 65 秒后回到 403；SCIM：令牌 201、列表不含原文、Users 200、错误/吊销令牌 401；设备码：无 complete URI、9 位代码、间隔 5 秒、pending→slow_down→拒绝后 access_denied，`/device` 页含警告，`localhost` 回调 400。测试用户、组织、审批、访问申请、SCIM 令牌已删除，smoke 账号已禁用；日志中无假值与 SCIM 原始令牌（审计链保留记录）。升级前备份恢复演练 PASS（41 表 4450 行一致，迁移后 schema 18）。

浏览器检查期间日志会出现 4 条 `503 STORAGE_UNAVAILABLE`（审批页的并行 GET）：检查脚本在页面请求未完成时就跳到 `/device`，请求上下文被取消，`store.Scoped` 的 Ping 失败。直接并行调用复现不出，PostgreSQL 无连接错误，属客户端中断，不是服务故障；以后可把已取消请求记成 499 以免误报。

**仍未解决（需决定或后续阶段）**

- 单个 admin 可以 PATCH 关掉 `approval_required` 后直接写 prod，四眼原则可被一人绕过（有审计 `env.approval`）。可选：关闭审批本身也要第二人批准，或仅 owner 可关。
- 新建的单人组织 prod 默认开审批，唯一的 owner 写 prod 前必须先关掉审批。
- 审批是“盲批”：批准人看不到新值，只能看 key、动作、申请人。
- JIT 授权按设计越过 restricted、受保护环境和联邦显式授权检查（因为已有管理员批准）。
- SCIM 用户没有登录关联（合成邮箱，不能用来登录），目前只用于成员与角色同步。
- Back-channel logout 只结束浏览器会话，不吊销 bearer/refresh token。
- 导出文件仍在本地磁盘，多副本需要共享卷。
- NPM 全局 real_ip 问题（见 Phase 5）。

> Phase 7 删除环境模型后，其中“单人组织 prod 默认开审批”已不再适用；其余审批 / JIT 条目需按项目级重新评估。

---

## Phase 7：个人空间/组织模型与控制台重设计（2026-11-10 – 11-12）

**背景**：`/impeccable` 控制台重设计过程中确认原「组织 → 项目 → 环境」模型把同一批密钥拆成三份，也不符合个人自部署的使用场景。决定取消环境、引入个人空间，并按领域重建结构基线。该阶段不在原 Phase 0–6 计划内，直接在 `develop` 上迭代。

**状态（2026-11-12）**：功能代码完成并 push（`d6af762`，与 `origin/develop` 同步，工作区干净）；本机 SQLite 测试、前端类型检查/构建、本地账号浏览器测试通过，CI 的 go-test / web-build / smoke-go 通过。**未达到可发布状态**：CI 5/8 job 失败、dev122 未部署、文档未收口、全量验证未跑。

### 7.1 本阶段完成

| 变更 | 内容 | 主要提交 |
|---|---|---|
| 空间模型 | 注册创建个人空间；组织按需创建；owner/project/folder 成为唯一结构 | `2fe022a`、`a04cb33` |
| 取消环境 | 每个项目一棵密钥树；审批移到项目；`environments` 从 schema 与 API 删除，不迁移旧数据 | `4b95c53`、`1a4c44f`、`f34815c` |
| 定位符 | `owner/project/folder` 寻址；CLI / SDK 跟随（MCP 仍走项目路由） | `10bef76`、`7cae0d6`、`7c15279` |
| 控制台 | 墨黑+朱砂；分组侧栏、密钥行内动作、详情抽屉、审计时间线；首登用户名落款；全屏组织管理页 | `56ad794`、`f3c4f1a`、`838097a`、`d6af762` |
| 结构基线 | PostgreSQL 13 个、SQLite 10 个迁移；goose 启动自动迁移；旧自研迁移器建的库不再兼容 | `2d3f83e`、`e4e3f30`、`d944b3a`、`32df33b` |

设计基线：`DESIGN.md` 记录已建成的世界，`UX-CONTRACT.md` 记录行为契约；finish review 的两项外观遗留已在 `DESIGN.md` 关闭。

### 7.2 当前缺口

| 项 | 现状 |
|---|---|
| CI | 2026-11-12 的 run `43244817423`（`d6af762`）5/8 失败：postgres / consumers / smoke-node / federation / packaging |
| dev122 | 仍是 `taboo:dev-6beaa0c`（2026-11-01，schema 18）；新基线 schema 13/10 与旧库不兼容（旧库没有 `goose_db_version`，脚本 `CREATE TABLE` 非幂等，直接覆盖会启动失败） |
| 文档 | README 已按新模型修正；quickstart 第 16 行、本文件与 `docs/design/*` 仍按三环境描述 |
| 全量验证 | 2026-11-01 后未再执行 `scripts/verify-all.sh --container` |

### 7.3 收尾任务（未执行）

| # | 任务 | 验收 |
|---|---|---|
| T1 | 修 PostgreSQL 下的 locator 404 | `TestLocatorReadsAndWritesASecret`、`TestPersonalSpaceAndCreatedOrganization` 等 PG 用例通过；SQLite 与 PG 都全绿 |
| T2 | 修 consumers | Go / Node / Python / MCP consumer 夹具切到新 locator；`scripts/test-consumers.sh` 通过 |
| T3 | 决定 Node MVP 范围 | 收缩为契约回归，或升级到新模型；`smoke-node` 通过 |
| T4 | 修 federation / packaging | CI 安装 `just` 或脚本不再依赖它；两个 job 通过 |
| T5 | 全量验证 | `verify-all.sh --container` 通过，并把依赖环境模型的步骤更新掉 |
| T6 | 文档收口 | README / quickstart / 本文件 / `docs/design` 与新模型一致 |
| T7 | 发布 dev122 | 用新库启动 → 冒烟 + 浏览器检查 + 恢复演练；旧镜像 `taboo:dev-6beaa0c` 保留可回滚 |

建议顺序：T1–T5 → T7 → T6（T6 可与 T1–T5 并行）。

### 7.4 CI 失败明细（2026-11-12）

| job | 结果 | 原因 |
|---|---|---|
| go-test / web-build / smoke-go | 通过 | SQLite 与前端基线 |
| postgres | 失败 | PG 下 locator 返回 `LOCATOR_NOT_FOUND`；同用例 SQLite 通过 |
| consumers | 失败 | Node consumer 仍用旧 locator，`taboo set` 退化为 usage |
| smoke-node | 失败 | Node MVP 参考后端仍是环境模型，且缺 rotation / scan 端点 |
| federation / packaging | 失败 | runner 没有 `just`，webServer 与 GoReleaser hook 无法启动 |

### 7.5 目录作用域修复（2026-11-13）

`export` / `run` 之前忽略 locator 的文件夹，导出/注入整个项目。现在：

- 默认只导出 locator 指向的目录，`--recursive` 才包含子目录；
- 根目录默认只导出根目录，根目录 + `--recursive` 等价于整个项目；
- 不带 `path` 的 `/projects/{pid}/export` 仍导出整个项目，兼容 SDK；
- 不存在的目录返回 404，不再静默退回整项目；
- `secrets.export` 审计记录目录与 `recursive`。

服务端改动在 `internal/secret/secret.go` 的 `Export`，回归测试 `internal/server/export_test.go`；CLI 只增加 `--recursive` 参数。

### 7.6 Agent skill（2026-11-13）

新增 `skills/taboo/SKILL.md` 与安装说明，覆盖 CLI / MCP 接入、locator、read / reveal / write、`export` / `run` 目录语义，以及“不把明文带进对话、日志、提交”的硬性规则。已 symlink 到 `~/.agents/skills/taboo` 与 `~/.claude/skills/taboo`；DSH 当前会话已能通过 `skill` 加载。

验证：Kimi 按 skill 只列 key 不取值；`run` 注入成功且不打印值；只读身份 reveal 返回 403 后不换身份、不绕路；MCP + skill 只列 `/team/` 的 key。

---

## 总体延期清单（截至 Phase 7）

- SAML、AuthZEN（无调用方/身份源要求）。
- dev122 上真实 OIDC 身份源（Keycloak / Identity V2）联调。
- Woodpecker CI、合并 `main`（待用户决定）。
- Node MVP 的去向（T3）。
- 备份异地拷贝、PG 多副本生产验证。
- 上面 Phase 6 “仍未解决”各项与 NPM real_ip 收窄；其中“单人组织 prod 默认开审批”已随环境模型删除而失效，审批四眼/盲批需要按项目级重新评估。

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
node --test apps/cli/oauth.test.js apps/cli/phase4.test.js apps/cli/phase6.test.js
just build && (TABOO_PORT=7199 TABOO_DATA_DIR=$(mktemp -d) ./apps/server-go/bin/taboo-server &) && TABOO_BASE=http://localhost:7199 npm run smoke
./scripts/test-consumers.sh
TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh
TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh
```

线上：`ssh dev122-root /var/snap/docker/common/swetcc-dev/taboo/taboo-ctl.sh smoke`（专用账号，21 项）+ `scripts/browser-check.mjs`（1280/390）。

> Phase 7 备注：脚本与冒烟用例里仍有按环境模型写的步骤（如成员授权里的“项目 / 环境”），Phase 7 的 T5 一并更新后再作为发布门槛。线上冒烟与浏览器检查目前仍指向 2026-11-01 的旧部署，T7 更新。

## 需用户决定 / 风险

1. ~~名称解析~~：已决定，主机名 `taboo-dev122.swetcc.com`，与其他 `*-dev122` 一样只用 `/etc/hosts`（不加 DNS 记录）；Mac 需用户用 sudo 添加 `10.0.30.122 taboo-dev122.swetcc.com`。
2. ~~dev122 旧部署~~：已决定，全新初始化，旧目录保留不动。
3. ~~Phase 3 提供方~~：已决定，使用 GitHub OAuth App；凭据已写入 dev122 `.env`，2026-11-01 验证通过。
4. Git：已决定每阶段 push 到 `origin/develop`；是否合并 `main`、是否启用 Woodpecker CI 待定。
5. ~~架构差异~~：Phase 1 采用 dev122 原生构建（`deploy/dev122/deploy.sh`）。
6. NPM 全局 real_ip 信任局域网 `X-Real-IP`（见 Phase 5 遗留风险），需 NPM/基础设施负责人决定是否收窄。
7. 审批开关：是否禁止单个 admin 关闭项目级 `approval_required`（四眼绕过）、审批是否继续盲批；原“单人组织 prod 默认开审批”已随环境模型删除而失效，见 Phase 6 / Phase 7。
8. dev122 新基线落库方式：清空/重建现有库，还是换新库名与旧库并存；旧目录 `/home/cloudzh/taboo-dev122/`、旧镜像与升级前备份保留（见 Phase 7 的 T7）。
9. Node MVP 是否继续维护到新模型，或只保留契约回归（见 Phase 7 的 T3）。
10. Phase 7 收尾顺序：先修 CI 再部署，还是先部署再补 CI；建议前者。

## 状态

| 阶段 | 状态 | 日期 |
|---|---|---|
| Phase 0 评估与计划 | 完成 | 2026-10-31 |
| Phase 1 dev122 部署与 HTTPS | 完成（主机名已改为 taboo-dev122.swetcc.com） | 2026-10-31 |
| Phase 2 可重复部署、备份恢复与工程基线 | 完成（Woodpecker 跳过） | 2026-10-31 |
| Phase 3 GitHub OAuth 登录 | 完成（真实账号端到端验证） | 2026-11-01 |
| Phase 4 设计文档 Must 缺口 | 完成并部署（schema 16） | 2026-11-01 |
| Phase 5 Should 能力与安全清单 | 完成并部署（schema 17，`taboo:dev-d1c0cc0`） | 2026-11-01 |
| Phase 6 Could / 联邦 P1 | 完成并部署（schema 18，`taboo:dev-6beaa0c`）；SAML/AuthZEN 延期 | 2026-11-01 |
| Phase 7 个人空间/组织模型 + 控制台重设计 | 代码完成并 push；CI / dev122 / 文档 / 全量验证未收口（schema 13/10） | 2026-11-12 |
