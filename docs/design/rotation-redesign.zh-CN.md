# 轮换重设计：需求、Walker 与 UI 简报

> 状态：Draft，2026-11-13。本文只做需求、架构与 UI 设计，不改代码。
> 适用：taboo 0.1.x；当前迁移版本 PG 13 / SQLite 10，本文按下一版迁移设计。
> 2026-11-13 修订：增加存储级 `generate` 路径、生成档位与“到期/逾期”术语；明确外部真源不能凭空随机生成。
> 2026-11-13 确认：fixed 手动更新只做警告 + 必填 reason，不强制勾选“已通知”；`change_ack` 放 P1。

阅读顺序：0–3 是定位与需求；4–6 是模型、调度与执行契约；7–8 是 API 与 UI；9–12 是迁移、验收、分期与待确认。

## 0. 已确认的决策

1. **定位：编排为主；自生成密钥可代生成。** taboo 负责策略、到期、待办、责任、审计和对外通知。对 **taboo 作为真源** 的 secret，v1 增加 `generate` 路径：由 taboo 用 CSPRNG 生成新值并写入新版本；对数据库、云厂商、OAuth 等 **外部真源**，仍由外部轮换器或 adapter 执行，taboo 不凭空生成对方不接受的字符串。
2. **Walker：独立进程。** 新增 `worker` 角色处理定时器与后台循环；API 进程只服务请求。单二进制 / SQLite 仍用 `all` 角色。
3. **启用：保存与启用分开。** 新建策略默认是草稿；`保存草稿` 不调度，`保存并启用` 才计算 `next_run_at`。这直接修掉“空白表单看起来已启用”的问题。
4. **固定密钥永久：** 没有 active 策略就没有 `next_run_at`；`fixed` 是显式的“不轮换”决定，没有到期/提醒/倒计时。`fixed` 不是不可写；手动仍可更新。未设置也没有到期时间，但它是“还没决定”，和 `fixed` 分开显示。
5. **fixed 手动更新：警告 + 必填 reason，不做强制勾选。** 手动更新 fixed 密钥时显示“不会创建轮换轮次、不会改数据库”的警告，`reason` 必填；不要求勾选“我已通知所有消费方”。消费方确认（`change_ack`）放 P1。

## 1. 现状审计

### 1.1 已有的

| 能力 | 现状 | 位置 |
|---|---|---|
| 策略存储 | 每个 secret 最多一条 `rotation_policies`，间隔 300 秒–366 天或五段 UTC cron，二选一 | `apps/server-go/internal/store/schema/pg/00006_runtime.sql` |
| 到期扫描 | Go 服务启动时在进程内起 goroutine，每 20 秒扫一次；行租约 60 秒；每批 32 条 | `apps/server-go/cmd/server/cli/serve.go:103`、`internal/rotation/service.go` |
| 到期动作 | 同一事务写 outbox `rotation_due`、写审计 `secrets.rotation.due`、推进 `next_run_at` | `internal/rotation/service.go` |
| 对外通知 | Webhook 映射为 `secret.rotation_due`，HMAC-SHA256 签名，payload 不含明文；同步器忽略该事件 | `internal/webhooks/webhooks.go`、`internal/sync/sync.go` |
| 控制台 | 轮换表单嵌在“版本历史”抽屉里；新建时预填 24 小时 / 启用 | `apps/web/src/components/RotationPolicy.tsx`、`pages/SecretDetail.tsx` |

### 1.2 真正的缺口

1. **不是没有定时器，而是没有独立 walker 进程。** 扫描逻辑在 API 进程里，每个 API 副本都会跑；没有独立生命周期、健康检查、领导/租约视图，也没有“调度器是否活着”的界面。
2. **到期事件没有闭环。** 目前只有“发一个 webhook”，没有轮次、负责人、宽限期、完成/失败/跳过、回写校验、逾期升级。外部轮换器即使换了密钥，也没有一个正式的回写对象。
3. **默认启用是表单/接口默认值，不是一条已保存的策略。** GET 返回 404 时，前端 `enabled` 仍是 `true`；后端在请求省略 `enabled` 时也取 `true`。没有策略行时不会触发，但界面看起来像已启用。
4. **UI 位置不对。** 轮换是治理动作，却被塞进版本历史抽屉的底部；没有项目级总览、没有轮次历史、没有负责人、没有调度器健康状态。
5. **事件契约不完整。** `secret.rotation_due` 的 payload 只有当前 version，没有 `run_id`、`old_version`、`due_at`、`grace_until`；OpenAPI 的 webhook events 枚举也还没列出 `secret.rotation_due`，文档与实现有漂移。
6. **可观测性不足。** 没有 worker 心跳、到期数量、逾期数量、投递失败汇总；运维只能看日志。

### 1.3 这次要回答的问题

- 谁对这条密钥的轮换负责？
- 什么时候提醒，提醒几次，什么时候算逾期？
- 谁来执行，执行完怎么证明？
- 失败、跳过、逾期怎么留痕？
- 调度器是否活着，没活着时 UI 怎么诚实地说？

## 2. 目标与非目标

### 2.1 目标

- **可编排**：策略有方式、周期、负责人、宽限期、状态。
- **可追责**：每次到期生成一个轮次，记录触发者、负责人、旧版本、结果和证据。
- **可验证**：完成轮次必须有一个比旧版本新的 secret version；没有新版本不能“完成”。
- **可运维**：独立 worker、心跳、租约、追赶、重试、逾期升级、健康状态可见。
- **不误导**：不把“提醒”叫“自动轮换”；自动生成也不等于自动分发；不默认启用；不隐藏调度器离线。
- **可迁移**：现有 `rotation_policies` 的数据和 `secret.rotation_due` 订阅方不被破坏。

### 2.2 非目标（v1）

- 不内置云厂商 / 数据库 / GitHub 等 provider adapter；`mode=adapter` 只预留枚举。
- 不替外部系统生成它不接受的明文；v1 的 `generate` 只用于 taboo 作为真源的 secret。外部真源仍走 manual/webhook，provider adapter 放 P2。
- 不做非对称密钥对（RSA/EC）生成；v1 只生成对称随机字符串。
- 不做邮件 / 短信 / Slack 通知；站内待办 + 现有 Webhook。
- 不做每条策略独立 Webhook URL；复用组织级 Webhook 订阅。
- 不做每个 secret 多条策略、复杂日历、批量轮换。
- 不做自动验证上游凭据；v1 是“版本前进 + 人工确认”。
- 不做组织级默认策略 / 模板；放到后续阶段。
- 不做静态 secret 的过期时间 / TTL；`fixed` 和“未设置”都不会因为时间失效。
- 不做 `locked` / 不可写；`fixed` 只表示不安排轮换，手动仍可更新。需要“连手动也不能改”时另做。
- 不改动态密钥（dynamic leases）的到期回收逻辑。
- 本文的轮换指用户 secret；不是组织 DEK、Master Key 或 JWT 签名密钥的内部轮换。

### 2.3 成功标准

- 每条 active 策略在到期时**恰好**产生一个 open run；两个 worker 并发时也不重复。
- 无策略 / 草稿 / 已暂停 → 不产生 run。
- 到期到 run 创建的 p95 ≤ 60 秒（worker 15 秒轮询）。
- 100% 的 run 有审计记录；0 条 payload 含明文。
- worker 离线 90 秒内，项目页出现明确告警。
- UI 390px 无横向溢出；键盘可完成创建、启用、完成、跳过。
- 空白表单不再显示“已启用”。

## 3. 用户与任务

| 用户 | 场景 | 任务 | 成功 |
|---|---|---|---|
| owner / admin | 季度安全巡检 | 看覆盖率、逾期、失败；给策略指派负责人 | 5 秒内知道哪些密钥没人管 |
| 密钥负责人 / developer | 收到到期提醒 | 去上游换密钥、回写新版本、标记完成 | 一次完成，不用翻聊天记录 |
| 安全 / 审计 | 事后追责 | 看某轮谁触发、谁完成、旧/新版本、证据 | 时间线完整，能导出 |
| 外部轮换器 / CI | 收到 Webhook | 验证签名、换上游凭据、回写、回调完成 | 幂等、可重试、不重复轮换 |
| viewer | 查看状态 | 看策略和下次时间，不改 | 只读但信息完整 |

## 4. 领域模型

### 4.0 术语

| 术语 | 定义 | 不是什么 |
|---|---|---|
| **到期**（due） | **仅 active 策略**排定的“该轮换了”的时间点；到点由 worker 创建一个轮次并通知 | 不是密钥失效、过期、删除；fixed / 未设置没有到期时间；静态密钥不会因为到期自动失效 |
| **轮次**（run） | 一次轮换任务：到期、负责人、旧版本、结果、证据 | 不是密钥版本本身 |
| **宽限期**（grace） | 到期后允许完成的时间窗；`due_at + grace_seconds` | 不是密钥的有效期 |
| **逾期**（overdue） | 超过宽限期仍未完成/分发；任务状态，不是密钥状态 | 不是密钥已失效 |
| **固定**（fixed） | 明确决定“这条密钥不安排轮换”；没有周期、没有 `next_run_at`、没有轮次/提醒 | 不是不可写；手动仍可更新；也不是外部系统永久有效的保证 |
| **未设置** | 还没有任何策略；同样没有到期时间，但可能是遗漏，不算明确决定 | 不是 `fixed`，也不是自动永久承诺 |
| **生成**（generate） | 由 taboo 用 CSPRNG 产生新值并写成新版本 | 不等于轮换完成；还没分发 |
| **分发**（distribute） | 让消费方拿到新值并切换 | taboo 只能通知/推送已有目标，不能保证所有消费方更新 |
| **退役**（retire） | 旧值不再使用；taboo 能标记，但只有签发方能真正吊销 | 不是自动删除旧版本 |
| **轮换**（rotation） | 触发 → 生成/获取 → 应用 → 分发 → 验证 → 退役的完整过程 | 不是“生成一个随机字符串”就结束 |
| **有效期 / TTL** | 动态 lease 等有真实失效时间的凭据 | 静态 secret 没有 TTL；到期只是任务时间 |

