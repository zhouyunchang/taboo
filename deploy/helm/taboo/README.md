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
- **健康检查**：liveness 用 `--check`（加密往返 + schema 版本 + 关键表存在性），readiness 用 HTTP `/`。
