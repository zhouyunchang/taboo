# taboo Helm Chart

开源自部署密钥管理平台的 Kubernetes 部署。

## 快速开始

```bash
# 1. 准备密钥（Master Key 丢失 = 全部密文不可解，务必离线备份）
kubectl create namespace taboo
kubectl -n taboo create secret generic taboo-keys \
  --from-literal=TABOO_MASTER_KEY=$(openssl rand -hex 32) \
  --from-literal=TABOO_JWT_SECRET=$(openssl rand -hex 32)

# 2. 安装
helm install taboo ./deploy/helm/taboo -n taboo \
  --set existingSecret=taboo-keys \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=taboo.example.com

# 3. 验证（启动自检：加密往返 + 迁移状态）
kubectl -n taboo exec deploy/taboo-taboo -- /usr/local/bin/taboo-server --check
```

## 设计说明

- **单副本**：SQLite 内嵌模式（WAL，单写多读）。事件 worker（Sync / Webhook / 动态密钥回收 / 审计导出）均为进程内轮询，不支持水平扩展；需要 HA 时请等待 PG 主模式（issue #1）。
- **密钥注入**：`TABOO_MASTER_KEY` / `TABOO_JWT_SECRET` 只经 K8s Secret 注入，禁止写入 values 或 git。
- **数据卷**：`/data` 含 `taboo.db`、`master.key`（如使用文件模式）与审计导出文件，请纳入备份策略。
- **健康检查**：liveness = `GET /api/v1/healthz`，readiness = `GET /api/v1/readyz`。`--check` 仅用于启动/CI。

## PostgreSQL 与标准身份联邦

`database.mode=postgres` 时设置 `database.existingSecret`，其中仅需 `TABOO_DATABASE_URL`；`database.runtimeRole` 默认 `taboo_runtime`，角色由 DBA 预建。主密钥/JWT 仍由顶层 `existingSecret` 注入，各副本保持相同。应用不使用超级用户 DSN。

`federation.publicOrigin` 是浏览器外部精确 origin；`federation.issuerAllowlist` 为完整 issuer 数组。`federation.bootstrapSecret` 可选，使用 envFrom 注入 `TABOO_OIDC_ORG/ISSUER/CLIENT_ID/CLIENT_SECRET`；不会自动创建组织或覆盖既有连接。`disableRegister/disablePassword` 默认 false，验证外部 owner 和恢复路径后再开启。

SQLite 强制单副本/Recreate。PG 多副本还需 `persistence.enabled=true`、`persistence.accessMode=ReadWriteMany`，同一共享卷保存跨实例审计导出；未满足条件时模板报错。迁移前停写/停 worker，禁止不同 schema 版本同时运行。没有执行生产 Kubernetes 发布。

本地模板校验：`./scripts/test-helm.sh`。恢复和回退见 [运行手册](../../../docs/runbooks/standard-federation-release.zh-CN.md)。