UI 文案规则：静态 secret 只说“下次轮换”，不说“密钥到期/过期”；“已逾期”只描述轮次，不描述密钥。fixed / 未设置不显示“下次”，不制造倒计时。

### 4.1 策略（rotation_policies）

| 字段 | 说明 |
|---|---|
| `id` / `org_id` / `project_id` / `secret_id` | 归属；`secret_id` 唯一 |
| `mode` | `generate` / `manual` / `webhook`；`adapter` 预留；`fixed` 状态不使用 mode |
| `interval_seconds` / `cron` | 仅 active/暂停恢复时有意义；二选一；interval 300 秒–366 天；cron 五段 UTC，连续触发至少隔 5 分钟 |
| `generation_profile` / `generation_length` / `generation_prefix` | 仅 `mode=generate`：生成档位、随机部分长度、可选前缀；见 4.7 |
| `auto_generate` | 仅 `mode=generate`：到期是否立即生成；默认 false，开启前必须提示消费方重载 |
| `owner_user_id` | 负责人；active 必填；默认创建者 |
| `grace_seconds` | 宽限期，0–30 天；默认 7 天 |
| `status` | `draft` / `active` / `paused` / `fixed` |
| `fixed_reason` / `fixed_by` / `fixed_at` | `fixed` 必填原因、操作者、时间；用于区分“明确固定”和“忘记配置” |
| `next_run_at` | draft/paused/fixed 为 null；active 为下一次到期 |
| `last_run_at` / `last_run_id` | 最近一次轮次 |
| `created_by` / `created_at` / `updated_at` | 留痕 |
| `version` | 乐观并发；编辑时冲突返回 409 |

`fixed` 的语义：没有周期、没有 `next_run_at`、worker 不扫描、不产生轮次/提醒。值仍可手动更新；“固定”只表示不安排轮换，不是不可写，也不是对外部系统永久有效的承诺。

兼容：保留 `enabled` 列一个版本周期；API 返回 `enabled = status === 'active'`，新写入以 `status` 为准。

### 4.2 轮次（rotation_runs）

| 字段 | 说明 |
|---|---|
| `id` / `policy_id` / `org_id` / `project_id` / `secret_id` | 归属；`policy_id` 与 `secret_id` 都允许置空，删除策略/密钥后保留历史 |
| `secret_key` / `folder_path` / `policy_snapshot` | 快照：key、目录、当时的 mode/周期；保证删除策略/密钥后历史仍可读 |
| `run_key` | 幂等键：定时轮次 = `policy_id + due_at`；手动 = 新 ID |
| `trigger` | `schedule` / `manual` / `retry` |
| `status` | `due` / `awaiting_generation` / `awaiting_distribution` / `awaiting_manual` / `awaiting_external` / `overdue` / `succeeded` / `failed` / `skipped` / `canceled` |
| `due_at` / `grace_until` | 到期与逾期线 |
| `old_version` | 到期时的 secret 版本 |
| `new_version` | 生成/完成时写入的版本；未生成 null |
| `owner_user_id` | 到期时的负责人快照 |
| `actor_id` / `actor_kind` | 触发者；定时为 system |
| `attempts` / `last_error` | 通知/外部的尝试次数与最后错误 |
| `evidence` | JSON：生成档位/长度/熵、Webhook delivery id、完成备注、验证方式、操作者 |
| `created_at` / `started_at` / `finished_at` | 时间线 |

约束：

- 定时轮次唯一：`UNIQUE(policy_id, due_at)`（仅 `trigger='schedule'`）。
- 每条策略最多一个 open run：`status in ('due','awaiting_generation','awaiting_distribution','awaiting_manual','awaiting_external','overdue')` 的部分唯一索引。
- 完成幂等：同一 `new_version` 重复 complete 返回 200；已完成后换版本返回 409。
- 外键：`policy_id` / `secret_id` 用 `ON DELETE SET NULL`，不级联删除历史；策略删除只删策略。

站内待办不新增通知表：由 open/overdue run 直接派生；导航徽标、项目页计数、密钥页摘要查同一数据源。

### 4.3 Worker 心跳（worker_heartbeats）

| 字段 | 说明 |
|---|---|
| `worker_id` | 进程实例 ID |
| `role` | `rotation` / `webhook` / `all` 等 |
| `hostname` / `version` | 部署排查 |
| `started_at` / `last_seen_at` | 存活 |
| `last_sweep_at` | 最近一次扫描 |
| `claimed` / `errors` | 最近一次扫描的认领数/错误数（可选） |

每 10 秒 upsert；超过 30 秒未更新为“心跳变慢”，超过 90 秒为“离线”。保留最近 24 小时，定时清理。

### 4.4 状态机

策略：

```text
draft ⇄ active ⇄ paused
  \       |       /
   \      v      /
    fixed（明确不轮换）──改为轮换──> active
```

- `draft`：已保存，不调度，`next_run_at` 为空。
- `active`：必须有周期和负责人；编辑周期重新计算下次时间。
- `paused`：保留历史，不调度；恢复时从当前时间重新计算，不补发暂停期间的轮次。
- `fixed`：明确不轮换；没有周期、`next_run_at`、轮次、提醒；必须填原因。值仍可手动更新；不是不可写。
- `fixed` 可以从 draft/active/paused 进入；转入时取消该策略的 open run（审计 `rotation.run.canceled`，原因 `fixed`）。
- 删除策略不删除历史轮次；轮次保留 key 快照。删除 fixed 后回到“未设置”，不是“固定”。

轮次（三条执行路径）：

```text
定时 / 手动创建
  ├─ generate ─> awaiting_generation ─生成─> awaiting_distribution ─确认分发─> succeeded
  │                    │                              ├──跳过──> skipped
  │                    └────────超过宽限期────────────> overdue ──完成/跳过──> ...
  ├─ manual   ─> awaiting_manual ──完成──> succeeded
  │                    ├──跳过──> skipped
  │                    └──超过宽限期──> overdue ──完成/跳过──> ...
  └─ webhook  ─> awaiting_external ──回调完成──> succeeded
                       ├──回调失败──> failed
                       └──超过宽限期──> overdue ──完成/跳过──> ...
```

`due` 是创建后、通知入队前的瞬时状态；对外可统一显示“待处理”。

- 到期由 worker 创建 `due`，同一事务写入 outbox；投递后转为 `awaiting_*`。
- `generate` 路径下，到期只创建 `awaiting_generation`，**不自动改值**；负责人点“生成新值”后才写入新版本，再进入 `awaiting_distribution`。`auto_generate=true` 才在到期时直接生成。
- `overdue` 不是终态；它一直可见，直到完成或跳过。
- 失败是终态；重试是新建一个 `trigger=retry` 的轮次，并记录 `previous_run_id`。
- 完成时如果 `next_run_at` 已过，推进到下一个未来时间，避免完成后立刻再跑一轮。

### 4.5 调度语义

- 所有时间存 UTC epoch 秒；UI 显示本地时间，cron 标注 UTC。
- interval：300 秒–366 天；cron：五段，连续两次至少隔 5 分钟。
- worker 每 15 秒轮询；每批最多 64 条；只有 `status='active'` 且 `next_run_at` 非空的策略会被扫描。`draft` / `paused` / `fixed` / 未设置都不会产生轮次。
- `fixed` 和“未设置”没有 `next_run_at`，也没有 TTL；从 taboo 的角度它们永久有效，除非被手动更新/删除，或外部系统自己让凭据失效。
- 到期比较用数据库时间（PG `now()` / SQLite `strftime('%s','now')`），避免多副本时钟偏差。
- **追赶**：worker 停机后重启，只补一轮：用存储的 `next_run_at` 作为 `due_at`，创建一条 run，然后推进到下一个未来时间；不按错过次数补发。
- **合并**：如果策略已有 open run，不创建新 run；推进 `next_run_at`，写审计 `rotation.run.coalesced`。避免逾期堆积。
- **暂停/恢复**：暂停清空 `next_run_at`；恢复从当前时间重算。
- **手动运行**：立即创建一条 `trigger=manual` 的 run；不改 `next_run_at`；有 open run 时返回 409。
- **interval 的节拍**：v1 是固定节拍，从 `next_run_at` 推进，不随完成时间漂移。“每 30 天”是计划节拍，不是“完成后 30 天”。如果未来要 after-completion 语义，加 `schedule_basis=after_completion`，v1 不做。
- **宽限期**：`grace_until = due_at + grace_seconds`；超过后轮次转 `overdue`，发一次 `secret.rotation_overdue`，不自动停用策略。
- **生成时机**：`mode=generate` 默认在到期后等负责人点“生成新值”；`auto_generate=true` 才由 worker 在到期时直接生成。后者要求消费方能重载新值，默认关闭。

### 4.6 权限

| 动作 | 要求 |
|---|---|
| 查看策略 / 轮次 | 对该 secret 有 read |
| 创建 / 编辑 / 启用 / 暂停 / 删除策略 | 对该 secret 有 write |
| 标记固定 / 取消固定 | 对该 secret 有 write；必须填原因；审计 `rotation.fixed` / `rotation.unfixed` |
| 立即运行 / 完成 / 跳过 / 重新通知 | 对该 secret 有 write |
| 生成新值（`mode=generate`） | 对该 secret 有 write **且** reveal；生成响应会显示一次新值，没有 reveal 不能完成分发 |
| 确认分发（`awaiting_distribution` → `succeeded`） | 对该 secret 有 write |
| 更改负责人 | 对该 secret 有 write |
| 查看项目轮换页 | 对该项目有 read；列表按可读项目过滤 |
| 查看 worker 健康 | 组织成员；计数按组织过滤 |

