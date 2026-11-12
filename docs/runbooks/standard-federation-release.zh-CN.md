# 标准身份联邦：发行、升级与恢复

本手册针对 Go 服务、schema 15、SQLite 单实例或 PostgreSQL 18。身份连接、机器信任、权限和秘密属于 taboo；不需要闭源仓库、私有 SDK、专有 Claim 或私有镜像。

## 部署和引导

参照 [快速开始](../quickstart.md) 与 [Helm 参数](../../deploy/helm/taboo/README.md)。镜像与二进制发行包交付单一 `taboo-server`（含 `migrate` 运维子命令）。应用的 control DB 登录必须 NOSUPERUSER/NOBYPASSRLS，独立 runtime 角色必须 NOLOGIN/NOSUPERUSER/NOBYPASSRLS；不允许 runtime 继承 control。应用验证这些条件后才启动。

保存原始主密钥、JWT 签名材料、数据库、审计导出目录。PG 所有实例显式使用相同根材料；多副本共享导出目录（Helm 使用 ReadWriteMany）。不要跨版本混跑 worker。SQLite 使用单副本/Recreate；数据库迁移期间先停写再升级。

初始化顺序：本地 owner 建立组织 → 登记标准连接 → 双重证明更换 owner 登录身份 → 配置显式操作授权与会话策略 → 验证外部 owner 登录和恢复路径 → 开启 EXTERNAL_REQUIRED。全局关闭密码/注册是独立开关。JIT 默认关闭；开启即允许该提供方的已认证用户加入该组织，映射最多 admin，不能自动成为 owner。

可选 `TABOO_OIDC_*` bootstrap 只登记已存在组织的连接；allowlist 使用统一 `TABOO_ISSUER_ALLOWLIST`，兼容旧 `TABOO_OIDC_ISSUER_ALLOWLIST`。引导凭据经 secret 注入，不写进 values/git。更换 issuer 或已使用连接的 client_id 要登记新连接，不能把原 subject 解释成新提供方的身份。

## 升级步骤

1. 停止所有应用实例和 worker，记录正在回收的动态数据库账号；保留其引擎配置和后台任务，不能因切换登录而丢弃回收责任。
2. 复制停机 SQLite 数据目录，或由 DBA 执行一致的 PostgreSQL `pg_dump`。独立保存原始根材料和导出文件目录。恢复旧版本必须使用与其 schema 对应的原备份，不能让旧程序打开 schema 15。
3. 在隔离副本上执行 `taboo-server migrate verify --data-dir COPY`。该命令打开数据库会进行 schema 迁移；“verify”只保证不改写密文，不代表 schema 只读。缺失/错误根密钥必须失败，已有数据库不会生成替代根密钥。
4. `taboo-server migrate envelopes --data-dir COPY` 先报告旧密文数量，再使用 `--apply` 原子升级。失败不部分改写；成功清除临时浏览器会话，要求重新登录。原组织/用户/项目/秘密 ID 不变。
5. 如需 SQLite → PG，设置目标 DSN/runtime 角色和原 `TABOO_MASTER_KEY`，执行 `taboo-server migrate import-sqlite --source COPY --data-dir KEY_DIRECTORY`。目标业务表必须为空；逐表计数、关联及密文保持，浏览器会话不复制。再执行 `migrate verify` 并核对审计链。
6. schema 15 引入投递顺序和 worker 租约。没有顺序号的历史 outbox 任务进入可诊断的 dead 状态；确认旧队列后，从同步目标“全量重推”生成当前状态任务。不会猜测时间戳顺序或重放过期秘密覆盖新值。
7. 启动新版本，验证 readiness、原秘密解密、审计链、组织隔离、每种实际使用的客户端与身份连接，然后开放写入。审计导出须保留原文件路径或重新生成。

升级前回退只需恢复原镜像、原数据库和匹配的根材料。升级开放写入后，先停写并安排增量数据回收，不能直接恢复旧备份丢弃新数据。动态租约的回收任务随原数据库恢复；人工接管须记录实际数据库账号状态。

## 失败和重试

