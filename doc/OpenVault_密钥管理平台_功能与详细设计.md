# OpenVault（代号）— 开源自部署密钥管理平台 · 功能与详细设计

> 定位：Infisical / HashiCorp Vault 的开源平替。**单一许可证完全开源（MIT/Apache 2.0）**，无 open-core 付费墙，Go 后端 + TypeScript/React 前端，可私有化部署。
> 核心原则：密钥永不落盘明文、永不进 Git 仓库、全操作可审计。

---

## 1. 产品定位与差异化

| 维度 | Infisical | Vault/OpenBao | **本项目** |
|---|---|---|---|
| 许可证 | open-core（RBAC 等付费） | BUSL / MPL | **完全 MIT** |
| 部署依赖 | PostgreSQL + Redis | Raft/存储后端 | **PostgreSQL 单依赖**（可选 SQLite 嵌入式模式） |
| 核心卖点 | 开发者体验 | 动态密钥、生态 | 轻量 + Agent 友好（CLI 注入 + MCP） |
| 目标用户 | 中小团队 | 大企业 | 中小团队、个人、AI Agent 场景 |

**聚焦场景（避免做成 Vault 那样的大而全）：**
1. 应用配置密钥的集中管理（dev/staging/prod 环境隔离）
2. 本地开发与 CI/CD 的密钥注入（`ovr run -- npm run dev`）
3. AI 编程 Agent 的受控密钥访问（机器身份 + 只读授权 + 审计）
4. 密钥泄漏防护（提交前扫描）

**明确不做（v1 范围外）**：PKI/证书管理、SSH CA、PAM、加密即服务（Transit）、HSM 集成。架构上预留插件点，后续迭代。

---

## 2. 功能清单（MoSCoW 优先级）

### Must Have（v1.0 核心）

- **组织与项目模型**：Organization → Project → Environment（dev/staging/prod 可自定义）→ Folder（多级路径）
- **密钥 CRUD**：键值对 + 备注 + 标签；版本历史（每次修改保留旧值，可回滚）
- **密钥引用**：`${project:env:KEY}` 跨环境引用，运行时解析
- **用户与认证**：邮箱密码注册登录、TOTP 双因素、Session/JWT
- **RBAC 权限**：预置角色 Owner / Admin / Developer / Viewer，按 项目/环境 粒度授权
- **机器身份（Machine Identity）**：为 CI/CD、Agent 颁发 `client_id + client_secret` 或短期 Token，作用域限定到项目+环境+读写
- **CLI**：`login` / `run`（环境变量注入）/ `get` / `set` / `export` / `scan`（Git 提交前密钥泄漏扫描，140+ 正则规则）
- **Web Dashboard**：密钥管理、成员管理、审计日志、机器身份管理
- **审计日志**：谁在何时对哪个密钥做了什么（读/写/删/回滚），不可篡改（追加写）
- **SDK**：Go、Python、Node.js
- **部署**：Docker 单容器 all-in-one（内嵌 SQLite）、Docker Compose（PG）、Helm Chart

### Should Have（v1.1 ~ v1.5）

- **动态密钥**：按需生成 PostgreSQL/MySQL 短期账号（到点自动回收）
- **Secret Sync**：推送到 GitHub Actions Secrets、Vercel、Cloudflare Workers
- **Webhooks**：密钥变更事件通知
- **SSO**：OIDC / SAML 登录
- **密钥轮换策略**：定时触发外部轮换器
- **MCP Server**：供 AI Agent 以标准协议查询密钥（只读、细粒度 scope）
- **导入/导出**：从 .env、Doppler、1Password CLI 批量导入

### Could Have（v2+）

- 密钥审批流（修改生产环境需二次确认）
- 访问请求（Access Request）：临时申请某环境权限，到期自动回收
- 密钥扫描的 Git 历史深度扫描 + pre-commit hook
- 多副本高可用、读写分离

---

## 3. 系统架构

### 3.1 总体架构

