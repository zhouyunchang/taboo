# PostgreSQL 结构脚本

这是 PostgreSQL 结构的**唯一真源**。服务启动时按文件名顺序在同一个事务里执行，
`store/postgres_schema.go` 只负责嵌入与顺序，不再在 Go 里拼 DDL。

| 文件 | 内容 |
| --- | --- |
| `10-account.sql` | 账号、组织、成员、刷新令牌、恢复码、定位符 |
| `20-projects.sql` | 项目、目录、密钥、密钥版本、组织安全策略、操作授权、邀请 |
| `30-identity.sql` | 机器身份、scope、OAuth 信任、工作负载绑定与授权 |
| `40-audit.sql` | 审计日志与链头、检查点、导出、幂等回执、事件总线 |
| `50-integration.sql` | Secret Sync 目标与记录、Webhook 与投递 |
| `60-runtime.sql` | 动态密钥引擎与租约、轮换策略、扫描报告、审批与限时访问 |
| `70-federation.sql` | OIDC 提供方、浏览器会话、外部身份、联邦准入 |
| `80-scim.sql` | SCIM 2.0 令牌、用户、组 |
| `90-device.sql` | 设备授权、CLI 授权码、限流计数、集群租约 |
| `92-functions.sql` | 不可变与完整性触发器函数（`CREATE OR REPLACE`） |
| `94-triggers.sql` | 行级触发器（先 `DROP` 再建，可重复执行） |
| `95-rls.sql` | 行级安全策略与逐表授权 |
| `99-schema.sql` | 版本表、运行角色授权、版本号写入 |

约定：

- 每个脚本都可以**重复执行**：建表前 `DROP TABLE IF EXISTS … CASCADE`，函数用
  `CREATE OR REPLACE`，触发器与策略先 `DROP`。
- 只有两处占位由迁移替换：`{owner}`（控制登录，schema 属主）、`{role}`（运行角色）、
  `{{version}}`（当前 `SchemaVersion`）。
- PostgreSQL 版本差异只体现在标识符与默认值上；SQLite 的同义脚本在 `sqlite/`。
- 新增对象时先改这里，再跑 `go test ./internal/store ./internal/server`——
  `TestPostgresTwoReplicas` 会在真实 PostgreSQL 上完整重放这套脚本。
