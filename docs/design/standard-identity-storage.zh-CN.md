# 标准身份联邦：存储、加密与投递实施记录

> 本文件保留分阶段实施检查点；其中的剩余工作已由后续提交推进，当前范围见 [P0 验收记录](standard-federation-acceptance.zh-CN.md)。

本记录对应 KTI-01、KTI-04、KTI-05、KTI-09 的已落地部分，不代表整个 P0 完结。协议配置、机器 OAuth、完整管理界面、所有既有管理写路径的事务审计和发布验收仍须按主计划继续完成。

## 数据库

- SQLite 便携模式保留；schema 11 升级在同一事务内完成，拒绝未来版本和无效归属关系。旧库升级前备份。
- PostgreSQL 18 使用独立基线和事务迁移锁。`TABOO_DATABASE_URL` 指向非超级用户、无 BYPASSRLS 的控制账号；`TABOO_DATABASE_RUNTIME_ROLE` 必须是独立 NOLOGIN、NOSUPERUSER、NOBYPASSRLS 角色。控制账号拥有业务表，并由 DBA 授予切换到运行角色的权限。
- 所有业务表启用 FORCE RLS。认证后的路由重新绑定到含组织、主体上下文的运行角色连接。基础登录、身份发现和受信后台任务使用控制连接；控制连接的全组织访问是显式的信任边界，不宣称后台任务拥有数据库级租户隔离。
- 请求连接不跨请求复用，在请求结束或异常退出时关闭；控制池不修改角色或组织上下文。这优先保证隔离，部署容量评估必须考虑每个活跃请求最多一个 PostgreSQL 连接。
- 项目授权、文件夹、环境、同步目标、webhook 投递均增加归属约束；归属关系不可直接移动。机器 scope 不能引用其他组织项目。
- 同一组织审计写入以事务行锁串行化，保留原有哈希链。PostgreSQL 使用独立递增序号保存链顺序。

PostgreSQL 多实例必须显式配置相同 `TABOO_MASTER_KEY`、`TABOO_JWT_SECRET`；禁止启动时各自生成密钥。RLS 不是抵御控制账号失陷的机制，也不替代应用权限检查。

## 加密和重试

新写入使用 `taboo:v1:` 信封、AES-256-GCM 和按用途 HKDF 派生的密钥。AAD 包括不可变组织、项目、环境、对象 ID 及版本。组织 DEK 包装也绑定组织。OIDC 客户端凭据、PKCE、TOTP、同步目标、webhook、动态引擎与租约使用各自用途和对象上下文。

读取允许无版本前缀的原格式；有版本前缀但未知或认证失败的密文不会退回旧格式。旧格式自身不提供上下文绑定，必须执行离线升级后才能对存量数据获得该保护。回滚会先解密历史版本，再为新版本重新加密。DEK 缓存同时绑定包装密文与主密钥，返回副本且有容量上限。

密钥创建、更新、回滚、删除支持可选 `Idempotency-Key`（最长 128 字符）。同一组织和主体重复相同请求返回原响应；同一键换请求返回 409。收据仅保存带密钥的请求摘要和公开响应，与业务、审计和 outbox 同事务提交。更新与回滚支持 `expectedVersion`；版本不符返回 409。兼容旧客户端时允许省略这两个字段。

## 持久化投递

请求只写 outbox，不再并行发布内存事件。dispatcher 在同一数据库事务内创建所有匹配的 sync/webhook 任务并确认事件；任一消费者失败则整体回滚。确定性任务 ID 跨 pending/success/dead 去重。

worker 使用有期限的租约和完成时的 token 比较，允许故障恢复并防止过期 worker 覆盖新的处理结果。外部效果为至少一次投递；webhook 通过稳定的 `X-Taboo-Delivery` 标识支持接收方去重。网络中断后的响应丢失仍可能导致外部重复，不宣称 exactly-once。

同步任务保存确切版本的加密快照，不再从组织/环境通配范围内猜一个同名 key。旧任务缺少快照时会失败并进入既有重试/死信流程，需要管理员对明确目标重新入队。删除事件保持既有语义：webhook 通知，第三方同步目标不自动删除。

## 离线升级和恢复

先停止全部服务器与 worker，保存数据库、原主密钥、JWT 配置和审计导出文件目录。SQLite 使用 `taboo-server backup NEW_DIRECTORY`，目标中已存在数据库时拒绝覆盖；PostgreSQL 使用由运维管理的 `pg_dump`/`pg_restore`。凭据通过环境或安全配置传递，不放在命令参数、日志或仓库中。

构建：`make -C apps/server-go build`，同时生成 `taboo-server` 和 `taboo-migrate`。

1. `taboo-migrate verify -data-dir BACKUP_DIRECTORY`：用原主密钥验证所有持久化密文可恢复，不改写密文。
2. `taboo-migrate envelopes -data-dir DATA_DIRECTORY`：验证并报告旧格式数量。
3. `taboo-migrate envelopes -data-dir DATA_DIRECTORY -apply`：原子升级；任何损坏或错误主密钥使整个升级回滚。升级清除浏览器登录流程及会话，用户须重新登录。重复执行不重复加密。
4. SQLite → PostgreSQL：先对停止服务后的 SQLite 副本做备份；设置目标 `TABOO_DATABASE_URL`、`TABOO_DATABASE_RUNTIME_ROLE` 和原 `TABOO_MASTER_KEY`，执行 `taboo-migrate import-sqlite -source SOURCE_COPY -data-dir KEY_DIRECTORY`。目标业务表必须全部为空，稳定 ID、密文、审计链与幂等收据原样复制，并逐表核对数量。浏览器临时会话不复制。
5. 对目标执行 `verify`；完成组织、密钥、审计及消费者验收后才切换连接配置。审计导出文件需复制到相同配置的数据目录，或重新生成导出。
6. 回滚：保持服务停止，恢复原数据库副本和匹配主密钥、原连接配置与版本。切换后发生的新写入不得直接丢弃；需要另行安排停写窗口和数据回收。旧二进制不支持 schema 11，不直接打开升级后的库。

`verify` 和 `envelopes` 打开数据库时会执行当前版本的 schema 迁移，因此始终先备份并使用停机副本。这些工具没有执行任何真实部署或生产切换。

## 可重复验证

- `go test -race ./...`：便携模式、协议与故障测试。
- `scripts/test-postgres.sh`：临时 PostgreSQL，非特权控制账号、RLS、跨组织写入拒绝、请求连接隔离、并发审计、原子投递、密文升级和 SQLite 备份恢复/迁移。
- `TABOO_TEST_WITH_KEYCLOAK=1 scripts/test-postgres.sh`：在 PostgreSQL 下执行真实 Keycloak 与独立协议 fixture。
- `TABOO_TEST_BROWSER=1 scripts/test-federation.sh`：真实浏览器闭环。

当前仍需补充全套消费者迁移、提供方替换与显式身份关联、剩余协议 profile、UI 路由与权限矩阵、生产发布配置验收；以主计划中的未完成项为准。