```
┌─────────────┐  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐
│  Web UI     │  │  CLI (Go)   │  │  SDK (3语言) │  │ MCP Server  │
│ React + TS  │  │             │  │              │  │ (AI Agent)  │
└──────┬──────┘  └──────┬──────┘  └──────┬───────┘  └──────┬──────┘
       └────────────────┴──── HTTPS ─────┴─────────────────┘
                          │
              ┌───────────▼────────────┐
              │   API Server (Go)      │
              │   ┌─────────────────┐  │
              │   │ AuthN/AuthZ     │  │  ← JWT + RBAC 中间件
              │   │ Secret Service  │  │  ← 加解密、版本、引用解析
              │   │ Identity Service│  │  ← 用户/机器身份
              │   │ Audit Service   │  │  ← 异步落审计
              │   │ Sync Service    │  │  ← v1.1+ 外部推送
              │   └─────────────────┘  │
              └───────┬────────┬───────┘
                      │        │
              ┌───────▼──┐  ┌──▼─────────┐
              │PostgreSQL│  │KMS 适配层   │ ← Master Key 来源：
              │(密文存储) │  │            │   本地文件 / 环境变量 /
              └──────────┘  └────────────┘   AWS KMS / GCP KMS
```

**关键设计决策：**

1. **单体优先（Modular Monolith）**：单个 Go 二进制 + 前端静态文件内嵌（`embed.FS`），`docker run` 一条命令起服务。内部按模块划分边界，未来可拆。
2. **单数据库依赖**：PostgreSQL 为主（生产）；SQLite 模式供个人/试用（用 modernc.org/sqlite 纯 Go 驱动，免 CGO，便于交叉编译）。
3. **无 Redis**：缓存用进程内 LRU（ristretto），会话存 PG，降低部署门槛。需要分布式部署时再以插件形式引入。
4. **技术栈选型**：
   - 后端：Go 1.23+ / **Chi** 路由（标准库风格，轻）/ **sqlc**（类型安全 SQL，避免 ORM 黑盒）/ **Zap** 日志 / **Wire** 依赖注入（可选）
   - 前端：React 19 + TypeScript / Vite / **TanStack Query**（服务端状态）/ Tailwind CSS + shadcn/ui / React Router
   - CLI：Go + **Cobra**，单二进制分发（go install / brew / scoop）

### 3.2 加密架构（核心，必须设计正确）

采用 **信封加密（Envelope Encryption）** 三层结构：

```
Root Key (KEK, 主密钥)          ← 部署方持有：文件/环境变量/KMS
   │  用于加解密 ↓
Org Data Key (每组织一把 DEK)   ← 加密后存 DB，启动时按 org 惰性解密入内存
   │  用于加解密 ↓
Secret Value (AES-256-GCM)      ← DB 中只存密文 + nonce + 版本
```

- **算法**：AES-256-GCM（认证加密，防篡改）；nonce 每次加密随机生成 12 字节，绝不复用。
- **密钥派生**：用户密码用 Argon2id（内存 64MB、迭代 3、并行 4）哈希存储，只用于认证，不参与密钥加密。
- **Master Key 管理**：
  - 简单模式：32 字节随机 key 存文件（0600 权限）或环境变量，首次启动自动生成
  - 生产模式：对接 AWS KMS / GCP KMS / 阿里云 KMS（通过统一 `KMSProvider` 接口）
  - 支持 **Shamir 分片 unseal**（可选，对标 Vault）：Root Key 拆 5 份，任 3 份可恢复，重启需管理员手动 unseal
- **明文在内存中的生命周期**：仅在请求处理期间存在；响应后立即清零（`runtime.KeepAlive` + 显式 `memzero`）；禁止日志打印。
- **CLI 端**：`run` 命令在本地进程内解密（服务端返回密文 + 该身份可解的 DEK 副本），密钥注入子进程环境变量后父进程立即清除自身内存。

### 3.3 目录结构（Go 后端）

```
openvault/
├── cmd/
│   ├── server/main.go        # API Server 入口
│   ├── cli/                  # Cobra 命令树
│   └── mcp/main.go           # MCP Server（v1.1）
├── internal/
│   ├── config/               # Viper 配置加载
│   ├── server/               # HTTP 路由、中间件
│   ├── auth/                 # 登录、JWT、TOTP、Session
│   ├── identity/             # 机器身份
│   ├── rbac/                 # 权限检查（策略求值器）
│   ├── secret/               # 密钥服务：CRUD、版本、引用解析
│   ├── crypto/               # 信封加密、KMS 适配、Shamir
│   ├── audit/                # 审计追加写、查询
│   ├── org/                  # 组织/项目/环境/文件夹
│   ├── sync/                 # 外部平台同步（v1.1）
│   ├── store/                # sqlc 生成代码 + migrations
│   └── apperr/               # 统一错误模型
├── api/openapi.yaml          # OpenAPI 3.1 契约（前后端唯一事实源）
├── web/                      # React 前端（构建产物 embed 进二进制）
├── migrations/               # goose 迁移脚本
├── deploy/                   # Dockerfile、compose、helm
└── Makefile
```

