# dev122 运维手册：升级、回滚、备份与恢复

适用于 dev122 上的 taboo 部署（`/var/snap/docker/common/swetcc-dev/taboo`，下称 `$D`）。
所有服务端操作都由 `$D/taboo-ctl.sh` 执行（root，部署时由 `deploy/dev122/deploy.sh` 同步）。
本文不含任何密钥；凭据只在 dev122 上以 0600 文件存放（见末节）。

## 1. 升级（本机一条命令）

```sh
deploy/dev122/deploy.sh                       # 同步源码 → dev122 原生构建 taboo:dev-<sha> → 升级
deploy/dev122/deploy.sh --image taboo:dev-<sha>   # 切到 dev122 上已有的镜像（不构建）
```

升级流程（`taboo-ctl.sh upgrade TAG`）：首次记录 baseline → **升级前备份**（`pre-upgrade`）→ 改写 `.env` 中镜像 →
`init-db.sh`（幂等）→ `compose up -d` → 等待 healthy → 写入 `releases.log`。新镜像 90 秒内不健康则**自动回滚**到原镜像。
构建时注入版本：`taboo-server --version`、`/api/v1/healthz` 的 `version/commit/build_time`、镜像 OCI 标签，前端侧栏与登录页显示 `taboo vX · <commit>`。

## 2. 回滚

```sh
deploy/dev122/deploy.sh --rollback            # 回到 releases.log 中上一个不同的镜像
deploy/dev122/deploy.sh --rollback taboo:dev-<sha>
ssh dev122-root $D/taboo-ctl.sh history       # 发布记录
```

回滚**只切换镜像，不回退数据库**。如果新版本做了不兼容迁移，需要按 §4 用升级前备份恢复数据。

**schema 18（Phase 6，`taboo:dev-6beaa0c` 起，2026-11-01 12:54 CST 部署）**：新增 `approval_requests`、`access_requests`、`scim_tokens`、`scim_users`、`scim_groups`、`scim_group_members`、`device_authorizations`、`cli_auth_codes`、`logout_replays`、`rate_counters`、`cluster_leases`，`environments` 增加 `approval_required`（已有环境保持关，新建项目的 prod 默认开）。旧镜像（schema ≤17）会拒绝 schema 18 的库，单独 `--rollback` 到 Phase 5 镜像会因不健康被自动回退。
确需回到 schema 17（Phase 5）：
1. `taboo-ctl.sh backup manual` 留底（升级后写入的数据，包括审批、访问申请、SCIM 令牌，会丢失）；
2. 把 `$D/.env` 的 `TABOO_IMAGE=` 改为 `taboo:dev-d1c0cc0`（见 `backups/20261101-125433-pre-upgrade/meta.txt`）；
3. `taboo-ctl.sh restore backups/20261101-125433-pre-upgrade --yes`；
4. `taboo-ctl.sh smoke` 确认。
schema 18 后的核对：`schema_version` = 18；52 张 taboo 表有 51 张 `relforcerowsecurity=true`（`schema_version` 除外）；`taboo-server --check` 通过；`SELECT count(*) FROM environments WHERE approval_required` 对已有组织应为 0。
2026-11-01 约 13:00 CST 对该备份做恢复演练：PASS（41 表 4450 行一致，迁移后 schema 18）。

**注册已关闭**：dev122 上 `/auth/register` 返回 403。临时测试用户用 `printf '%s\n' "$PW" | ssh dev122-root "docker exec -i taboo taboo-server user create <email> --name '...'"` 创建（会同时建个人组织与项目），用完按邮箱删除用户及其组织；审批、访问申请、SCIM 令牌按组织一起删。审计日志是哈希链，不删。