原则：**write 是能力，owner 是责任。** owner 是默认执行人和 UI 上的“谁该处理”，不是硬门禁；负责人休假时，其他有 write 的人可以完成，但审计记录实际操作者。admin 可以改派负责人。

v1 不新增 `rotation:*` 权限；如果后续要更严格，再加组织级 `rotation_admin_only` 开关。外部轮换器用机器身份，至少需要 `read`（读取当前值）+ `write`（回写新版本）；webhook 事件本身不含明文。

### 4.7 生成规则（mode=generate）

先分清三种“真源”，否则“轮换 = 随机生成”会误导：

| 类型 | 谁是真源 | 能否由 taboo 生成 | 例子 |
|---|---|---|---|
| 存储级 | taboo 存的值就是消费方读的值 | 能：生成 + 写新版本；消费方重载 | 应用启动时从 taboo 拉取的共享密码/token；没有外部签发方的配置值 |
| 外部真源 | 数据库 / 云厂商 / OAuth / 第三方系统 | 不能凭空生成；要 provider adapter 或外部执行器 | DB 用户密码、AWS key、OAuth client secret |
| 签发级 | taboo 签发并能让旧凭据失效 | 能：生成 + 签发 + 吊销；v1 不做静态签发 | 动态数据库 lease（已有） |

**结论：** v1 的 `generate` 只对“存储级”开放。外部真源不是“随机一个字符串”就能换——数据库不认识它；必须走 manual/webhook，P2 再加 adapter。`generate` 也不等于轮换完成：生成只写新版本，分发和旧值退役仍要确认。

taboo 目前没有 secret 类型/外部真源元数据，不能自动判断属于哪一类；`mode` 必须由用户选择，默认 `manual`。UI 负责把三类讲清楚，不能假装系统知道。

#### 4.7.1 生成档位

默认 `alnum32`，服务端固定目录；不允许前端传任意正则。

| profile | 字符集 | 随机长度 | 熵 | 用途 |
|---|---|---|---|---|
| `alnum32`（默认） | `A-Z a-z 0-9`，去掉易混的 `I l O 0`（58 字符） | 32 | ≈188 bit | 通用 API key、共享密码 |
| `alnum64` | 同上 | 64 | ≈375 bit | 需要更长、无符号的兼容场景 |
| `base64url32` | `A-Z a-z 0-9 - _`（32 字节熵源） | 43 | 256 bit | URL / token |
| `hex32` | `0-9 a-f`（32 字节熵源） | 64 | 256 bit | 只接受 hex 的系统 |
| `password24` | 可打印 ASCII（去易混、不含空格），要求大写/小写/数字/符号 | 24–32 | ≥150 bit | 人工输入/兼容密码策略 |
| `custom` | 从服务端 allowlist 选字符集 | 16–128 | 服务端计算，≥128 bit | 兼容特殊格式；不接受任意 regex |

规则：

- **随机源**：只用 `crypto/rand`，禁止 `math/rand`、时间戳、UUID、计数器作为熵源；字符映射用拒绝采样，避免 modulo bias。
- **熵下限**：`length × log2(charset_size) ≥ 128 bit`；默认档位 ≥188 bit。低于 128 的请求直接拒绝，不允许“兼容旧系统”静默降级。
- **前缀**：可选，前缀不计入熵、不计入随机长度；总长度受 provider 限制。默认不加。
- **唯一性**：生成后与当前版本及最近 10 个版本的哈希比较；碰撞则重试最多 3 次，仍碰撞则失败（概率可忽略）。
- **不生成密钥对**：v1 只生成对称字符串；RSA/EC 密钥对需要算法、曲线、用途和证书链，单独立项。
- **不生成 provider 格式**：OAuth client secret、云厂商 key 的格式由 provider 决定，不能套 `alnum32`；由 adapter 按 provider 规则生成，或由 provider 自己生成。
- **内存/日志**：生成值只存在请求内存与加密版本中；审计只记 profile、长度、熵、版本、actor，不记值；日志/事件不含明文。Go 无法可靠 zeroize 字符串，做 best-effort 清理，不宣称“内存绝不留存”。

#### 4.7.2 生成、分发、退役

生成新值只完成第一步：

1. **生成**：服务端生成候选值，用现有 secret 写入路径写成新版本（加密、审计、outbox、sync 照常触发）。
2. **分发**：消费方（应用、CI、外部系统）需要拿到新值。taboo 只能通知和推送已有 sync 目标，不能保证所有消费方都更新。
3. **确认**：负责人确认所有消费方已切换后，点“完成分发”；run 才 `succeeded`。
4. **退役**：旧版本保留，标记“旧值仍可能有效”；taboo 不自动删除旧版本，也不自动吊销（它不是签发方）。P1 可加 `retire_after` 和旧版本标记。

因此 `mode=generate` 的完成状态是 `awaiting_distribution`，不是“生成完就成功”。`auto_generate=true` 只跳过“点生成”这一步，不跳过分发确认。

#### 4.7.3 什么时候生成

- 默认：到期创建 `awaiting_generation`，负责人点“生成新值”才改值。避免定时器在消费方没准备好时突然换值。
- `auto_generate=true`：到期由 worker 直接生成，进入 `awaiting_distribution`；适合“消费方从 taboo 实时读取并支持热重载”的场景。默认关闭，开启时 UI 必须提示风险。启用前检查负责人是否有 `reveal`；没有就阻止启用，或要求指派一个有 reveal 的负责人/值班人。
- 手动运行：同样先创建 `awaiting_generation`，不直接改值；要立即生成需再点一次确认。

### 4.8 手动更新与通知（fixed / 未设置）

`fixed` 不是“不能改”，而是“不安排轮换”。手动更新走现有 secret 写入路径：

1. 写入新版本（信封加密、审计、outbox）；
2. 发出现有 `secret.updated` 事件（Webhook 订阅方）；
3. 触发已有 Sync 目标；
4. **不创建轮换轮次，不改变 fixed 状态**。

这里要分清两种“通知”：

| 对象 | 能不能被通知 | 通知之后会怎样 |
|---|---|---|
| 应用 / CI（从 taboo 读值） | 能，`secret.updated` | 重新拉取或重载；缓存型应用需要显式失效 |
| 数据库 / 云厂商 / OAuth（外部真源） | **不能靠通知改变** | 它不会因为收到 Webhook 就改密码；必须有人或 adapter 去改上游 |
| 配置写死的消费方 | 只能通知到人 | 需要改配置/重新部署，不是热更新 |

所以 DB 密码的正确顺序是：

1. 先在数据库改密码（DBA 或 adapter）；
2. 再把新值写进 taboo；
3. 发 `secret.updated` 通知应用/CI 重载；
4. 验证应用能连上；
5. 旧密码按需要退役。

反过来先写 taboo、后改数据库，应用会拿到数据库不认识的密码。**“通知订阅方”和“更新权威系统”是两件事，不能混为一谈。**

对 fixed 手动更新的产品约束：

- 编辑表单顶部提示：“这是固定密钥；手动更新不会创建轮换轮次，也不会自动更新数据库/外部系统。请先确认上游已更新，并通知消费方。”
- **要求**填写 `reason`（复用现有 secret 写入的 reason 字段），审计记录这次手动变更；这不改变 fixed 状态。
- 不做“我已通知所有消费方”的强制勾选；只显示警告 + reason。消费方确认放 P1 的 `change_ack`。
- 更新成功后显示回执：“已写入 vN；已排队通知 X 个 Webhook；Sync 目标 Y 个。数据库/外部系统不会自动更新。”没有订阅方时明确说“没有订阅方，请手动通知”。
- 不显示“已通知数据库”这种不存在的承诺；数据库不是 Webhook 订阅方。
- 如果希望把“通知 + 消费方确认”也做成可追踪的闭环，后续可以引入轻量的 `change_ack`；v1 先用现有 Webhook 投递记录 + 审计，不新增表。

## 5. Walker 架构

### 5.1 进程角色

同一个二进制，三个角色：

| 角色 | HTTP | 后台循环 | 用途 |
|---|---|---|---|
| `all` | 有 | 有 | 单二进制 / SQLite 默认；兼容现有部署 |
| `serve` | 有 | 无 | PG 多副本 API；只服务请求 |
| `worker` | 可选健康端口 | 有 | PG 部署的后台进程；不对外服务业务 API |

命令建议：`taboo-server all`（默认）、`taboo-server serve`、`taboo-server worker`。SQLite 只支持 `all`；拆分角色在 SQLite 上拒绝启动并给出明确错误。

### 5.2 循环

`worker` 最终拥有全部后台循环；迁移分两期：

| 循环 | 频率 | 阶段 |
|---|---|---|
| rotation scheduler | 15s | P0 |
| rotation watchdog（逾期/心跳/清理） | 30s | P0 |
| webhook delivery | 5s | P0（随 worker 一起迁移） |
| outbox | 3s | P1 |
| sync | 10s | P1 |
| dynamic lease reclaim | 20s | P1 |
| audit checkpoint | 60s | P1 |
| expiry sweeps | 60s | P1 |

当前实现把全部后台循环放进 `worker`：rotation、webhook、outbox、sync、dynamic、audit；`serve` 不启动这些循环，只保留 HTTP/请求级服务和治理过期清扫。这样 API 与 worker 不会重复跑同一批循环。原计划 P0 只拆 rotation + webhook，P1 再搬其余；实际一并搬完，避免两套进程重复占用。`worker` 停机的窗口内，run 仍可由 API 手动操作，事件/投递在 worker 恢复后继续。

### 5.3 认领事务