---

## 4. 数据模型（PostgreSQL 核心表）

```sql
organizations     (id, name, slug, dek_encrypted, created_at)
users             (id, email, password_hash, totp_secret, status, created_at)
org_members       (org_id, user_id, role, joined_at)

projects          (id, org_id, name, slug, created_at)
environments      (id, project_id, name, slug, sort_order)   -- dev/staging/prod
folders           (id, env_id, parent_id, path)              -- 物化路径 /a/b/

secrets           (id, env_id, folder_id, key, type, comment, tags[], 
                   latest_version_id, created_at, updated_at,
                   UNIQUE(env_id, folder_id, key))
secret_versions   (id, secret_id, version, ciphertext, nonce, 
                   created_by, created_at)                   -- 只追加不修改

machine_identities(id, org_id, name, auth_type, client_id, secret_hash, 
                   token_ttl, created_at)
identity_scopes   (identity_id, project_id, env_id, permission) -- read/write/admin

policies          (id, org_id, subject_type, subject_id, resource, action, effect)
audit_logs        (id, org_id, actor_type, actor_id, action, resource, 
                   metadata jsonb, ip, user_agent, created_at)  -- 分区表按月
api_tokens        (id, owner_type, owner_id, token_hash, expires_at, last_used_at)
```

要点：
- 密钥值只存在于 `secret_versions.ciphertext`，读即解密返回；列表接口默认**不返回值**（仅元数据），取值走单独接口并记审计。
- `audit_logs` 只 INSERT 不 UPDATE/DELETE（应用层强制 + DB 权限回收），按月分区便于归档。
- 所有查询带 `org_id` 租户隔离；敏感字段加密列不支持检索（key 可检索、value 不可）。

---

## 5. API 设计（REST，节选）

```
POST   /api/v1/auth/register | /login | /refresh | /totp/verify
GET    /api/v1/orgs/:slug/projects
POST   /api/v1/projects/:id/environments

# 密钥核心
GET    /api/v1/projects/:pid/secrets?env=dev&path=/db        # 列表（无值）
GET    /api/v1/projects/:pid/secrets/:key?env=dev            # 取单值（记审计）
POST   /api/v1/projects/:pid/secrets                         # 创建/更新（产生新版本）
GET    /api/v1/projects/:pid/secrets/:key/versions           # 版本历史
POST   /api/v1/projects/:pid/secrets/:key/rollback           # 回滚到 vN
POST   /api/v1/projects/:pid/secrets/export?env=dev          # 导出 .env（记审计）

# 机器身份（Agent/CI 用）
POST   /api/v1/identities                                    # 创建，返回一次性 secret
POST   /api/v1/identities/token                              # client_credentials 换短期 JWT
GET    /api/v1/audit?resource=secret:DB_PASS                 # 审计查询

# v1.1
POST   /api/v1/dynamic-secrets/postgres/:id/lease            # 申请动态账号
```

统一约定：JSON、错误格式 `{code, message, details}`、cursor 分页、所有写操作幂等（客户端生成 Idempotency-Key）。

---

## 6. RBAC 权限模型

策略四元组：**(主体, 资源, 动作, 环境约束)**

```
主体：user:alice / identity:ci-runner / role:developer
资源：org/acme/project/billing/env/prod/secrets/*
动作：secrets.read / secrets.write / secrets.reveal / secrets.rollback
约束：env IN (dev, staging)
```

- 预置角色：Viewer（读元数据）/ Developer（读值+写 dev）/ Admin（全环境写）/ Owner（组织管理）
- 策略求值用内存中编译的 AST，P99 < 1ms；DB 只做策略存储。
- **reveal（取明文）与 read（看元数据）分离**——这是密钥系统区别于普通 RBAC 的关键设计：列表可见 ≠ 能看值。
- 机器身份默认最小权限：创建时必须显式指定 scope，不支持通配 `*`。

---

## 7. 关键流程

### 7.1 CLI 注入流程（`ovr run -- npm run dev`）

```
1. CLI 读取 ~/.openvault/session 或 OVR_TOKEN 环境变量
2. POST /identities/token 换取短期 JWT（TTL 15min）
3. GET /secrets/export?env=dev → 服务端校验 scope，记审计，
   返回该身份可解的密文包 + 加密的 DEK
4. CLI 本地解密（内存中），fork 子进程 npm run dev 并注入 env
5. 父进程退出前 memzero；子进程环境变量随进程结束消亡
```