**schema 17（Phase 5，`taboo:dev-d1c0cc0` 起，2026-11-01 部署）**：新增 `rotation_policies`（ENABLE + FORCE RLS，`tenant_scope` 策略），`machine_identities` 增加 `allowed_cidrs` 与 `list_scan_reports`，`dynamic_engines.type` 允许 `mysql`。旧镜像（schema ≤16）启动时会拒绝 schema 17 的库，单独 `--rollback` 到 Phase 4 镜像会因不健康被自动回退。
确需回到 schema 16：
1. `taboo-ctl.sh backup manual` 留底（升级后写入的数据会丢失）；
2. 把 `$D/.env` 的 `TABOO_IMAGE=` 改为 `taboo:dev-967bdbd`（见 `backups/20261101-115152-pre-upgrade/meta.txt`）；
3. `taboo-ctl.sh restore backups/20261101-115152-pre-upgrade --yes`。
schema 17 后的核对：`schema_version` = 17；41 张 taboo 表有 40 张 `relforcerowsecurity=true`；`taboo-server --check` 通过（含 `rotation_policies`）。
2026-11-01 11:55 CST 对该备份做恢复演练：PASS（40 表 4158 行一致，迁移后 schema 17）。

**客户端地址 / IP 白名单**：taboo 只采信 `TABOO_TRUSTED_PROXIES` 内直连对端的 `X-Real-IP`（compose 默认 `172.18.0.4`，即 NPM 在 swetcc 网络的地址），忽略 `True-Client-IP`。NPM 重建后地址若变化，需在 `.env` 设置 `TABOO_TRUSTED_PROXIES=<新地址>` 并 `docker compose up -d`；不更新时机器身份白名单会按 NPM 地址判断（拒绝而非放行）。注意 NPM 全局配置对私网与 CDN 段启用了 `real_ip_header X-Real-IP`，局域网客户端可自带该头伪造地址，此问题需在 NPM 侧收窄。

**schema 16（Phase 4，`taboo:dev-967bdbd` 起）**：新增 `scan_reports` 表（ENABLE + FORCE RLS，`tenant_scope` 策略），
迁移只加表不改旧表。旧镜像（schema ≤15）启动时会拒绝 schema 16 的库，所以单独 `--rollback` 到 Phase 3 镜像会因不健康被自动回退。
确需回到 schema 15：
1. `taboo-ctl.sh backup manual` 留底（升级后写入的数据会丢失）；
2. 把 `$D/.env` 的 `TABOO_IMAGE=` 改为 `backups/20261101-104636-pre-upgrade/meta.txt` 中的旧镜像
   （`restore` 用 `.env` 里的镜像启动；若仍是新镜像，恢复后会被再次迁移到 16）；
3. `taboo-ctl.sh restore backups/20261101-104636-pre-upgrade --yes`。
schema 16 后的核对：`schema_version` = 16；`pg_class` 中 40 张 taboo 表有 39 张 `relforcerowsecurity=true`（`schema_version` 本身不设 RLS）；`taboo-server --check` 通过。

## 3. 备份

| 项 | 内容 |
|---|---|
| 位置 | `$D/backups/<YYYYmmdd-HHMMSS>-<label>/`（目录 0700） |
| 内容 | `taboo.dump`（pg_dump -Fc）、`toc.txt`、`data.tar.gz`（容器 `/data`）、`env`（含根密钥，0600）、`counts.tsv`（各表行数）、`meta.txt`（镜像/版本）、`SHA256SUMS` |
| 定时 | `taboo-backup.timer`：每日 03:30 CST（随机延迟 ≤5 分钟，错过开机补跑），label `daily`，保留 7 份 |
| 其他 | `pre-upgrade` / `pre-restore` / 手动备份各保留 5 份 |
| 演练 | `taboo-restore-drill.timer`：每周日 04:15 CST 对最新备份做恢复演练 |

```sh
ssh dev122-root $D/taboo-ctl.sh backup manual     # 手动备份
ssh dev122-root $D/taboo-ctl.sh backups           # 列表
ssh dev122-root systemctl list-timers 'taboo-*'
ssh dev122-root $D/taboo-ctl.sh install-timers    # 重新安装 systemd 定时器
```

备份为**在线备份**（pg_dump 提供一致性快照，不停机）。备份仍在同一台主机上，异地拷贝需另行安排。

## 4. 恢复

```sh
ssh dev122-root $D/taboo-ctl.sh restore-drill [backups/<dir>]   # 演练：不影响线上库
ssh dev122-root $D/taboo-ctl.sh restore backups/<dir> --yes     # 真正恢复（覆盖 taboo 库）
```

