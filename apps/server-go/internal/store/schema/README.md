# 数据库结构迁移

结构脚本是**唯一真源**：Go 代码里不再拼 DDL。迁移用
[goose](https://github.com/pressly/goose)（MIT）执行，启动时自动运行。

## 目录

| 目录 | 方言 | 迁移数 | 内容 |
| --- | --- | --- | --- |
| `pg/` | PostgreSQL | 13 | 账号 → 项目 → 身份 → 审计 → 集成 → 运行时 → 联邦 → SCIM → 设备 → 函数 → 触发器 → RLS → 运行角色授权 |
| `sqlite/` | SQLite | 10 | 同序领域，方言不同；表、索引、触发器都在这里 |

文件名 `00001_account.sql` 的前缀就是迁移版本号，**必须是连续、唯一、递增的整数**。

## 启动行为

`Open` / `OpenPostgres` 调用 `store.migrateSchema`：

1. 确认 `goose_db_version` 存在（没有就建，并写入版本 0）；
2. 读取已应用的最大版本；
3. 版本高于本二进制 → 拒绝启动（不做降级）；
4. 基线化判断（见下）；
5. `provider.Up()` 按序执行缺失脚本，每个脚本一个事务。

PostgreSQL 额外做两件事：`checkRoles` 校验控制登录/运行角色形态（控制登录
NOSUPERUSER NOBYPASSRLS、运行角色 NOLOGIN 且不继承控制角色），并挂上 goose 的
**会话锁**（`pg_advisory_lock`），多副本同时启动时只有一个实例真正执行迁移。
SQLite 是单文件单写者，靠连接串行化。

## 基线化（老库怎么办）

goose 之前的两代实现没有执行历史，只有终值：PostgreSQL 的 `schema_version.version`、
SQLite 的 `PRAGMA user_version`。首次用 goose 打开老库时：

| 终值 | 行为 |
| --- | --- |
| 0 | 空库，从头执行全部迁移 |
| 23（旧迁移器的终值） | **基线化**：把 1..N 标记为已执行，不重放任何脚本 |
| 其他 | 报错并提示：旧的中间版本结构不同，请用旧版本二进制先升到 23，或重建数据库 |

基线化必须存在，因为这些脚本创建的是**终态结构**，对已有数据的库重放会 `CREATE TABLE`
失败（或更糟：重建空表）。基线化只写 `goose_db_version`，不碰业务表。

## 关闭自动迁移

`TABOO_AUTO_MIGRATE=0`：启动时只校验版本是否等于本二进制期望值，不一致就拒绝启动。
适合由独立迁移 Job 负责、应用账号无 DDL 权限的部署。

## 运维命令

```bash
taboo-server schema     # 只读：dialect / applied / expected / pending
taboo-server --check    # 自检：schema 版本、RLS、触发器、密钥可解
```

## 新增一次结构变更

1. 在 `pg/`（和/或 `sqlite/`）新增下一个编号的脚本，例如 `pg/00014_webhook_retry.sql`；
2. 脚本第一行写 `-- +goose Up`；
3. 更新 `store.SchemaVersionPostgres` / `SchemaVersionSQLite` 常量；
4. 跑 `go test ./internal/store ./internal/server`——`TestPostgresTwoReplicas` 会在真实
   PostgreSQL 上从空库重放全部脚本，验证顺序与幂等性。

**不要修改已经发布过的脚本**：goose 按版本号记录执行状态，改内容不会重跑，
不同环境会悄悄产生结构差异。需要修正就新增一个版本。

## 写脚本的注意点

- goose 默认**按 `;` 切分语句**。PL/pgSQL 函数体（`$$ … $$`）内部的 `;` 会被切碎，
  必须用 `-- +goose StatementBegin` / `-- +goose StatementEnd` 包住（见 `pg/00010_functions.sql`）。
- PostgreSQL 脚本里有两个部署占位符，由 `store.migrations.go` 在执行前替换：
  `{owner}`（控制登录，用于 RLS 策略判定）和 `{role}`（运行角色，用于授权）。
  仓库里不存角色名。
- 运行角色不该读到迁移表：`pg/00013_runtime_grants.sql` 里 `REVOKE ALL ON goose_db_version`。