### 7.2 动态密钥流程（v1.1）

```
Agent 申请 lease → 服务端连 PG 执行 CREATE USER (TTL 1h)
→ 返回临时账号密码 + lease_id → 到期后台 worker 自动 DROP USER
→ 全程审计；Agent 续约可延长 lease
```

### 7.3 AI Agent 集成（MCP Server，v1.1）

- 以独立二进制提供 `secrets.list` / `secrets.get` 两个 MCP Tool
- 配置示例：Claude Code 的 `.mcp.json` 指向本地 mcp 进程，Agent 拿到的是经 scope 限制的只读访问
- Agent 的每次取值都带 `actor=identity:xxx` 落审计，可随时吊销 identity 切断访问

---

## 8. 安全设计清单

- [ ] 传输：强制 TLS（Caddy 反代或内置 Let's Encrypt）
- [ ] 认证：Argon2id 密码哈希、登录限流（5 次/分钟/IP）、TOTP 2FA
- [ ] Token：JWT 短 TTL（15min）+ refresh token 旋转；机器 token 绑定 IP 白名单（可选）
- [ ] 加密：AES-256-GCM + 信封加密；nonce 唯一性由随机性保证（96bit，生日界内安全）
- [ ] 防泄漏：响应头禁止缓存；日志中间件自动脱敏（`password|secret|token` 字段置 `[REDACTED]`）；`run` 输出不 echo 环境变量
- [ ] 审计：append-only、含 actor/IP/UA、支持导出
- [ ] 供应链：GoReleaser 签名发布、SBOM、容器镜像 cosign 签名
- [ ] 自检：启动时 `--check` 验证加密往返、DB 迁移状态

---

## 9. 前端页面清单（React + TS）

| 页面 | 说明 |
|---|---|
| 登录/注册/2FA | 邮箱密码 + TOTP |
| 组织/项目总览 | 卡片式项目列表 |
| **密钥工作台**（核心页） | 左：环境 Tab + 文件夹树；中：密钥表格（key/值掩码/标签/更新时间）；右：详情抽屉（值、版本历史、回滚、审计片段） |
| 版本对比 | 两版本 diff 视图 |
| 成员与角色 | 邀请、角色分配、环境级权限矩阵 |
| 机器身份 | 创建向导（scope 勾选）、Token 查看、吊销 |
| 审计日志 | 筛选（actor/动作/资源/时间）、导出 |
| 组织设置 | Master Key 状态、SSO、Webhook、Sync 配置 |
| 密钥扫描报告 | CLI 扫描结果上传汇总 |

交互要点：密钥值默认掩码 `••••••`，点击 reveal 走单独接口（再次权限校验 + 审计）；复制到剪贴板 20 秒后自动清空。

---

## 10. 开发路线图

| 阶段 | 周期 | 交付 |
|---|---|---|
| M1 地基 | 3 周 | repo 脚手架、PG schema、信封加密、用户注册登录 + JWT |
| M2 密钥核心 | 4 周 | 项目/环境/文件夹模型、密钥 CRUD + 版本 + 回滚、reveal 分离 |
| M3 RBAC + 身份 | 3 周 | 角色系统、机器身份、client_credentials、策略求值器 |
| M4 CLI + 前端 | 4 周 | `login/run/get/set/export/scan` 全命令、密钥工作台页面 |
| M5 审计 + 发布 | 2 周 | 审计页、Docker 镜像、Helm Chart、文档站、v1.0 发布 |
| v1.1 | — | 动态密钥（PG/MySQL）、MCP Server、Secret Sync |
| v1.5 | — | OIDC SSO、审批流、访问请求 |

---

## 11. 风险与取舍

1. **加密实现是最大风险点**：不要自己造密码学轮子——AES-GCM 用标准库、Shamir 用 hashicorp/vault 分离出的成熟实现思路、上线前做一次外部安全评审。
2. **open-core 诱惑**：本项目承诺完全开源，可持续模式靠托管云服务/商业支持，而非阉割自托管版。
3. **范围控制**：v1 坚决不做 PKI/PAM，避免重蹈 Vault 复杂度覆辙；动态密钥留好 `SecretEngine` 接口即可。
4. **多语言 SDK 维护成本**：v1 只做 Go SDK，Python/Node 用 OpenAPI 生成器产出，社区迭代。
