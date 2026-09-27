# 标准 OAuth 工作负载与消费者

此模块在 taboo 内实现资源服务器和本地授权，不引用任何私有身份工程。浏览器会话策略与机器画像分别配置。以下是本地实现及公共测试说明，不代表所有提供方兼容或真实部署验收。

## 明确选择认证来源

组织 owner 通过 `/api/v1/orgs/{slug}/oauth-trusts` 登记可信 issuer、API audience、元数据模式和验证画像。issuer 必须位于部署者配置的 `TABOO_ISSUER_ALLOWLIST`（旧 `TABOO_OIDC_ISSUER_ALLOWLIST` 继续兼容）。外部调用同时发送 `Authorization: Bearer …`、`X-Taboo-Trust: <本地 trust ID>`、`X-Taboo-Organization: <组织 slug>`。这些头只选择登记项，不授予权限；验证失败不再尝试本地 JWT 或浏览器 Cookie。

元数据模式 `oauth` 使用 RFC 8414 地址，`oidc` 使用 OIDC Discovery。当前公开画像支持 RS256 和 `client_secret_basic`，仅允许 HTTPS（本机临时测试允许 loopback HTTP）、精确 issuer、同源端点、禁止 HTTP 重定向、10 秒超时和 1 MiB 响应上限。端点元数据缓存 5 分钟；JWKS 缓存按公开验证库处理轮换。能力缺失拒绝配置。不能把任意 JWT 当成 RFC 9068 Token。

| 验证画像 | 必须验证 | 上游状态保证 |
| --- | --- | --- |
| `JWT_RFC9068` | RS256 签名、`typ=at+jwt` 或 `application/at+jwt`、iss/sub/client_id/aud/iat/exp/jti/scope、nbf（存在时） | `BOUNDED`，接受的 Token 总寿命上限可配置为 60–900 秒，默认上限 900 秒；上游停用不使已发离线 Token 立即失效 |
| `INTROSPECTION_RFC7662` | 每次受保护请求在线验证 active、Bearer、iss/sub/client_id/aud/iat/exp/scope；opaque 不被解析 | `ONLINE_REQUIRED`，不缓存 ALLOW；inactive 拒绝，网络/服务错误 503；上游账户停用是否撤销旧 Token 仍取决于提供方语义 |

7662 本身不要求所有响应字段；上述字段是 taboo 的显式接入画像。缺少 audience 或身份上下文的提供方不能启用此画像。JWT 时效不为 exp 添加宽限；iat/nbf 最多 30 秒时钟偏差。所有实例应同步时钟。

## 绑定与权限

`/orgs/{slug}/workloads` 把精确 `(trust, sub, client_id)` 绑定到本地机器身份，并登记项目、环境、动作和动作要求的单个 OAuth scope。`read`、`write`、`reveal`、`export`、`rollback`、`delete` 分开授权，Token scope 与本地 grant 必须同时满足；write 不推导 reveal 或 export。`PUT /workloads/{id}/grants` 原子替换，`POST /workloads/{id}/revoke` 立即本地撤销。信任、绑定、授权变更与审计同事务。服务端每次重新检查本地启用状态；已在途通过授权的操作不承诺撤销。

工作负载只允许项目 secrets、folders、environments 和 export 路由，并在业务处理前检查组织归属。不允许组织管理、动态引擎和其他治理路由。原有本地机器 scope 保留原语义（write 包含 reveal/export），外部迁移需明确列出所需动作，不能直接拷贝旧 scope 推导授权。

trust 的 issuer 不可原位修改。更换提供方须新建 trust、证明准确主体、重新登记绑定，并撤销旧绑定；组织 ID、项目、密文不随之改变。服务端不按邮箱或同名 client 自动关联，外部绑定不生成可交换本地 Token 的 client secret。

## SDK、CLI、MCP

Go SDK 使用 `WithOAuthClientCredentials(OAuthCredentials{...})`；CLI 和 MCP 使用相同环境配置：

```sh
TABOO_AUTH_MODE=oauth
TABOO_SERVER=https://kms.example.test
TABOO_OAUTH_ISSUER=https://login.example.test
TABOO_OAUTH_METADATA_MODE=oidc
TABOO_CLIENT_ID=registered-machine-client
# TABOO_CLIENT_SECRET 从部署的 secret 引导来源注入，不放入命令参数或仓库
TABOO_ORG=example-org
TABOO_OAUTH_TRUST_ID=registered-trust-id
TABOO_OAUTH_SCOPES='kms.read kms.reveal'
TABOO_PROJECT_ID=registered-project-id
TABOO_ENV=dev
# 仅提供方支持 RFC 8707 时配置；否则在上游登记正确 audience
TABOO_OAUTH_RESOURCE=https://kms.example.test/api
```

CLI `login --oauth` 仅检查凭据，不把 Token 或 secret 写盘。外部 Token 在进程内缓存，过期前 30 秒重新获取，并发获取合并。请求失败不会回退到已保存的本地会话，也不会自动重试写请求。API 401/403 保留给消费者处理。MCP `secrets.get` 默认只读取元数据并输出固定掩码；显式 `reveal=true` 才调用明文 API。CLI `run` 需要独立 export grant。

切换回本地机器模式须明确设置 `TABOO_AUTH_MODE=local` 并移除外部 issuer 配置；原有 CLI 登录、Go `WithIdentity` 和 MCP 本地凭据继续可用。CLI 可用 `TABOO_SESSION_FILE` 指定本地会话文件，默认仍为 `~/.taboo/session.json`。不在外部模式覆盖旧本地会话。

## 可复现证据

- `./scripts/test-consumers.sh`：真实 Go SDK 请求、CLI 子进程、长驻 MCP stdio；JWT、opaque 和原本地身份三组。覆盖读写/导出、CLI run、MCP 默认无明文、scope/组织/issuer 错误、缓存 Token 本地撤销。
- `./scripts/test-federation.sh`：固定摘要 Keycloak 的浏览器与真实 client_credentials / introspection。Keycloak 普通 JWT 未标称 9068；测试通过内省画像，分别登记 API audience 与内省客户端 audience，不关闭 audience 检查。
- `TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh`：非超级用户 PostgreSQL、RLS、存储/恢复与同一消费者/协议测试。
- 公共 CI 分别包含 SDK、MCP、Node OAuth 单测和上述必跑套件。普通 `go test ./...` 会提示跳过需要专用环境的测试，专用 job 不允许静默跳过。