```text
BEGIN
  SELECT ... FROM rotation_policies
   WHERE status='active' AND next_run_at <= now
   ORDER BY next_run_at LIMIT 64
   FOR UPDATE SKIP LOCKED            -- SQLite 用 BEGIN IMMEDIATE + 条件更新
  for each policy:
    if open run exists:
      advance next_run_at
      audit coalesced
      continue
    INSERT rotation_runs(run_key = policy_id + due_at, ...)
      ON CONFLICT DO NOTHING
    INSERT outbox event (run_id, old_version, due_at, grace_until, ...)
    advance next_run_at to next > now
    audit run.created
COMMIT
```

要点：

- 唯一索引是最终幂等保证；租约/行锁只是减少竞争。
- run 与 outbox 同事务；事件投递失败不影响 run 存在。
- 崩溃回滚后，下一次扫描重试；不会产生半个轮次。
- 两个 worker 并发：一个插入成功，另一个 `ON CONFLICT` 后只推进时间。

### 5.4 追赶、合并与重试

- **追赶**：只补一轮，`due_at` 用原定时间；审计记录 `missed_count`。不按停机时长补发 N 条。
- **合并**：有 open run 时不再创建；推进时间，审计 `coalesced`。完成 open run 后，下一次按新时间触发。
- **重试**：Webhook 投递沿用现有 6 次指数退避；轮次本身不自动重试完成。外部失败由执行器调用 `fail`，或宽限期后转逾期。
- **手动重试**：`POST /runs/{id}/notify` 重新发一次事件；不改变 run 状态。
- **失败重跑**：`POST /runs/{id}/retry` 新建 run，`trigger=retry`，链接前一条；不覆盖历史。
- **自动生成**：`auto_generate=true` 时，worker 在创建 run 后调用生成函数；生成、写新版本、run 状态更新在同一事务里提交。提交成功即幂等，重复执行返回 `ROTATION_ALREADY_GENERATED`；提交失败回滚，run 保持 `awaiting_generation`，下次扫描重试。

### 5.5 健康与可观测

- worker 每 10 秒写心跳；UI 判定：<30s 在线，30–90s 变慢，>90s 离线。
- `GET /api/v1/orgs/{slug}/rotation/health` 返回：
  - `online`、`last_seen_at`、`worker_version`、`last_sweep_at`；
  - `pending_due`、`overdue_runs`、`failed_runs_24h`（按组织过滤）；
  - `next_sweep_at`（估算：`last_sweep_at + 15s`）。
- 日志字段：`worker=rotation`、`policy_id`、`run_id`、`due_at`、`outcome`；不含明文。
- 指标（可选，P1）：`rotation_due_total`、`rotation_runs_created_total`、`rotation_runs_overdue`、`rotation_worker_heartbeat_age_seconds`。
- 优雅退出：停止认领、释放租约、写完心跳后退出；K8s `terminationGracePeriodSeconds` ≥ 30。

### 5.6 部署形态

- **SQLite / 单二进制**：`all`；不需要额外进程。
- **PostgreSQL / Docker Compose**：`api` 服务跑 `serve`，新增 `worker` 服务跑 `worker`；两者共用 `TABOO_MASTER_KEY`、`TABOO_JWT_SECRET`、`TABOO_DATABASE_URL`。
- **Kubernetes / Helm**：API Deployment + Worker Deployment；worker 副本 1–2，靠租约防重；worker 不暴露 Service，只开健康端口给 probe。
- **迁移切换**：schema 变更会触发仓库的版本守卫，旧二进制不能连接更高版本；不能“先迁库、再跑旧服务”。建议维护窗口：备份 → 停旧副本 → 新二进制以 `serve` + `worker` 启动并执行迁移 → 验证心跳/run。滚动升级只在迁移脚本纯加法、旧副本只写旧列时考虑，且必须在切换前停掉旧 rotation 循环。
- **回滚**：goose 只向前，没有自动 down。回滚 = 恢复迁移前备份 + 旧二进制，或保留新二进制。run 数据在备份/新库中保留。

## 6. 执行契约

### 6.1 事件

- 事件名保持 `secret.rotation_due`（兼容现有订阅），新增 `secret.rotation_overdue`、`secret.rotation_succeeded`、`secret.rotation_failed`、`secret.rotation_skipped`。manual 模式建议也发 `secret.rotation_due`，事件里 `mode=manual`，消费方不得据此自动执行。
- fixed / 未设置的手动更新不产生轮换事件，走现有 `secret.updated`；两者不要混淆。

payload（示意）：

```json
{
  "id": "evt_...",
  "type": "secret.rotation_due",
  "timestamp": "2026-12-03T01:00:00Z",
  "org": "acme",
  "project": "prod",
  "folder": "/",
  "key": "DB_PASSWORD",
  "version": 3,
  "actor": { "id": "system", "name": "rotation", "kind": "system" },
  "rotation": {
    "policy_id": "rot_...",
    "run_id": "run_...",
    "mode": "webhook",
    "due_at": 1796000000,
    "grace_until": 1796604800,
    "old_version": 3,
    "attempt": 1
  }
}
```

- 不含明文；`version` 是到期时的当前版本，`old_version` 显式给出。
- 签名沿用 `X-Taboo-Signature: t=<unix>,v1=<hex(hmac)>`；新增建议：接收方拒绝时间戳偏差超过 5 分钟的事件，防重放。
- 外部执行器需要读取当前值时，用机器身份走现有 reveal API；事件只是触发器，不是凭据载体。

### 6.2 生成路径（mode=generate）

1. 到期创建 `awaiting_generation`；负责人点“生成新值”，或 `auto_generate=true` 由 worker 直接执行。
2. 服务端校验 run 状态与权限（write + reveal），按 4.7 的档位生成候选值；与当前/最近 10 个版本哈希比较；用现有 secret 写入路径写成新版本（`expectedVersion = old_version`）。如果版本已前进，返回 `ROTATION_VERSION_STALE`，run 保持 `awaiting_generation`，要求人工检查，不要覆盖。
3. 更新 run：`new_version`、`status=awaiting_distribution`，`evidence` 记 profile/length/entropy；审计 `rotation.run.generated`，不记值。
4. 响应把新值显示一次；之后按 reveal 权限从版本历史查看。生成值已经加密落库，所以“只显示一次”是 UX 强调，不是安全保证。
5. 现有 `secret.updated` 事件与 Sync 目标会照常触发；这能覆盖部分消费方，但不能保证数据库/云厂商等外部系统已更新。
6. 负责人更新所有消费方后，调 `POST .../runs/{run_id}/distribute`，body `{ "note": "...", "verified": true }`；服务端校验 `new_version === secret.latest_version` 后置 `succeeded`，审计 `rotation.run.distributed`。
7. 如果生成后、确认前又有人写了新版本，`distribute` 返回 `ROTATION_VERSION_STALE`；不要静默完成，要求人工检查。
8. 生成失败（CSPRNG 错误、写入失败）不产生版本；run 保持 `awaiting_generation`，可重试。
9. 旧版本保留，不自动删除、不自动吊销；taboo 不是签发方时也无法吊销。`awaiting_distribution` 超过宽限期同样转 `overdue`。

### 6.3 外部回写

外部执行器顺序：

1. 验签、校验时间戳；
2. 在上游系统轮换凭据；
3. 用 `PUT /api/v1/projects/{pid}/secrets/{key}?path=...` 写入新版本，带 `expectedVersion`；
4. 调 `POST /api/v1/projects/{pid}/secrets/{key}/rotation/runs/{run_id}/complete`，body：`{ "new_version": 4, "note": "...", "verified": true }`；
5. 上游失败则调 `POST .../runs/{run_id}/fail`，body：`{ "reason": "..." }`。

完成校验：

- run 必须是 open 状态；
- `new_version > old_version`；
- `new_version === secret.latest_version`；如果之后又有人写了新版本，返回 `ROTATION_VERSION_STALE`，要求人工检查；
- 幂等：同一版本重复完成返回 200；不同版本返回 409。

### 6.4 人工完成

1. 负责人在上游系统换密钥；
2. 在 taboo 写入新版本；
3. 打开轮次，选“完成本轮”；
4. 选择新版本（默认最新），勾选“已确认新凭据在目标系统可用”，可填备注；
5. 服务端校验版本后标记 `succeeded`，审计记录操作者、版本、备注、验证方式。

taboo 不自动探测上游是否真的生效；`verified` 是人的声明，不是系统证明。UI 文案必须说清楚这一点。

### 6.5 逾期与失败

- 超过 `grace_until` 仍未完成：转 `overdue`，发一次 `secret.rotation_overdue`，项目页/密钥页显示逾期。
- 逾期不会自动停用策略；下一条到期到来时，如果仍有 open run，合并并推进时间，不堆叠。
- 外部明确失败：`failed`，发 `secret.rotation_failed`，负责人收到待办；重试会新建 run。
- 跳过：必须填原因；状态 `skipped`，发 `secret.rotation_skipped`；下一轮照常。
- 策略删除：open run 转 `canceled`（原因 `policy_deleted`），历史保留；不再产生新 run。

### 6.6 安全

- 事件、审计、日志都不含明文。
- 生成路径只允许服务端 `crypto/rand`；生成响应含一次新值，必须 `Cache-Control: no-store`，且要求 `write + reveal`。
- 生成审计记 profile/length/entropy/version/actor，不记值；日志和事件同样不记值。
- Webhook 只允许 HTTPS；沿用现有 SSRF/重定向限制。
- 机器身份最小权限：`read` + `write`；能用 IP 白名单就用。
- “立即运行”限流：每策略每分钟最多 1 次，防止误触/滥用。
- 完成/跳过/失败都写审计；审计包含 run_id、old/new version、actor、reason，不含明文。

## 7. API 草案

