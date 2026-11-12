# 标准身份联邦：首批实现与验证

> 本文件保留分阶段实施检查点；其中的剩余工作已由后续提交推进，当前范围见 [P0 验收记录](standard-federation-acceptance.zh-CN.md)。

日期：2026-09-27。对应 [KTI 实施计划](../plans/2026-09-27-standard-identity-federation-plan.zh-CN.md)。这是已批准 P0 计划的首批代码，**不是全部 P0 完成或生产验收**。实现位于 Go 服务；没有闭源工程、私有 SDK、权限目录或提供方专用 Claim 依赖。

> 这是第一批实现快照。后续 PostgreSQL、AAD 信封、幂等写入、事务投递和恢复工具进展见 [存储实施记录](standard-identity-storage.zh-CN.md)；下面“未实现”条目以较新实施记录为准。

## 已实现的边界

- `internal/federation/oidc`：公开 Discovery、Authorization Code + S256、RS256/JWKS、issuer/aud/azp/nonce/时效校验；发现和换码有超时、响应大小限制及禁止重定向。Discovery 的认证、换码和 JWKS 地址须与 issuer 同源。当前客户端认证仅 `client_secret_basic`，不宣称兼容全部可选 OIDC 画像。
- 部署管理员登记精确 issuer 白名单；组织管理员只能选择已允许的 issuer。HTTP 仅用于显式 loopback 开发地址；生产使用 HTTPS。白名单授权包括信任其 DNS、发现文档及同源端点，仍应由部署的出站网络策略限制可达范围。
- 登录请求保存 state/browser 摘要、加密 PKCE verifier、nonce、连接配置摘要、精确回调及五分钟期限。回调在换码前原子消费，浏览器相关性、组织入口或配置变化不符均拒绝。回调不使用请求 Host / Forwarded 构造目标。
- 验证后的 `(issuer, sub)` 是身份键。无 email/groups/tenant Claim 也能登录；邮箱不自动链接本地账号。同 issuer/sub 可由本地准入进入多个组织；不同 issuer 的同名 sub 不合并。不同 Client 的 pairwise sub 也不猜测关联。
- 新连接的 JIT 默认关闭；管理员显式开启表示允许该提供方的已认证用户加入绑定组织。默认 viewer，owner 映射上限 admin。映射仅精确匹配完整 Claim 值，不推导组路径末段。角色同步只管理由该连接授予且未被本地管理员修改的角色。
- 成员移除保留准入撤销记录，不能通过重新登录触发 JIT 自动恢复。已删除连接的历史来源记录不会删除秘密。
- HttpOnly / SameSite=Lax 浏览器会话，HTTPS 使用 Secure。数据库只存随机会话摘要；浏览器不收到 ID Token、Access Token 或 Refresh Token。写操作要求精确 Origin 与绑定当前会话的 CSRF 证明。会话只属于登录时选定的组织。
- 每次请求重查用户、成员、连接配置和部署策略。成员删除、账号停用、连接删除/禁用/认证配置变化，下次检查拒绝。已经完成授权的在途操作不声称能被撤回。
- 本期只支持管理员显式选择 `BOUNDED`：15 分钟且不超过原 ID Token 期限，不自动刷新。上游账号/组变化不会被离线会话即时感知。未实现 ONLINE_REQUIRED、强认证策略和上游退出；普通登录不代表 MFA。退出仅结束 taboo 本地会话。
- OIDC 登录、连接管理及成员角色修改与审计同事务；审计失败不留下会话或配置变更。
- 旧本地用户/机器 Bearer 继续按原规则使用。非 user/identity 类型的本地签名 Token 不能进入业务 API；历史外部用户换出的本地 Bearer/Refresh 被拒绝，须重新 OIDC 登录。

## 当前可访问的功能

外部浏览器来源可访问当前组织的项目、环境、文件夹、秘密列表/详情/版本及按现有角色授权的写入；服务端通过显式路由准入再检查组织归属。元数据、明文读取仍执行各自已有授权检查。组织审计读取允许，但当前 UI 尚未拆分导出能力，因此外部用户暂不显示审计页。

机器身份管理、Sync、Webhook、批量导出、动态凭据、成员/身份连接治理等尚未为新浏览器来源完成逐项适配，默认拒绝，不因新的登录来源自动开放。本地认证保留原能力。未完成计划中的细粒度权限迁移、强认证或全产品保证画像，不应把该首批代码用于要求这些能力的生产接入。

## 配置与升级

运行新 Go 版本之前，备份数据库、配置及实际使用的根材料。新表为增量 schema 10，保留组织、项目、秘密及历史版本 ID；这不是 SQLite→PostgreSQL 迁移。

```sh
TABOO_PUBLIC_ORIGIN=https://secrets.example.org
TABOO_OIDC_SESSION_PROFILE=BOUNDED
TABOO_OIDC_ISSUER_ALLOWLIST=https://idp.example.org
```

白名单可用逗号分隔多个精确 issuer，**不省略或归一化末尾斜杠**。在提供方登记回调：`https://secrets.example.org/api/v1/auth/oidc/<组织slug>/callback`。以本地管理员进入组织设置 → 用户源 / SSO → 添加身份连接，登记 Client 和 Secret，按需要显式启用 JIT。未公开的连接通过组织登录链接进入。

