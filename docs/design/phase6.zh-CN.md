# Phase 6：审批、限时访问、SCIM、Back-Channel Logout、CLI 登录与多副本

本文记录已实现的范围和取舍。dev122 仍是 schema 17，本阶段不在这里部署。

## 审批

项目增加 `approval_required`。新建项目默认关闭，可在控制台或 `PATCH /projects/{id}` 切换。

开启后，`upsert` / `delete` / `rollback` 不直接改密钥，而是写入 `approval_requests`。明文用组织 DEK 以 `approval` 用途密封，审计、列表和日志都不含明文。批准人必须是另一名 owner 或 admin（机器身份不能批，申请人不能批自己）。批准与写入在同一事务：冲突或失败会回滚，请求仍是 `pending`。默认 24 小时过期，过期后不能再批。导入在这种环境上直接拒绝，预览仍可用。

## 限时访问

成员可申请 `reveal` / `export` / `write` / `delete` / `rollback`，时长 1 分钟到 4 小时。批准后 `access_requests.expires_at` 生效。授权检查读这张表，到期立刻失效，不依赖清扫是否赶上。清扫只把状态改成 `expired` 并写审计。批准人规则与审批相同。限时授权不能变成 admin、成员管理或审计导出。外部身份的敏感操作仍受近期认证约束，限时授权不能绕过。

## SCIM

`/api/v1/scim/v2` 提供 Users、Groups 和发现端点。令牌是 `taboo_scim_` 前缀的随机值，只存 SHA-256，只在创建时返回一次。令牌属于一个组织。

用户按本组织的 `externalId`（没有则按 SCIM id）建档，从不按邮箱并入已有账号。密码不可用。组可以映射到 viewer、developer 或 admin，不能映射 owner。管理员手工改过的角色（`scim_managed=0`）和 owner 不会被组同步覆盖。停用只拿掉本组织的 SCIM 成员资格；仅当该用户没有其他组织且来源是 SCIM 时，才把账号标为 disabled。过滤器只接受 `userName` / `externalId` / `displayName` 的 `eq` 比较，参数绑定，拒绝其他语法。

## OIDC Back-Channel Logout

`POST /api/v1/auth/oidc/{org}/backchannel-logout?pid={provider}` 接受 `application/x-www-form-urlencoded` 的 `logout_token`。校验 RS256、issuer、audience、`events`、短有效期，拒绝带 `nonce` 的令牌。有 `sid` 时只结束该会话，否则按 `sub` 结束该提供方下的全部会话。`jti` 或令牌哈希写入 `logout_replays`，重复投递失败。登录时把 ID Token 的 `sid` 和 `sub` 记在 `browser_sessions` 上。

## CLI 浏览器登录

`taboo login --device` 走 RFC 8628：设备码只存哈希，用户码 8 位，10 分钟有效，轮询快于间隔返回 `slow_down`。批准页是 `/device`。

`taboo login --browser` 在 `127.0.0.1` 上听一个回环端口，S256 PKCE，回调路径必须是 `/callback`。授权码两分钟、一次性。两种方式发的都是现有用户 JWT 和 refresh，不把浏览器会话写进 CLI 磁盘以外的地方。

## 多副本

PostgreSQL 上可以起多个进程，共用同一个库、同一个 Master Key 和同一个 `TABOO_JWT_SECRET`。SQLite 仍然是单连接单副本。

正确性不靠进程内存：

- outbox 用 `UPDATE ... WHERE status='pending' RETURNING` 认领，订阅者在同一事务里写下 webhook 和同步任务。每个副本启动时注册同一组订阅者。发现文档缓存和 DEK 缓存可以各进程一份，丢了会重算。
- 轮换、动态凭据、webhook、同步沿用原来的行租约。
- `cluster_leases` 保证审计检查点、审批过期和限时授权过期同一时刻只有一个副本在扫。租约过期后其他副本接手。这些更新本身也是条件更新，重复执行不会做第二次。
- 登录限流写入 `rate_counters`。数据库不可用时返回 503，不改成放行。`DB == nil` 的进程内限流只留给没有库的路由自检。

导出文件仍在本机磁盘。多副本要共用导出卷，否则下载请求落到另一台会 404。Helm 默认仍是 1 个副本。

## 明确不做

- **SAML**：现有登录是 OIDC 和 GitHub OAuth，dev122 没有要求 SAML 的身份源。完整的 SP 要处理签名、条件、受众、重放和加密断言；半套实现会变成新的认证入口。继续留到真有身份源再做。
- **AuthZEN**：没有调用方。单独开一个决策接口会多一条探测密钥是否存在的途径，而授权已经在每次请求里做完。
- **在 dev122 上联调真实 OIDC**：代码路径和本地测试提供方已经覆盖。真实 Keycloak 需要运维凭据和变更窗口，不在这次代码变更里。
