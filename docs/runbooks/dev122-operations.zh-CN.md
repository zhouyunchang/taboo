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
  日志写到备份目录 `restore-drill-*.log`，退出时删除临时库和容器。
- **restore**：确认当前根密钥与备份一致 → 先做 `pre-restore` 备份 → 停止 taboo → `pg_restore --clean --if-exists --single-transaction` →
  启动并对比行数。只操作 `taboo` 库，不动共享 postgres 的其他库。

### 演练记录

| 日期 | 备份 | 结果 |
|---|---|---|
| 2026-10-31 14:49 CST | `20261031-144859-pre-upgrade` | PASS：39 张表 154 行一致；`taboo-migrate verify` verified=true、legacy 0；临时实例 readyz OK（d7093f4）；临时库已删除 |

## 5. 账号与冒烟

```sh
ssh dev122-root $D/taboo-ctl.sh smoke                       # 15 项 HTTPS 冒烟（含清理）
ssh dev122-root $D/taboo-ctl.sh user status|enable|disable <email>
```

- 冒烟使用专用账号 `smoke@taboo.swetcc.com`，凭据 `$D/smoke-account.json`（0600）。账号平时为 **disabled**，
  每次冒烟启用，结束（含失败）由 EXIT trap 再次禁用；冒烟创建的密钥在最后一步删除。旧镜像无 `user` 子命令时走 SQL 兜底。
- 禁用账号会同时吊销其 refresh token 与浏览器会话，并写审计 `admin.user.disable`。
- 初始 owner 凭据：`$D/bootstrap-owner.json`（0600）。
- 浏览器检查（本机，经 HTTPS）：先 `user enable smoke@taboo.swetcc.com`，然后
  `ssh dev122-root cat $D/smoke-account.json | TABOO_BASE=https://taboo.swetcc.com TABOO_HOST_MAP='taboo.swetcc.com 10.0.30.122' node scripts/browser-check.mjs`，最后再 `user disable`。