路径沿用当前 `/api/v1/projects/{pid}/secrets/{key}/rotation`，查询参数 `path` 保留。

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `.../rotation` | 策略 + 最近轮次摘要 |
| PUT | `.../rotation` | 保存策略；body 带 `status` |
| DELETE | `.../rotation` | 删除策略（历史保留） |
| POST | `.../rotation/preview` | 预览未来 3 次；不写库 |
| POST | `.../rotation/run` | 立即运行一轮 |
| GET | `.../rotation/runs?limit=&cursor=` | 轮次列表 |
| GET | `.../rotation/runs/{run_id}` | 轮次详情 + 投递记录 |
| POST | `.../rotation/runs/{run_id}/generate` | `mode=generate`：生成新值并写新版本 |
| POST | `.../rotation/runs/{run_id}/distribute` | `mode=generate`：确认分发完成 |
| POST | `.../rotation/runs/{run_id}/complete` | `manual` / `webhook`：完成本轮 |
| POST | `.../rotation/runs/{run_id}/fail` | 外部报告失败 |
| POST | `.../rotation/runs/{run_id}/skip` | 跳过本轮（必须给原因） |
| POST | `.../rotation/runs/{run_id}/notify` | 重新通知 |
| POST | `.../rotation/runs/{run_id}/retry` | 失败后新建一轮 |
| GET | `/api/v1/generation-profiles` | 生成档位目录与熵 |
| GET | `/api/v1/projects/{pid}/rotations` | 项目轮换列表 |
| GET | `/api/v1/orgs/{slug}/rotation/health` | worker 健康与计数 |
| GET | `/api/v1/orgs/{slug}/rotations` | 组织总览（P1） |

### 7.1 策略响应

```json
{
  "id": "rot_...",
  "key": "DB_PASSWORD",
  "folder": "/",
  "status": "active",
  "enabled": true,
  "mode": "generate",
  "generation": {
    "profile": "alnum32",
    "length": 32,
    "prefix": "",
    "min_entropy_bits": 188
  },
  "auto_generate": false,
  "interval_seconds": 2592000,
  "cron": "",
  "owner": { "id": "usr_...", "name": "alice" },
  "grace_seconds": 604800,
  "next_run_at": 1796000000,
  "last_run_at": 1793408000,
  "last_run_id": "run_...",
  "open_run": {
    "id": "run_...",
    "status": "awaiting_generation",
    "due_at": 1796000000
  },
  "created_at": "2026-11-01T00:00:00Z",
  "updated_at": "2026-11-01T00:00:00Z"
}
```

`enabled` 为兼容字段，等于 `status === 'active'`；新客户端用 `status`。`fixed` 响应没有 `next_run_at` / `open_run`，带 `fixed_reason`、`fixed_by`、`fixed_at`。

### 7.2 PUT 语义

```json
{
  "status": "active",
  "mode": "generate",
  "generation": {
    "profile": "alnum32",
    "length": 32,
    "prefix": ""
  },
  "auto_generate": false,
  "interval_seconds": 2592000,
  "cron": "",
  "owner_id": "usr_...",
  "grace_seconds": 604800
}
```

- `status` 必填：`draft` / `active` / `paused` / `fixed`。
- `fixed` 必须带 `fixed_reason`；不要求周期/owner，但建议保留 owner。`fixed` 会清空 `next_run_at`，取消 open run。
- `mode` 新请求必填：`generate` / `manual` / `webhook`；`status=fixed` 时不需要 mode。`generate` 必须带合法的 `generation`；其他模式忽略生成字段。旧客户端省略 `mode` 时默认 `manual`（兼容一个版本周期）。
- `active` 必须有周期和 owner；`generate` 的 owner 还必须有 `reveal`，否则返回 `ROTATION_REVEAL_REQUIRED`。`draft`/`paused`/`fixed` 不计算 `next_run_at`。
- 兼容：旧客户端只传 `enabled` 时，映射为 `active`/`paused` 一个版本周期；两者都不传返回 `ROTATION_STATUS_REQUIRED`。这改变了当前“省略即启用”的行为，必须写进变更说明。
- 编辑 active 策略的周期会重新计算下次；只改 owner/宽限期/生成档位不改变下次。

### 7.3 预览

`POST .../rotation/preview` body 同 PUT 的周期字段；返回：

```json
{
  "next": [
    "2026-12-03T01:00:00Z",
    "2027-01-02T01:00:00Z",
    "2027-02-01T01:00:00Z"
  ],
  "local": ["2026-12-03 09:00", "..."],
  "timezone": "Asia/Shanghai"
}
```

服务端计算，前端不重复实现 cron。

### 7.4 错误码

| code | 含义 |
|---|---|
| `ROTATION_STATUS_REQUIRED` | 新请求缺少 `status` |
| `ROTATION_SCHEDULE_REQUIRED` | active 但没有周期 |
| `ROTATION_OWNER_REQUIRED` | active 但没有负责人 |
| `ROTATION_ALREADY_OPEN` | 已有 open run，不能再手动运行 |
| `ROTATION_FIXED` | 策略是 `fixed`，不能运行/生成 |
| `ROTATION_RUN_NOT_OPEN` | 轮次已完成/跳过/失败 |
| `ROTATION_VERSION_STALE` | 新版本不是当前版本 |
| `ROTATION_VERSION_MISSING` | 指定的新版本不存在 |
| `ROTATION_REASON_REQUIRED` | 跳过/失败没有原因 |
| `ROTATION_GENERATION_PROFILE_INVALID` | 生成档位/长度不合法，或熵低于 128 bit |
| `ROTATION_REVEAL_REQUIRED` | 没有 reveal，不能生成/显示新值 |
| `ROTATION_ALREADY_GENERATED` | 本轮已经生成过新版本 |
| `ROTATION_DISTRIBUTION_REQUIRED` | `generate` 轮次必须先确认分发才能完成 |
| `ROTATION_FIXED_REASON_REQUIRED` | 标记 fixed 没有原因 |

## 8. UI/UX 设计

### 8.1 原则

1. **到期是待办，不是错误。** 用琥珀色，不用朱砂红；红色只给失败/删除。
2. **未设置是正常状态。** 空状态要教学，不伪造进度；fixed 是明确决定，不是缺口。
3. **草稿与启用是两种状态。** 不是一个预勾选的复选框；两个按钮说清楚。
4. **调度器要可见。** 时间是不可见的承诺，心跳条是承诺的凭证。
5. **轮换是第二条时间线。** 版本是“值的历史”，轮次是“责任的历史”；都用时间线，不混在一起。
6. **沿用现有词汇。** 墨/朱砂/结界、`section`/`table`/`pill`/`notice`/`drawer`；不发明第二套视觉。

### 8.2 信息架构

- 项目 rail 的「治理」组新增 **轮换**，位于「动态密钥」之后；图标用 `refresh`。**可见性按 read 判断，不要复用 `canDelegate`**；编辑/运行按 write 控制。
- 项目页顶部的密钥页头增加一个「轮换」入口（小屏底栏只有四格，不能加第五格）；密钥详情抽屉里也有入口。小屏点这个入口进入项目轮换页。
- 密钥详情抽屉增加「概览 / 轮换」两个页签；版本历史仍在「概览」里，保留原有“版本历史”标题与可访问名。
- 轮换页签内是：状态摘要 → 策略（只读或编辑）→ 轮次列表 → 选中轮次详情。
- 项目轮换页是总览与筛选；点行打开该密钥详情抽屉的「轮换」页签。
- 项目页的「新建轮换」打开一个密钥选择器（只列还没有策略、且当前用户有 write 的 secret，可搜索）；选中后打开该密钥详情的轮换页签，并进入编辑态。
- Webhook 设置的事件选择改成复选框列表，包含新的轮换事件；不再用逗号分隔的自由文本。

### 8.3 密钥详情：轮换页签

把当前 `RotationPolicy` 从版本抽屉底部移出，改成抽屉页签。默认「概览」；从项目轮换页进入时默认「轮换」。

```text
┌ 密钥详情 ──────────────────────────────────────────────┐
│ DB_PASSWORD                          acme / prod /    │
│ [概览] [轮换]                                      [×] │
├────────────────────────────────────────────────────────┤
│ 轮换  [运行中]                                         │
│ 每 30 天 · 下次 12-03 09:00 · 负责人 @alice            │
│ 方式 生成并写入（alnum32 · 188 bit）· 宽限期 7 天      │
│ [生成新值] [暂停]                                      │
│                                                        │
│ 策略                                                   │
│ ...                                                    │
│ 轮次                                                   │
│ 12-03 09:00  待生成    v3 → —  @alice       [查看]     │
│ 11-03 09:00  已完成    v2 → v3  @alice       [查看]     │
└────────────────────────────────────────────────────────┘
```

固定不轮换时：

```text
轮换  [固定]
由 @alice 于 11-13 标记 · 原因：厂商托管，无法轮换
没有下次轮换，不会提醒、不会生成新值；值仍可手动更新。
[改为轮换]
```

未设置时：

```text
轮换
未设置轮换。没有到期时间，也没有提醒。
轮换是可选的责任分配；可以设置周期，或明确标记为固定不轮换。
[设置轮换] [标记固定]
```

要点：

- 页签打开时并行加载策略与最近 10 条轮次；骨架屏。
- 只读用户看到摘要与轮次，没有编辑/运行/固定按钮；用一句“需要 write 权限”解释。
- 负责人已离组时，摘要显示琥珀色“负责人已离组，请重新指派”，admin/write 可改派。
- 没有 Webhook 订阅时，若 `mode=webhook`，显示“没有订阅者，外部轮换器不会收到通知”，链接到 Webhook 设置；不阻止保存。
- `mode=generate` 的摘要显示档位与熵；有 open run 时，主按钮随状态切换为 `生成新值`（待生成）或 `完成分发`（待分发）。没有 reveal 时按钮禁用并说明。
- `fixed` 摘要不显示“下次”，不显示倒计时/催办；只显示固定原因、标记人和时间。重新改为轮换时需要重新填周期/负责人。

