# taboo（禁制）

> 传说中有大价值的地方，都有禁制保护。taboo 就是保护你密钥的那道禁制。

开源自部署密钥管理平台 —— Infisical / HashiCorp Vault 的轻量平替。**密钥永不落盘明文、永不进 Git 仓库、全操作可审计。** 完全 MIT，无 open-core 付费墙。

详细设计见 [doc/OpenVault_密钥管理平台_功能与详细设计.md](doc/OpenVault_密钥管理平台_功能与详细设计.md)（本仓库架构的唯一事实源，代号 OpenVault，产品名 taboo）。

## 为什么叫 taboo（禁制）

修仙小说里，藏有重宝的洞府必有禁制：外人不可见、不可触，擅动者必被察觉。密钥管理的本质一样——**高价值数据 + 严格访问控制 + 全程留痕**。

## 当前状态：MVP（Node.js 验证版）

> 本 MVP 用 Node.js 标准库（`node:http` / `node:sqlite` / `node:crypto`）零外部依赖快速验证设计；
> 目标架构为 **Go 后端 + React 前端**（见设计文档 §3），加密契约、API 契约、数据模型保持一致，可平滑迁移。

已实现（对齐设计文档 Must Have 子集）：

- [x] 信封加密三层结构：Root Key (KEK) → 组织 DEK → Secret Value（AES-256-GCM，nonce 12B 随机不复用）
- [x] 注册即建个人组织 + 默认项目 + dev/staging/prod 三环境；组织/项目/环境模型
- [x] 密钥 CRUD + 版本历史 + 回滚（`secret_versions` 只追加）
- [x] 用户认证：scrypt 密码哈希（Go 版迁移 Argon2id）、JWT 15min + refresh 旋转
- [x] RBAC-lite：owner / admin / developer / viewer；**reveal（取明文）与 read（看元数据）分离**；developer 禁写 prod
- [x] 审计日志：append-only，谁何时对哪个密钥做了什么（读/写/删/回滚/导出）
- [x] REST API：`/api/v1/auth/*`、`/orgs/*`、`/projects/*/secrets`、`export`、`/audit`
- [x] Web Dashboard：登录注册、项目/环境切换、密钥表（掩码/显示/复制 20s 自清除）、版本抽屉+回滚、审计筛选
- [x] CLI：`login / set / get / list / export / run`（`run` 对齐设计文档 §7.1 注入流程）
- [ ] 机器身份（Machine Identity）、动态密钥、MCP Server、Secret Sync、TOTP 2FA → 见路线图

## 快速开始

要求：Node.js ≥ 22.5（使用内置 `node:sqlite`，无需任何外部服务）

```bash
# 1. 启动 API + Web（同一端口，前端已构建时自动托管 web/dist）
cd server && npm start          # http://localhost:7100

# 2. 前端开发模式（Vite，/api 自动代理到 7100）
cd web && npm install && npm run dev

# 3. CLI
node cli/taboo.js login you@example.com your-password
node cli/taboo.js set DB_PASS s3cret --env dev
node cli/taboo.js run -- npm run dev
```

冒烟测试（14 项端到端检查）：

```bash
cd server && npm start &        # 先起服务
npm run smoke
```

## 配置

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `TABOO_PORT` | `7100` | 服务端口 |
| `TABOO_DATA_DIR` | `server/data` | SQLite + master key 目录 |
| `TABOO_MASTER_KEY` | 自动生成文件 | 32B hex/base64；不设置则生成 `data/master.key`（0600） |
| `TABOO_JWT_SECRET` | 随机（重启失效） | 生产必须显式设置 |
| `TABOO_CORS_ORIGIN` | `*` | 开发跨域来源 |

## 目录结构

```
taboo/
├── doc/            # 设计文档（OpenVault 功能与详细设计）
├── server/
│   ├── src/
│   │   ├── index.js    # HTTP 入口：API + 静态托管 + 安全头/CORS
│   │   ├── api.js      # REST 路由
│   │   ├── auth.js     # JWT 中间件 + RBAC 求值 + 审计写入
│   │   ├── crypto.js   # 信封加密、scrypt、JWT 原语
│   │   └── db.js       # SQLite schema（DDL 以 PG 语义设计）
│   └── scripts/smoke.js
├── web/            # React 19 + TS + Vite Dashboard
├── cli/taboo.js    # MVP CLI
└── README.md
```

## 路线图（对齐设计文档 §10）

| 阶段 | 内容 |
|---|---|
| **M0（当前）** | Node.js MVP：验证加密架构、API 契约、RBAC、审计 |
| M1 | Go 重写（Chi + sqlc + modernc.org/sqlite/PG）、Argon2id、embed.FS 单二进制 |
| M2 | 机器身份（client_credentials + scope）、TOTP 2FA、文件夹多级路径 |
| M3 | CLI 全命令（scan 泄漏扫描）、OpenAPI 契约、SDK |
| M4 | 动态密钥（PG/MySQL lease）、MCP Server（AI Agent 只读访问） |
| M5 | Secret Sync、Webhooks、OIDC SSO、审计导出、Docker/Helm 发布 |

## 许可

MIT（v1.0 正式确定；当前代码以 MIT 精神完全开放，无付费墙）