- write/reveal/export/rollback/delete 分别授权；本地变更后的下一次授权检查使用新状态。已经通过检查且正在执行的请求可能完成，不能宣称取消所有在途操作。
- 秘密写入/回滚可提交 `expectedVersion`（0 为仅创建）；删除使用同名 query。携带 `Idempotency-Key` 的同一请求重试返回原响应。请求参数或主体改变后不能复用该键。
- CLI `set --idempotency-key KEY --expected-version N`；自动生成的收据写到 stderr，不包含秘密。SDK 的可选 `MutationOptions` 保存相同 key/expectedVersion。结果未知时保留原始请求并重试，不能换新 key。浏览器只在内存保留收据和表单；关闭后刷新服务器版本再决定下一步。
- 审计或事务提交失败不发布未确认状态。动态回收失败保留 `revoking` 并退避，达到上限保留 `failed`，可从控制台重新回收。发放/续期提交不明不返回密码，由隔离回收任务收口。
- audit export、sync、webhook 使用带截止时间的唯一租约；过期 worker 不能覆盖新 worker。每目标的同步按组织序号执行，较旧任务重试时不让后续任务越过。
- 审计导出未确认提交时可能留下无引用文件。只在停 worker、核对数据库引用后清理孤儿文件；不删除仍被 job 引用或租约保护的文件。

## 身份与消费者迁移

[身份提供方替换](identity-provider-replacement.zh-CN.md) 保留秘密和本地授权；旧、新身份都要验证，不按 email 关联。一个本地用户保持一个外部身份关联。严格外部登录组织必须始终存在可用外部 owner；跨组织 owner 更换身份可能需要先安排另一位已验证 owner。

[机器 OAuth 说明](../design/standard-oauth-workloads.zh-CN.md) 描述 SDK/CLI/MCP。逐消费者登记新 trust、subject/client_id、准确项目环境/action/scope，先验证只读和拒绝边界，再授权所需动作。确认新方式可用后撤销旧机器身份。新绑定可保留空 grants（默认拒绝所有资源），清空 grants 也能即时撤回所有操作。外部机器暂不接受动态数据库凭据协议；原本地机器模式继续支持它。

上游保证只能选择实际支持的画像：浏览器 BOUNDED 最多 900 秒，ONLINE_REQUIRED 逐次标准内省且故障拒绝；机器严格 9068 JWT 是有界撤销，7662 内省逐次检查。提供方禁用用户是否使旧 Token inactive 需按部署验证；公开 Keycloak 夹具已覆盖该语义。SCIM、AuthZEN、Back-Channel Logout 为独立 P1，不在本次完成声明内。

## 公共验证命令

```sh
npm ci
npm run types
npm exec --workspace=@taboo/web -- tsc --noEmit
go -C apps/server-go test -race ./...
go -C packages/sdk-go test -race ./...
go -C apps/mcp test -race ./...
./scripts/test-consumers.sh
TABOO_TEST_BROWSER=1 ./scripts/test-federation.sh
TABOO_TEST_CONSUMERS=1 TABOO_TEST_WITH_KEYCLOAK=1 ./scripts/test-postgres.sh
docker build -f apps/server-go/Dockerfile -t taboo:standard-federation-test .
node scripts/test-container-recovery.mjs
./scripts/test-helm.sh
```

旧 pre-federation 数据库不迁移；升级流程从空卷开始，旧镜像只用于回滚。测试只使用公开临时夹具，不需真实租户或秘密。Docker、网络依赖和可用端口是前置；必要套件失败或缺少工具应失败，不能当作通过。普通 Go 单测会注明真实 OP/PG/消费者测试未启用，CI 中上述专用 job 显式启用。


发行流水线用 GoReleaser 2.18.0 生成含 `taboo-server` 的 Linux、macOS（amd64/arm64）压缩包；发布镜像当前为 Linux amd64，从已编译二进制的专用 Dockerfile 打包。直接从源码构建的主 Dockerfile 仍由宿主目标平台决定架构。公共 CI 验证配置和不发布快照；tag 发布需 GHCR 登录、syft 与 GitHub OIDC 签名权限。校验和使用 cosign sign-blob，容器使用 digest 签名；这两类签名不能混用。配置依据 [GoReleaser 制品签名](https://goreleaser.com/customization/sign/sign/) 和 [容器签名](https://goreleaser.com/customization/sign/docker_sign/)。本次未执行上传、远端签名或 tag 发布。