### 8.4 项目轮换页

```text
┌ 轮换 ───────────────────────────────────────── [新建轮换] ┐
│ 12 条策略 · 2 条待处理 · 另有 4 条未设置                  │
├───────────────────────────────────────────────────────────┤
│ ● 调度器运行中 · 最近心跳 12s 前 · 下次扫描 15s 内        │
├───────────────────────────────────────────────────────────┤
│ 运行中 8     草稿 2     已暂停 0     固定 2                │
│ 7 天内到期 2 · 已逾期 1 · 待处理 2                        │
├───────────────────────────────────────────────────────────┤
│ [状态 v] [负责人 v] [方式 v]              搜索 key…        │
│ KEY             状态      方式   周期      下次     负责人│
│ DB_PASSWORD     运行中    生成   每 30 天  12-03    @alice│
│ STRIPE_KEY      已逾期    外部   每 90 天  11-28    @bob  │
│ JWT_SECRET      草稿      人工   每 180 天 —        @alice│
│ VENDOR_KEY      固定      —      —         —        @alice│
└───────────────────────────────────────────────────────────┘
```

- 统计分两行：第一行是互斥的策略状态（运行中/草稿/已暂停/固定，合计 = 有策略的密钥数）；第二行是关注项（7 天内到期/已逾期/待处理），可与状态重叠。不要把它们加成一个总数。
- “未设置”是没有任何策略、也没有标记固定的密钥数；它不是错误，但提醒还有覆盖缺口。fixed 不计入“未设置”。
- 默认排序：逾期优先，然后 `next_run_at` 升序，固定/未设置沉底，再按 key。
- 筛选：状态（全部/待处理/已逾期/草稿/已暂停/固定/未设置）、负责人、方式；搜索 key/目录。
- 行点击打开密钥详情抽屉的「轮换」页签；行内「查看轮次」直接定位到轮次区。
- 空状态：“还没有轮换策略。轮换是 opt-in：为密钥设置周期和负责人后，taboo 才会在到期时提醒；不想轮换的密钥可以明确标记为固定。”按钮“新建轮换”。
- fixed 行不显示“下次”，不要用倒计时/催办；只在状态列显示“固定”。
- 无权限：只读列表，不显示新建/编辑；说明需要 write。

### 8.5 编辑表单

表单就在「轮换」页签内，用 view/edit 两态切换；不弹第二个对话框。

```text
┌ 策略 ───────────────────────────────────────────────────┐
│ 方式   (•) 生成并写入   ( ) 人工轮换   ( ) 外部 Webhook   │
│        由 taboo 生成随机值并写新版本；消费方重载后确认分发。│
│                                                          │
│ 生成   [alnum32 v]  [32] 位   前缀 [        ]             │
│        熵 188 bit · 字符集 A-Z a-z 0-9（去 I l O 0）      │
│        ☐ 到期自动生成（默认关闭；消费方必须支持热重载）     │
│                                                          │
│ 周期   [固定间隔 v]  [30] 天                              │
│        下次 12-03 09:00 · 01-02 09:00 · 02-01 09:00      │
│        最小 5 分钟；cron 为五段 UTC。                     │
│                                                          │
│ 负责人 [alice v]                                         │
│ 宽限期 [7 天 v]                                          │
│ 通知   ☑ 站内待办    ☑ Webhook（2 个订阅）                │
│                                                          │
│ [保存草稿]                              [保存并启用]      │
└──────────────────────────────────────────────────────────┘
```

字段规则：

- 方式：`generate` / `manual` / `webhook` 单选；不支持的模式 disabled，不假装可用。选择 `generate` 时提示“仅适用于 taboo 作为真源的值；外部系统也持有同一个值时，生成后仍需在外部更新”。
- 生成（仅 `generate`）：档位下拉、随机长度、可选前缀；实时显示熵；低于 128 bit 阻止保存。没有 `reveal` 权限时禁用生成相关控件，并说明原因。
- 自动生成（仅 `generate`）：`auto_generate` 默认关闭；开启时在字段下方明确“到期会直接写入新版本，消费方必须能重载；否则可能读到未准备好的值”。负责人没有 reveal 时不能启用自动生成。
- 周期：固定间隔（分钟/小时/天，最小 5 分钟，最大 366 天）或 cron（五段，UTC）；下方显示未来 3 次，服务器计算。
- 负责人：组织成员下拉；默认当前用户；没有可选成员时阻止启用并说明。
- 宽限期：1/3/7/14/30 天；默认 7 天。
- 通知：站内待办固定开启；Webhook 显示当前订阅数；0 个时给警告和设置链接，不阻止保存。
- 按钮：
  - 新建/草稿：`保存草稿`（quiet） + `保存并启用`（stamp） + `标记固定`（quiet）。
  - active：`保存`（stamp） + `暂停`（quiet） + `标记固定`（quiet） + `删除`（danger，需二次确认）。
  - paused：`保存` + `恢复` + `标记固定` + `删除`。
  - fixed：`改为轮换`（default） + `删除`；改为轮换时回到编辑态，必须重新填周期/负责人。
- `标记固定` 不弹第二个对话框：在同一页签内切到一个小表单，必填“固定原因”，可选负责人；确认后清空 `next_run_at`、取消 open run，状态转 `fixed`。文案要说明“不是不可写，只是不安排轮换；不会再有到期/提醒”。
- 错误：字段下方 inline，`role=alert`，聚焦第一个错误；不关闭表单、不清空输入。
- 保存成功：`role=status` “已保存草稿” / “已启用，下次 …”；页签摘要同步更新。

### 8.6 轮次列表与详情

轮次列表用紧凑表格，不做卡片：

| 到期 | 状态 | 版本 | 负责人 | 触发 | 操作 |
|---|---|---|---|---|---|
| 12-03 09:00 | 等待回写 | v3 → — | @alice | 定时 | 查看 |
| 11-03 09:00 | 已完成 | v2 → v3 | @alice | 定时 | 查看 |
| 10-28 09:00 | 已跳过 | v2 → — | @bob | 手动 | 查看 |

点“查看”在列表下方展开详情（不叠加对话框）：

```text
┌ 轮次 run_... ─────────────────────────────── [返回轮次] ┐
│ [等待回写]   定时触发 · 到期 12-03 09:00               │
│ 旧版本 v3 · 新版本 — · 负责人 @alice · 投递 1/6          │
│                                                         │
│ ● 12-03 09:00  轮次创建                                 │
│ ● 12-03 09:00  已通知 Webhook（投递 whd_...）            │
│ ○ 等待外部轮换器回写                                    │
│                                                         │
│ [重新通知]  [标记完成]  [跳过本轮]                       │
└─────────────────────────────────────────────────────────┘
```

- 时间线用现有 `.timeline` 节点；当前节点朱砂，完成节点玉色，失败节点朱砂。
- 完成表单：新版本下拉（默认最新）、备注、勾选“已确认新凭据在目标系统可用”；提交按钮 `完成本轮`（stamp）。
- 跳过：必填原因；按钮 `跳过本轮`（danger/quiet 的二次确认）。
- 外部模式：显示“等待外部轮换器回写”，提供“重新通知”；仍允许人工完成（带说明）。
- 失败：显示原因、尝试次数、下一次重试时间；提供“重试”（新建一轮）。
- Webhook 投递记录：小表（时间/状态/HTTP/尝试/下次），最多 6 次；不展示 payload 中的敏感字段（本来也没有）。

`mode=generate` 的轮次不是“完成”，而是“生成 → 分发”：

```text
┌ 轮次 run_... ─────────────────────────────── [返回轮次] ┐
│ [待分发]   定时触发 · 到期 12-03 09:00                 │
│ 旧版本 v3 · 新版本 v4 · 负责人 @alice                 │
│                                                         │
│ ● 12-03 09:00  轮次创建                                 │
│ ● 12-03 09:01  已生成 v4 · alnum32 · 188 bit            │
│ ○ 等待确认分发                                          │
│                                                         │
│ 新值（仅此屏显示一次）              [复制] [隐藏]        │
│ taboo_7fK...                                            │
│ ☐ 我已更新所有消费方                                    │
│ [完成分发]  [跳过本轮]                                  │
└─────────────────────────────────────────────────────────┘
```

- `awaiting_generation`：按钮 `生成新值`（stamp）+ `跳过本轮`；没有 reveal 权限时禁用并说明。
- `awaiting_distribution`：显示密封的新值块；勾选“我已更新所有消费方”后才允许 `完成分发`（stamp）。
- 新值同时已加密落库；这里“仅此屏显示一次”是提醒，不是一次性销毁；之后按 reveal 权限查看。
- 自动生成：到期直接进入 `awaiting_distribution`，并通知负责人；仍需确认分发，不能自动 `succeeded`。
- 旧版本保留，显示“旧值仍可能有效”；不自动删除、不宣称已吊销。

### 8.7 状态与文案

策略：

| status | Pill | 文案 | 动作 |
|---|---|---|---|
| draft | muted `草稿` | 尚未启用，不会提醒 | 编辑、启用、删除 |
| active | ok `运行中` | 按周期提醒 | 编辑、暂停、立即运行、删除 |
| paused | muted `已暂停` | 暂停期间不会提醒 | 编辑、恢复、删除 |
| fixed | muted `固定` | 明确不轮换，没有到期/提醒 | 改为轮换、删除 |
| active + overdue | ok `运行中` + warn `有逾期` | 运行中，但有轮次逾期 | 查看轮次 |

“未设置”不是策略状态：没有策略行，没有 `next_run_at`，同样没有到期/提醒；它只是没做决定，和 `fixed` 分开显示。

