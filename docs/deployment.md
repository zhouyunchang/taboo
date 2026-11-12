# 部署指南

## 单机 Docker（SQLite 内嵌）

见 [quickstart.md](quickstart.md)。数据卷 `/data` 内含 `taboo.db` 与 `master.key`，
**备份 `master.key` 与 `taboo.db` 同等重要** —— Master Key 丢失意味着所有密文不可解。

生产注意：

- 显式设置 `TABOO_JWT_SECRET`（否则重启后会话全部失效）
- 前置 HTTPS 反向代理（Caddy/nginx），见设计文档 §8「传输：强制 TLS」
- 限制 `/data` 目录权限（容器内以 uid 10001 运行）

## 进程角色（PostgreSQL / 多副本）

`TABOO_ROLE` 控制一个进程做什么：

| 值 | HTTP / Web UI | 后台循环 | 适用 |
|---|---|---|---|
| `all`（默认） | 有 | 有 | 单二进制 / SQLite；也兼容单实例 PG |
| `serve` | 有 | 无轮换/投递/同步/动态回收/审计检查点 | PG 多副本 API |
| `worker` | 可选健康端口 | rotation + webhook + outbox + sync + dynamic + audit | PG 后台进程 |

- SQLite 只支持 `all`；`serve`/`worker` 会拒绝启动。
- `worker` 与 API 共用同一个 `TABOO_DATABASE_URL`、`TABOO_MASTER_KEY`、`TABOO_JWT_SECRET`。
- worker 不暴露业务 API；需要探针时设置 `TABOO_WORKER_ADDR=:7101`，提供 `/healthz`、`/readyz`。
- 轮换调度每 15 秒扫一次；心跳写入 `worker_heartbeats`，API 的 `/api/v1/orgs/{slug}/rotation/health` 读取它。
- 迁移仍由第一个启动的新实例执行；如果要独立迁移 Job，目前需先新增 `schema up` 命令（现有 `schema` 只读）。

```bash
# API
TABOO_ROLE=serve ./taboo-server
# Worker
TABOO_ROLE=worker TABOO_WORKER_ADDR=:7101 ./taboo-server
```

## Keycloak（外部用户源）

1. Keycloak 创建 Confidential Client，Standard flow；Valid redirect URI：
   `https://taboo.example.com/api/v1/auth/oidc/<org-slug>/callback`
2. Client scopes 把 Group Membership 写入 ID token（claim `groups`），或映射 realm roles
3. 用本地 owner 登录 taboo → 组织设置 →「接入 Keycloak / OIDC」，Issuer 形如
   `https://keycloak.example.com/realms/company`，勾选「显示在登录页」
4. 可选：关掉本地注册/密码，只走 Keycloak

```bash
-e TABOO_OIDC_ISSUER=https://keycloak.example.com/realms/company \
-e TABOO_OIDC_CLIENT_ID=taboo \
-e TABOO_OIDC_CLIENT_SECRET=... \
-e TABOO_OIDC_ORG=your-org-slug \
-e TABOO_DISABLE_REGISTER=1
```

## Kubernetes（Helm）

```bash
helm install taboo ./deploy/helm/taboo -n taboo \
  --set existingSecret=taboo-keys \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=taboo.example.com
```

- Master Key / JWT Secret 经 K8s Secret 注入（`existingSecret` 必填，values 明文会被 `fail` 拦截）
- 默认单副本。SQLite 不能水平扩展。PostgreSQL 可以把 `replicaCount` 调到 1 以上，前提是每个副本使用同一个 `TABOO_MASTER_KEY`、`TABOO_JWT_SECRET` 和 `TABOO_DATABASE_URL`，并且导出目录是共享卷。outbox、轮换、动态凭据和 webhook 用行租约；审计检查点和过期清扫用 `cluster_leases`。登录限流记在数据库里，数据库不可用时登录返回 503。发现文档和 DEK 缓存可以各进程一份
- 数据卷 PVC 存 SQLite 与导出文件；liveness = `/api/v1/healthz`，readiness = `/api/v1/readyz`
- 滚动更新：`terminationGracePeriodSeconds: 30`，`preStop` sleep 3s，进程 graceful drain 15s
- 详细说明见 [deploy/helm/taboo/README.md](../deploy/helm/taboo/README.md)

## 发布产物（GoReleaser）

```bash
git tag v1.0.0 && git push origin v1.0.0   # 触发 release 工作流
```

每个 release 包含：多平台二进制（linux/darwin × amd64/arm64）、容器镜像
（`ghcr.io/zhouyunchang/taboo`）、SBOM（syft）、cosign 签名。验证镜像：

```bash
cosign verify ghcr.io/zhouyunchang/taboo:v1.0.0
```

## 备份与恢复

工具化备份（进程内 SQLite `VACUUM INTO` + 复制 `master.key` 0600）：

```bash
taboo-server backup /backup/taboo-$(date +%Y%m%d)
```

K8s CronJob 示例：对 PVC 挂载的 `/data` 执行同一命令，产物落到备份卷。恢复仍是停机替换 `$TABOO_DATA_DIR`。

1. 停机（或至少确保无写入）
2. 备份整个 `$TABOO_DATA_DIR`（`taboo.db` + `master.key`）
3. 恢复 = 放回数据目录 + 相同 `TABOO_MASTER_KEY` 启动
