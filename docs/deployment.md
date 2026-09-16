# 部署指南

## 单机 Docker（SQLite 内嵌）

见 [quickstart.md](quickstart.md)。数据卷 `/data` 内含 `taboo.db` 与 `master.key`，
**备份 `master.key` 与 `taboo.db` 同等重要** —— Master Key 丢失意味着所有密文不可解。

生产注意：

- 显式设置 `TABOO_JWT_SECRET`（否则重启后会话全部失效）
- 前置 HTTPS 反向代理（Caddy/nginx），见设计文档 §8「传输：强制 TLS」
- 限制 `/data` 目录权限（容器内以 uid 10001 运行）

## Kubernetes（Helm）

```bash
helm install taboo ./deploy/helm/taboo -n taboo \
  --set existingSecret=taboo-keys \
  --set ingress.enabled=true \
  --set ingress.hosts[0].host=taboo.example.com
```

- Master Key / JWT Secret 经 K8s Secret 注入（`existingSecret` 必填，values 明文会被 `fail` 拦截）
- 默认单副本：SQLite WAL 单写多读，事件 worker（Sync/Webhook/动态密钥/审计导出）为进程内轮询
- 数据卷 PVC 存 SQLite 与导出文件；liveness = `--check`，readiness = HTTP `/`
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

1. 停机（或至少确保无写入）
2. 备份整个 `$TABOO_DATA_DIR`（`taboo.db` + `master.key`）
3. 恢复 = 放回数据目录 + 相同 `TABOO_MASTER_KEY` 启动