轮次：

| status | Pill | 文案 |
|---|---|---|
| due | warn `待处理` | 已到期，等待通知 |
| awaiting_generation | warn `待生成` | 等待负责人生成新值 |
| awaiting_distribution | warn `待分发` | 新值已生成，等待消费方更新 |
| awaiting_manual | warn `待人工` | 等待负责人完成 |
| awaiting_external | warn `等待回写` | 已通知外部轮换器 |
| overdue | warn `已逾期` | 超过宽限期，仍未完成 |
| succeeded | ok `已完成` | 新版本已写入；生成模式已确认分发 |
| failed | bad `失败` | 外部报告失败 |
| skipped | muted `已跳过` | 本轮被跳过，原因已记录 |
| canceled | muted `已取消` | 策略删除/人工取消 |

关键文案：

- “固定或未设置都不会有到期时间，也不会因为时间失效；固定是明确决定，未设置是还没决定。”
- “固定不是不可写：手动更新仍会产生新版本。”
- “taboo 只在存储级轮换时生成新值；数据库/云厂商等外部真源仍由外部执行器更换。”
- “生成只写入新版本；确认所有消费方已切换后，本轮才算完成。”
- “旧版本不会自动删除，也不会自动吊销。”
- “保存草稿不会开始调度。”
- “启用后，每 30 天生成一次待办，并通知负责人/Webhook。”
- “人工轮换完成前，请先在外部系统更换凭据，并把新值写入 taboo。”
- “逾期不会停用策略；它会置顶，直到本轮完成或被跳过。”
- “调度器离线，到期轮换不会触发；已到期的轮次仍可手动完成。”

### 8.8 空、加载、错误、权限、离线

| 状态 | 表现 |
|---|---|
| 无策略 | 教学空状态 + “设置轮换” / “标记固定”；不预填、不预勾选 |
| 固定 | 状态 `固定`；不显示“下次”、不做倒计时/催办；提供“改为轮换” |
| 未设置 | 显示“未设置”，不是错误；提供“设置轮换” / “标记固定” |
| 加载 | `.skeleton` 2–4 行；不转圈 |
| 保存中 | 按钮 loading，禁用重复提交；不关闭页签 |
| 保存失败 | inline `role=alert`；保留输入；焦点到第一个错误 |
| 无 write | 只读摘要 + “需要 write 权限才能修改轮换” |
| 无 reveal | 生成控件禁用 + “没有 reveal 权限，不能生成或显示新值”；可跳过或请负责人处理 |
| 生成失败 | inline `role=alert`；轮次保持 `awaiting_generation`；可重试；不产生半写版本 |
| 已生成未分发 | 琥珀条：“新值已写入，旧值仍可能有效；确认所有消费方切换后完成分发。” |
| 无 Webhook | 琥珀警告 + 去设置；不阻止保存 |
| 负责人离组 | 琥珀警告 + 改派入口（admin/write） |
| 调度器变慢 | 琥珀条：“心跳 45 秒未更新，轮换可能延迟。” |
| 调度器离线 | 朱砂条：“调度器离线。到期轮换不会触发；已到期轮次仍可手动完成。” |
| 轮次逾期 | 项目页统计 + 列表置顶 + 详情琥珀状态；不弹 toast |
| 轮次失败 | 列表红状态 + 失败原因；提供重试 |

### 8.9 响应式与无障碍

- ≤760px：rail 变底部四格；轮换入口放在密钥页头与密钥详情，不占第五格；轮换页全宽。
- ≤640px：表格变 `data-mobile="cards"`：key + 状态 / 周期 + 下次 / 负责人 + 操作；筛选换行；统计带两列。
- 密钥详情抽屉 ≤760px 全宽；页签横向滚动，不换行；表单行堆叠。
- 390px：`scrollWidth ≤ innerWidth`；cron、key、run id 用等宽并可换行；不裁切标识符。
- 页签：真实 `role=tablist`/`tab`/`tabpanel`，左右方向键切换；`aria-selected`；焦点可见。
- 状态：文字 + 颜色双编码；pill 有 accessible name。
- 表单：`fieldset`/`legend` 包住方式选择；所有输入有 label；错误 `role=alert`；首个错误聚焦。
- 对话：原生 `<dialog>` 焦点捕获、Escape、焦点归还；页签切换不丢焦点。
- 计数徽标：`aria-label="3 条轮换待处理"`；0 时不渲染。
- 时间：显示本地，`title` 给完整时区/UTC；cron 输入旁明确“UTC”。
- 动效：复用 `drawer-in`/`modal-in`/`skeleton`；状态变化不加入场编排；`prefers-reduced-motion` 折叠。

### 8.10 不做什么

- 不给 `fixed` / 未设置显示“下次轮换”、到期时间或倒计时。
- 不把 `fixed` 当成“未配置”或覆盖率缺口；它是明确决定。
- 不把 `fixed` 说成“不可写/已锁定”；需要不可写时另做 `locked`，本文不做。
- 不把“提醒”写成“自动轮换”。
- 不把“生成新值”写成“轮换完成”；生成和分发是两步。
- 不在消费方未确认时自动完成分发；`auto_generate` 也不跳过确认。
- 不自动删除旧版本，也不宣称“已吊销”taboo 不能吊销的凭据。
- 不在无策略时预勾“启用”。
- 不把完整表单塞回版本历史底部。
- 不嵌套第二层对话框；轮换详情在同一个抽屉内完成。
- 不用绿色/“一切正常”掩盖 worker 离线。
- 不展示伪造的覆盖率/成功率；没有数据就显示“—”。
- 不自动把“检测到新版本”当成完成；只提示，需人确认。

### 8.11 固定密钥的手动更新

固定密钥仍然可以手动更新；入口就是现有的“编辑”按钮，不经过轮换轮次。

编辑表单在 fixed 密钥上多一条提示：

```text
┌ 编辑密钥 · DB_PASSWORD ───────────────────────────────┐
│ [固定] 这条密钥不安排轮换。                            │
│ 手动更新不会自动修改数据库/外部系统，也不会创建轮次。   │
│ 请先在上游更新，再通知消费方重载。                     │
│                                                       │
│ 新值  [••••••••••••••••]                              │
│ 原因  [已由 DBA 在数据库改密，通知 app-team]           │
│                                                       │
│ [取消]                              [保存（产生新版本）]│
└───────────────────────────────────────────────────────┘
```

保存成功后显示回执：

```text
已写入 v5 · 已排队通知 2 个 Webhook · Sync 目标 1 个
数据库/外部系统不会自动更新；请确认消费方已重载。
[查看投递记录]
```

规则：

- 固定密钥手动更新时，`reason` 必填；审计记录 reason、版本、操作者，不记值。
- 不要求勾选“我已通知所有消费方”；只显示警告 + reason，避免形式主义；P1 再评估 `change_ack`。
- 更新不会改变 `fixed` 状态；只是版本前进。
- 有 Webhook / Sync 时显示“已排队/已触发”，但**不承诺消费方已应用**；投递成功 ≠ 消费方重载成功。
- 没有 Webhook / Sync 时显示“没有订阅方，请手动通知消费方”，不假装通知了谁。
- 不出现“已通知数据库”“已更新数据库”之类文案；数据库不是订阅方。
- 如果之后要做消费方确认，再加 `change_ack`；v1 用投递记录 + 审计。

## 9. 迁移与兼容

### 9.1 Schema 变更（下一版迁移）

实现已把迁移版本从 PG 13 / SQLite 10 推进到 PG 14 / SQLite 11；ROADMAP 里的 schema 17 是旧一代部署记录，不沿用这个编号。对应脚本是 `pg/00014_rotation_runs.sql` / `sqlite/00011_rotation_runs.sql`。

- `rotation_policies`：新增 `mode`、`generation_profile`、`generation_length`、`generation_prefix`、`auto_generate`、`owner_user_id`、`grace_seconds`、`status`、`fixed_reason`、`fixed_by`、`fixed_at`、`version`；`next_run_at` 允许 NULL；保留 `enabled` 一个版本周期。
- 新表 `rotation_runs`、`worker_heartbeats`。`rotation_runs` 有 `org_id`，PG 上 ENABLE + FORCE RLS；`worker_heartbeats` 是全局基础设施表，不含租户数据，不加 RLS，只给运行角色读写。
- 索引：`rotation_runs(policy_id, due_at)`、`rotation_runs(status, grace_until)`、`rotation_policies(status, next_run_at)`。
- 外键：`policy_id` / `secret_id` 用 `ON DELETE SET NULL`，保留 key/folder/policy 快照；策略删除不级联删除历史。
- 旧二进制在 `migrateSchema` 里有版本守卫，会拒绝更高版本；不能“先迁库、再跑旧服务”。

### 9.2 回填

- `enabled=1` → `status='active'`；`enabled=0` → `status='paused'`；`next_run_at` 保留。
- 没有策略的 secret 保持“未设置”，**不自动回填成 `fixed`**；不能替用户猜测“他不想轮换”。
- `fixed_reason` / `fixed_by` / `fixed_at` 只对新标记的 fixed 写入。
- `owner_user_id`：优先 `created_by`；否则项目 owner/admin；都没有则 active 策略标记“负责人缺失”，UI 提示改派。
- `mode`：默认 `manual`（保守；不改变现有行为——现有策略本来只发事件，没有执行器；如果已有 Webhook 订阅，管理员可改 `webhook`；`generate` 需要用户显式选择档位）。
- 现有策略的 `generation_*` 为空、`auto_generate=0`；不会因为升级突然开始生成值。
- 历史事件不补 run；从升级后的下一次到期开始记录。
- 按仓库现行的 pre-1.0 策略，开发环境可以直接删库重建；若要保留现有策略，则用下一版迁移做一次回填，并在测试库验证。

### 9.3 API 兼容