环境引导仍支持 `TABOO_OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/ORG`；新增 `TABOO_OIDC_AUTOCREATE=true` 才在新建配置中启用 JIT。默认显示名改为 OpenID Connect。已有配置不会被环境引导覆盖，必须盘点其 autocreate 与 role_map。此前依赖组路径末段的映射需改为完整值。旧 `username_claim` 只为配置兼容保留，不是身份键。

旧 OIDC 会话统一重新登录；旧外部用户只可按准确的 provider/sub 关联迁移，不能利用同邮箱、提供方更名或 issuer 修改证明是同一人。已建立的外部身份不能被另一 issuer 占用。跨提供方账户迁移工具尚未实现。回退旧二进制可能重新引入旧认证行为，**本次未完成降级/恢复演练**。

Node 参考服务不具备新的 BFF 协议。共享前端在 Node 的 session 端点返回 404 时保留本地账号使用；外部标准联邦以 Go 服务为准。

## 公开复现与本地证据

固定真实 OP：Keycloak 26.6.3，镜像 `quay.io/keycloak/keycloak@sha256:9b0330756022422149aa6502eb2def8cd47c6e1b000c7c65cdb13e7c0133e992`。`tests/federation/keycloak-realm.json` 仅含公开测试账号和测试 Secret，不能用于部署。测试 Client 没有默认/可选 scope mapper；仅请求 openid，不依赖邮箱、租户或组输出。

```sh
npm ci
npm exec --workspace=@taboo/web -- playwright install chromium
TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh
# 如 18081 被占用，可设置 TABOO_TEST_KEYCLOAK_PORT
```

脚本启动独立临时 Keycloak，测试完成自动删除该容器；浏览器服务使用一次性数据库，退出时清理。真实 OP 测试有强制模式，CI 不能以未配置 issuer 为由静默跳过。普通 `go test` 中明确提示真实 OP 用例需该脚本。

本地已运行的证据：

- Go `go test -race ./...`：通过（普通运行中真实 OP 测试明确跳过，另用专用脚本执行）。
- 独立协议夹具：无专有 Claim、错误 issuer/aud/azp/nonce/时效、state/browser/PKCE、回调重放、双组织/双 issuer、JWK 轮换、显式 JIT、撤销后不再入组、连接配置变更、审计回滚、本地角色来源保护、Token 类型分离。
- 真实 Keycloak：完整授权码 + PKCE + BFF 登录、API 组织隔离、回调重放拒绝、CSRF 及退出；不把该证据扩展成所有提供方互操作认证。
- Chromium（5 项真实浏览器用例）：实际登录、刷新、Cookie 不可读、localStorage 无外部 Token、跨组织拒绝、CSRF、退出、连接停用、390px 页面/表单与 Escape/焦点恢复。会话请求失败的重试有独立用例；开发者在 dev 创建/更新/reveal/回滚成功，切换受保护环境清除明文并禁止写入。
- 本地账号原有 14 项冒烟全部通过（注册、读写、历史回滚、导出、审计、未认证拒绝）。回归修复了回滚事务中再次占用 SQLite 单连接导致的等待，并在版本变化时清除页面已显示的旧明文。
- OpenAPI 类型生成、TypeScript 类型检查、Vite 构建通过。为已有响应补充实际必填字段，避免生成类型把必有标识视为可选。
- UI 严格静态检查通过（0 error）；登录、身份表单和原有表单统一使用共享验证组件，补齐本地化校验反馈与首个错误焦点。该静态结果不等于全产品可访问性验收；旧非身份弹窗、全部多标签状态广播与 KTI-07 全矩阵仍未完成。


新增公共 CI job 可复现上述联邦与浏览器测试；**尚未 push，远程 CI 没有执行，也没有 dev/生产部署或任何私有提供方联调证据**。

## 剩余工作包

| 工作包 | 当前状态与尚缺内容 |
| --- | --- |
| KTI-00 | 通用人员登录契约和真实 Keycloak 样板已落地；完整 OAuth/能力与授权矩阵仍待冻结 |
| KTI-01 | SQLite 增量联邦表已落地；PostgreSQL、RLS、工作负载模型和迁移失败门槛未完成 |
| KTI-02 | BFF 登录、本地退出、会话/CSRF 首批完成；组织强制外部登录、刷新策略及扩展退出未完成 |
| KTI-03 | 准入、精确映射、来源保护、本地撤权完成首批；ONLINE_REQUIRED、强认证及权限迁移未完成 |
| KTI-04 / 05 | 密文/AAD/CAS 和消费者任务持久化改造未开始；本次仅补齐联邦事务审计 |
| KTI-06 / 08 | 外部机器 JWT/内省、WorkloadBinding、CLI/SDK/MCP 未开始 |
| KTI-07 / 10 | 登录与基础页面、公开测试首批已落地；全产品 UI/消费者矩阵及远程 CI 未完成 |
| KTI-09 / 11 | 迁移恢复与生产部署画像、发行验收未完成 |

P1 SCIM/AuthZEN/Back-Channel 未包含。下一批优先补齐 KTI-01 存储与组织策略，再推进授权/机器消费；本记录不替代原计划门槛。

协议依据：[OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html)、[Discovery](https://openid.net/specs/openid-connect-discovery-1_0.html)、[RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html)、[go-oidc](https://pkg.go.dev/github.com/coreos/go-oidc/v3/oidc)、[Keycloak 容器文档](https://www.keycloak.org/server/containers)。