- **restore-drill**：校验 SHA256 → 新建临时库 `taboo_restore_test`（owner `taboo_control`）→ pg_restore → 对比各表行数 →
  用备份自带的根密钥运行 `taboo-migrate verify`（解密全部信封）→ 以临时容器 `taboo-restore-drill`（127.0.0.1:7198）启动并检查 readyz →
  记录迁移后的 schema 版本 → 日志写到备份目录 `restore-drill-*.log`，退出时删除临时库和容器。
- **restore**：确认当前根密钥与备份一致 → 先做 `pre-restore` 备份 → 停止 taboo → `pg_restore --clean --if-exists --single-transaction` →
  启动并对比行数。只操作 `taboo` 库，不动共享 postgres 的其他库。

### 演练记录

| 日期 | 备份 | 结果 |
|---|---|---|
| 2026-10-31 14:49 CST | `20261031-144859-pre-upgrade` | PASS：39 张表 154 行一致；`taboo-migrate verify` verified=true、legacy 0；临时实例 readyz OK（d7093f4）；临时库已删除 |
| 2026-11-01 10:49 CST | `20261101-104636-pre-upgrade`（schema 15） | PASS：39 张表 3807 行一致；verify verified=true、legacy 0；临时实例（967bdbd）readyz OK，并把恢复出的库迁移到 schema 16；临时库已删除 |
| 2026-11-01 11:55 CST | `20261101-115152-pre-upgrade`（schema 16） | PASS：40 表 4158 行一致，迁移后 schema 17 |
| 2026-11-01 约 13:00 CST | `20261101-125433-pre-upgrade`（schema 17） | PASS：41 表 4450 行一致，信封校验 OK，迁移后 schema 18 |

## 5. 账号与冒烟

```sh
ssh dev122-root $D/taboo-ctl.sh smoke                       # 21 项 HTTPS 冒烟（含清理）
ssh dev122-root $D/taboo-ctl.sh user status|enable|disable <email>
```

- 冒烟使用专用账号 `smoke@taboo.swetcc.com`，凭据 `$D/smoke-account.json`（0600）。账号平时为 **disabled**，
  每次冒烟启用，结束（含失败）由 EXIT trap 再次禁用；冒烟创建的密钥在最后一步删除。旧镜像无 `user` 子命令时走 SQL 兜底。
- 禁用账号会同时吊销其 refresh token 与浏览器会话，并写审计 `admin.user.disable`。
- 初始 owner 凭据：`$D/bootstrap-owner.json`（0600）。
- 浏览器检查（本机，经 HTTPS）：先 `user enable smoke@taboo.swetcc.com`，然后
  `ssh dev122-root cat $D/smoke-account.json | TABOO_BASE=https://taboo-dev122.swetcc.com TABOO_HOST_MAP='taboo-dev122.swetcc.com 10.0.30.122' node scripts/browser-check.mjs`，最后再 `user disable`。

### Phase 4 接口线上核对

线上手工核对（smoke 账号临时启用，用假值，结束后删除）：
- 引用：`GET .../secrets/{key}?env=dev` 返回原文与 `references`；加 `resolve=true` 才展开；缺失目标 `404 REFERENCE_NOT_FOUND`。
- 导出：`GET /projects/{pid}/export?env=dev&resolve=true`。**任一引用坏掉则整次导出失败**（404 REFERENCE_NOT_FOUND，fail-closed），修好或删掉坏引用后再导出。
- 导入：`POST /projects/{pid}/import`，先 `preview:true`；已有 key 未给 `strategy` 时 409 IMPORT_CONFLICT 且不写库；`skip` / `overwrite`。
- 版本：`GET .../secrets/{key}/versions/{n}`（需 reveal 权限，界面版本对比默认遮罩）。
- 扫描报告：`POST/GET /orgs/{slug}/scan-reports`，只收规则、相对路径、行号、sha256 指纹；带 `match` 字段 400。目前**没有删除接口**，测试报告会留在 smoke 组织里。
- 审计事件：`secrets.reference` / `secrets.reference_denied` / `secrets.import` / `scan_report.create`，均不含明文。