- GET 继续返回 `enabled`；新增 `status`、`mode`、`generation`、`auto_generate`、`owner`、`grace_seconds`、`open_run`；`fixed` 带 `fixed_reason`/`fixed_by`/`fixed_at`。`manual`/`webhook` 的 `generation` 为 null。
- PUT 新客户端必须带 `status` 和 `mode`；旧客户端省略 `mode` 时默认 `manual`，省略 `status` 只带 `enabled` 时映射一个版本周期；两者都不带返回 400。
- 事件名 `secret.rotation_due` 不变，新增字段向后兼容；新增事件类型只影响主动订阅方。
- OpenAPI 的 webhook events 枚举补齐全部事件；这是文档修复，不改变代码行为。

### 9.4 部署切换

1. 备份数据库（PG dump / SQLite 文件 + `master.key`）。
2. 维护窗口内停旧 API；不要在新 schema 上启动旧二进制。
3. 新二进制以 `serve` + `worker` 启动，由第一个实例执行迁移；如果要独立迁移 Job，需要新增 `taboo-server schema up`（当前只有只读 `schema`），落地前不要设置 `TABOO_AUTO_MIGRATE=0` 而没有人执行迁移。
4. 验证 `taboo-server schema` / `--check`、worker 心跳、run 创建、Webhook 投递。
5. 回滚：恢复备份 + 旧二进制，或保留新二进制。goose 只向前，没有自动 down。

如果必须滚动升级：迁移脚本只能做加法，旧副本只能写旧列，且旧 rotation 循环必须在迁移前停掉；否则旧事件与新 run 会并存。pre-1.0 阶段建议直接用维护窗口，不赌混合版本。

### 9.5 数据保留

- run 保留 1 年（可配置）；过期 run 由 worker 清理，审计保留按组织审计策略。
- 策略删除后 run 保留；`policy_id` / `secret_id` 置空，key/folder/policy 快照仍可读。
- 心跳保留 24 小时。

## 10. 验收标准

- [ ] 无策略：GET 404；不产生 run；UI 显示“未设置”，无预勾选、无倒计时。
- [ ] 固定：`status=fixed`、`next_run_at=null`；worker 不扫描、不产生 run/提醒；必须填原因；审计 `rotation.fixed`。
- [ ] 固定转入：active/paused/draft 标记 fixed 时，open run 取消；fixed 不计入“未设置”覆盖率缺口。
- [ ] 固定手动更新：写新版本、发 `secret.updated`、触发 Sync；不创建 run；fixed 状态不变；reason 必填；审计不含值。
- [ ] 固定手动更新回执：显示 Webhook 排队数/Sync 目标数；没有订阅方时明确“请手动通知”；不出现“已通知数据库”。
- [ ] 固定手动更新只要求 `reason`，不要求勾选“已通知所有消费方”；警告文案可见。
- [ ] 改为轮换：fixed → active 必须重新填周期/负责人；没有 `next_run_at` 残留。
- [ ] 保存草稿：`status=draft`、`next_run_at=null`；worker 不认领；UI 显示“草稿”。
- [ ] 保存并启用：`status=active`、有 owner、`next_run_at` 为未来；UI 显示“运行中”。
- [ ] 到期：恰好一条 run；两个 worker 并发不重复；outbox 有事件。
- [ ] 追赶：worker 停机跨越 3 个周期，重启只补 1 条 run，审计 `missed_count=2`。
- [ ] 合并：已有 open run 时到期，不新建 run；推进时间并审计 `coalesced`。
- [ ] 到期默认不改值：`mode=generate` 创建 `awaiting_generation`，在点“生成新值”之前 secret 版本不变；`auto_generate=false` 时 worker 不会写版本。
- [ ] 生成：`POST /generate` 写入新版本，run `awaiting_distribution`；审计有 profile/length/entropy/version，日志/事件无明文。
- [ ] 生成权限：没有 reveal 的 write 用户不能生成/显示新值，返回 `ROTATION_REVEAL_REQUIRED`。
- [ ] 生成档位：低于 128 bit 的档位/长度被拒绝；`alnum32` 长度/字符集正确；碰撞重试逻辑可测。
- [ ] 分发：`new_version` 仍是当前版本时 `distribute` 成功；之后又有人写新版本则 409 `ROTATION_VERSION_STALE`。
- [ ] 自动生成：`auto_generate=true` 到期直接生成并进入 `awaiting_distribution`；仍需确认分发，不能自动成功。
- [ ] 旧版本：生成后旧版本仍保留，不自动删除/吊销；UI 显示“旧值仍可能有效”。
- [ ] 人工完成：写入 v4 后 complete，run `succeeded`；审计有 actor/versions/note。
- [ ] 版本不匹配：complete 传旧版本 → 409 `ROTATION_VERSION_STALE`。
- [ ] 外部完成：Webhook payload 有 run_id/old_version/due_at/grace_until；无明文；签名可验。
- [ ] 逾期：超过 grace，run `overdue`，发一次 `secret.rotation_overdue`；UI 置顶。
- [ ] 跳过：必须填原因；run `skipped`；下一轮照常。
- [ ] 失败：外部 fail → `failed`；可 retry 新建一轮。
- [ ] worker 离线：90 秒内项目页出现离线告警；恢复后心跳恢复。
- [ ] 权限：viewer 看不到编辑/运行；write 可以完成别人的轮次并留痕。
- [ ] UI 390px：无横向溢出；键盘可完成创建、启用、完成、跳过；页签/焦点正常。
- [ ] 审计：策略 CRUD、启用/暂停、run 创建/通知/生成/分发/完成/失败/跳过/逾期全部有记录；生成记录不含值。
- [ ] 迁移：现有迁移版本的 enabled 策略升级后仍 active；下次时间不丢；`mode=manual`、`generation_*` 为空、`auto_generate=0`。

## 11. 分期

### P0（本次重设计的 MVP）

- 下一版迁移、`rotation_runs`、`worker_heartbeats`。
- `worker` 角色 + rotation scheduler/watchdog + 心跳；API `serve` 角色。
- 策略状态/草稿/启用分离；`fixed`（明确不轮换）；run 生命周期；`generate` / `manual` / `webhook` 三条路径。
- API：策略 CRUD、preview、run now、runs、generate/distribute、complete/fail/skip/notify、生成档位目录。
- UI：项目轮换页、密钥详情轮换页签、编辑表单（含生成档位/熵/自动生成）、生成新值与分发确认、轮次详情、worker 健康条。
- fixed 手动更新：提示、reason、`secret.updated`/Sync 回执；不假装通知了数据库。
- Webhook 事件补齐 run_id/old_version/grace；事件选择复选框。

### P1

- 多级逾期提醒/升级、失败重试、投递记录详情。
- 消费方确认（`change_ack`）/通知回执；把“已通知”和“已应用”分开记录。
- 覆盖率统计、未设置筛选、组织总览。
- outbox/sync/dynamic/audit 循环迁入 worker。
- 指标与日志面板；审计页轮换过滤。

### P2

- provider adapters（Postgres/MySQL 静态用户、云厂商、GitHub 等）。
- 组织级默认策略/模板、批量操作。
- 邮件/Slack 通知、审批流。
- 每策略独立 Webhook/执行器。

## 12. 待确认

1. **负责人模型**：只支持组织成员，还是也支持“团队/角色”？建议 v1 只支持成员，团队后续。
2. **权限严格度**：编辑策略是否只限 project admin？建议 v1 沿用 write，admin 可改派；后续加组织开关。
3. **时区**：cron 是否只支持 UTC？建议 v1 只支持 UTC，UI 显示本地预览；per-policy 时区后续。
4. **保留期**：run 保留 1 年是否合适？是否需要跟组织审计保留期一致？
5. **自动完成**：检测到新版本时，是否允许“自动完成”？建议只提示，不自动。
6. **manual 模式是否发 Webhook**：建议发，方便统一通知；但文案要说明“这不是自动执行”。
7. **事件命名**：`secret.rotation_overdue` 等新事件是否沿用 `secret.` 前缀？建议沿用。
8. **适配器优先级**：P2 先做哪一类？数据库静态用户、GitHub token、云厂商？
9. **组织级默认策略**：是否需要在组织设置里配置“默认宽限期/默认负责人”？建议 P1。
10. **默认生成档位**：`alnum32` 是否作为通用默认？数据库密码/云 key 不能套通用档位，建议按 secret 类型给推荐值，但 v1 只做通用档位 + 可选 length/prefix。
11. **自动生成**：`auto_generate` 是否在 P0 开放？建议开放但默认关闭；如果认为风险高，P0 只做“手动点生成”，自动生成放 P1。
12. **生成权限**：`write + reveal` 是否足够？是否需要独立的 `rotate` 权限（可生成、可看一次新值、不可看历史明文）？
13. **分发确认**：生成后必须人工确认分发；是否允许 `auto_generate + auto_distribute`？不建议，消费方未更新时会造成断服。
14. **旧版本退役**：P1 是否加 `retire_after` 和“旧版本已退役”标记？taboo 不是签发方时只能标记，不能真正吊销。
15. **节拍语义**：固定节拍 vs “上次完成后 30 天”？建议 v1 固定节拍，after-completion 后续再加。
16. **固定的强度**：`fixed` 只表示不安排轮换，手动仍可更新；是否需要更强的 `locked`（连手动写入也禁止）？本文建议不做，另立项。
17. **固定复核**：fixed 是否要每年复核一次？这是“复核提醒”，不是轮换；建议后续单独做。
18. **未设置覆盖率**：项目页是否把“未设置”算作覆盖缺口？建议显示数量，但不渲染成红色告警。
19. **外部真源**：数据库/云厂商是否要由 taboo 直接改？这需要 adapter，P2；v1 只做到“通知 + 提醒人工改上游”。
