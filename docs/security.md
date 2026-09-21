# 安全模型

## 加密架构（信封加密）

```
Root Key (KEK, Master Key)        文件 / 环境变量 / K8s Secret
  └─ Org Data Key (DEK)           AES-256-GCM 加密后存 DB，惰性解密入内存缓存
     └─ Secret Value              AES-256-GCM，nonce 12B 随机绝不复用
```

- Master Key 仅驻留内存；丢失 = 全部密文不可解
- 组织 DEK 使单组织密钥轮换可行（v1.5 暴露轮换接口）
- 同步目标配置 / Webhook 签名密钥 / OIDC client_secret / 动态引擎连接串
  均经 Master Key 信封加密落库，列表接口不回显

## 访问控制（设计文档 §6）

- 用户角色：viewer / developer / admin / owner；**reveal 与 read 分离**
  （developer 禁止 **保护环境** 明文与写入，默认 `prod` 为保护，不再用 slug 硬编码）
- 机器身份：显式 `(项目, 环境, read|reveal|write)`；存量 `read` 升为 `reveal`
- 成员治理：owner 管人（邀请/角色/项目矩阵）；admin 管配置；restricted 成员仅能访问 grant 项目
- 密钥值默认掩码；reveal 走独立接口（二次权限校验 + 审计 + IP 记录）
- **外部用户源（OIDC Realm）**：Keycloak / Authentik 等按 Proxmox 方式接入。
  用户以 IdP `sub` 绑定；登录页展示 `public_login` 的 Realm。组 claim
  （`groups` 或 `realm_access.roles`）映射角色，可每次登录同步。
  新用户不会被映射成 owner。PKCE S256 + nonce。可用
  `TABOO_DISABLE_REGISTER` / `TABOO_DISABLE_PASSWORD` 关掉本地账号。

## 审计

- 敏感路径与业务同事务 fail-closed；哈希链 + Master Key HMAC checkpoint
- 登录失败 / 越权 `authz.denied` / 成功 `sync.push` 均记审计（不含密码与明文）
- 支持 CSV/JSONL 异步导出；`GET /audit/verify` 验真

## 事件外传

- **Webhook**：负载不含明文；HMAC-SHA256 签名头 `X-Taboo-Signature: t=<ts>,v1=<sig>`；
  至少一次投递 + 指数退避；接收方应校验时间戳窗口（±300s）防重放
- **Secret Sync**：单向推送至 GitHub Actions / Vercel / Cloudflare；
  平台令牌加密落库；同步记录只存内容指纹

## 供应链与运行

- 登录限流（5 次/分钟/IP）、登录失败统一错误（不泄露账号存在性）
- Argon2id 密码哈希（m=64MB,t=3,p=4）；JWT 15min + refresh 旋转
- 响应头 `Cache-Control: no-store`、`X-Frame-Options: DENY` 等
- 启动自检 `--check`：加密往返 + schema 版本 + 关键表（仅启动/CI，不作 liveness）
- 探针：`GET /api/v1/healthz`（存活）、`GET /api/v1/readyz`（DB + schema）
- GoReleaser 发布：SBOM + cosign 签名（§8 供应链清单）

## 威胁模型外的内容

- 运行时内存中的明文（任何密钥管理系统都无法对 root 进程保密）
- 侧信道 / 物理攻击；多副本 HA（SQLite 单写者，v2 考虑 PG 主模式）