### Phase 6 线上核对

- 审批：`PATCH /projects/{pid}/environments/{env}` 设 `approval_required`；写入返回 202 并排队，`GET /orgs/{slug}/approvals` 只给元数据，`POST .../approvals/{id}/approve|reject|cancel`。自批 403 `SELF_APPROVAL`，期间密钥被改过则 409 `VERSION_CONFLICT`。
- 限时访问：`POST /orgs/{slug}/access-requests`（1 分钟–4 小时），管理员批准后生效，到期立即失效。
- SCIM：`POST /orgs/{slug}/scim/tokens` 只显示一次原文；`/api/v1/scim/v2/Users`。同一 IP 每分钟 20 次错误令牌后 429。
- 设备码：`POST /api/v1/auth/device` → 在 `/device` 手动输入代码并勾选确认。
- 浏览器检查期间日志里审批页 4 条 503 `STORAGE_UNAVAILABLE` 是脚本跳页取消了请求，不是故障。

## 6. GitHub 登录（OAuth App）

默认关闭（`/api/v1/auth/sources` 返回 `github_enabled:false`，`/api/v1/auth/github/login` 为 404）。

1. GitHub → Settings → Developer settings → **OAuth Apps** → New OAuth App（组织所有则在组织设置中创建）：
   - Homepage URL：`https://taboo-dev122.swetcc.com`
   - Authorization callback URL：`https://taboo-dev122.swetcc.com/api/v1/auth/github/callback`
   - 不勾选 Enable Device Flow。生成 Client secret。
2. 在 dev122 上**自己**写入凭据（不经过聊天/工单）：
   ```sh
   ssh dev122-root
   cd /var/snap/docker/common/swetcc-dev/taboo && vi .env   # 文件 0600，目录 0700
   # TABOO_GITHUB_CLIENT_ID=Ov23li...
   # TABOO_GITHUB_CLIENT_SECRET=...
   # TABOO_GITHUB_ALLOWED_USERS=alice,bob       # 至少一种准入策略
   ./taboo-ctl.sh reload                         # 输出 github login: enabled
   ```
3. 准入（至少设一项，否则服务拒绝启动；多项之间为“任一满足即可”）：
   - `TABOO_GITHUB_ALLOWED_USERS`：GitHub 登录名白名单。
   - `TABOO_GITHUB_ALLOWED_ORGS`：组织的 active 成员（会额外申请 `read:org`；若组织启用了 OAuth App access restrictions，需组织 owner 批准该应用，否则一律 not_allowed）。
   - `TABOO_GITHUB_ALLOWED_TEAMS`：`org/team-slug` 的 active 成员。
   - `TABOO_GITHUB_ALLOW_ANY=true`：任意 GitHub 账号（不建议）。
4. 账号对应：
   - 默认 `TABOO_GITHUB_AUTOCREATE=false`：用户先用密码登录，顶栏「GitHub」→ 绑定；之后可用 GitHub 登录。按 GitHub 数字 ID 绑定，不按邮箱合并。
   - `TABOO_GITHUB_AUTOCREATE=true`：通过准入且未绑定的账号自动创建（无密码、个人组织），主邮箱已被占用时拒绝（email_in_use）；
     `TABOO_GITHUB_JOIN_ORG=<slug>` + `TABOO_GITHUB_JOIN_ROLE=viewer|developer` 可同时加入共享组织。
5. 撤权：移出白名单/组织/团队后下一次 GitHub 登录即被拒绝；已签发的 Taboo 会话需 `./taboo-ctl.sh user disable <email>` 立即吊销
   （refresh token 与浏览器会话一并作废，access token 立即失效）。解绑 GitHub 也会吊销该用户全部 refresh token。
   GitHub access token 只在回调中使用一次，随即调用 GitHub API 吊销，不落库。
6. 审计：`auth.github.login` / `auth.github.provision` / `auth.github.link` / `auth.github.unlink` / `auth.github.denied`（meta.reason）。
7. 关闭：注释掉两行凭据 → `./taboo-ctl.sh reload`。
