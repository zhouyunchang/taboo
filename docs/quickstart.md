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

```bash
npm ci
npm run dev            # 前端 vite (5173)
cd apps/server-go && make web && make run   # 后端 (:7100，embed 前端)

make smoke             # 14 项契约冒烟检查
go test ./...          # 契约漂移检查（路由 vs openapi.yaml）
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
