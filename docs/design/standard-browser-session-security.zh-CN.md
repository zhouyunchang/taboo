# 浏览器会话安全画像

公开 OIDC Code + S256 登录后，浏览器只持有 HttpOnly opaque Cookie。Access Token、Refresh Token 和退出用 ID Token 按组织、会话哈希及凭据版本做 AEAD 加密，保存在服务端；不写入浏览器存储、审计或日志。

## 配置与保证

组织 owner 使用 `GET/PUT /api/v1/orgs/{slug}/oidc/{id}/security` 登记安全画像。部署同时明确 `TABOO_PUBLIC_ORIGIN` 和 `TABOO_OIDC_SESSION_PROFILE`。默认连接策略继承该画像，最长会话 900 秒、近期认证 900 秒；可分别在 60–900 秒内缩短。修改连接或安全策略会使现有会话失效。

- `BOUNDED`：本地每次校验当前成员、账号、连接及授权；上游禁用最多受绝对会话时限约束。不会宣称上游即时撤权。
- `ONLINE_REQUIRED`：登录和每次受保护访问都使用公开 RFC 7662 内省。必须配置 audience，并获得 active、iss、sub、client_id、aud、iat、exp、Bearer 类型证据。缺失、inactive、未来 nbf 均拒绝；提供方故障返回 503，不降级为 BOUNDED。不缓存在线 ALLOW。
- 近期认证：发送 `max_age`，要求 ID Token 中有效 `auth_time`。写入、明文、回滚、删除、导出及治理动作均检查当前认证年龄。可配置 `required_acr`（任一精确值）及 `required_amr`（全部精确值）。提供方必须声明所需能力；值的保证含义由管理员与提供方约定，不能把任意字符串解释为 MFA。
- 本地退出始终可用：精确 Origin 和 CSRF 校验后，在同一事务删除会话并记录审计。不会因提供方不可用而阻止本地退出。
- RP-Initiated Logout 为显式可选项：发现已登记同源退出端点后才允许启用；本地退出完成后可跳转该端点，回跳仅使用配置的公开站点根路径。未启用时只结束 taboo 会话。

## 刷新与故障恢复

使用初次响应中的 Refresh Token，在 ID Token 到期，或在线画像的 Access Token 距到期不足 30 秒时刷新。服务端先持久化 `inflight` 标记，再发起一次交换；多实例并发不能重复交换同一个旋转凭据。成功后更新加密凭据版本和审计，绝对会话截止时间及初次认证时间不延长。

按 OIDC Core，刷新响应允许省略 ID Token；若返回则重新验证签名、issuer、aud/azp、sub、可选 nonce 和 auth_time 一致性。遇到交换结果未知、凭据重放风险或提交失败，不自动重用旧 Refresh Token；需要重新登录。崩溃留下的 `inflight` 同样要求重新登录。

数据库备份可包含加密的短期会话材料。密文迁移命令会使旧登录请求及会话过期；导入 SQLite 到 PostgreSQL 不迁移浏览器会话。恢复后用户重新登录，业务对象和秘密版本不受影响。

## 公开互操作证据

独立协议夹具覆盖在线停用/故障、近期认证、acr/amr、并发刷新、主体替换、审计失败后禁止重复刷新、提供方故障下本地退出。真实 Keycloak 夹具使用固定公开镜像，并显式提供标准 `auth_time`、内省 `sub` 和 audience 映射。该配置不依赖租户、组或私有身份字段。

复现：`TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh`；PostgreSQL 使用 `./scripts/test-postgres.sh`。具体测试结果以本次验收记录为准。Back-Channel Logout、SCIM 和 AuthZEN 属于另行批准的 P1。
