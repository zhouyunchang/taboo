---
name: taboo
description: Use when a task needs secrets from a taboo（禁制）instance — list keys, read a value, inject secrets into a command, import a .env, or write a secret through the taboo CLI or MCP server. Triggers on a taboo locator such as owner/project/folder, TABOO_SERVER, a taboo MCP server, or a repo documenting taboo. Do not use for generic .env editing, other secret managers, or tasks that do not touch taboo.
---

# taboo（禁制）

taboo 是自部署密钥管理平台。Agent 通过 CLI 或只读 MCP server 访问它。目标不是“把密钥拿出来看看”，而是完成用户的任务，同时不把明文扩散到对话、日志和提交里。

## 硬性规则

1. 默认只读。`list` 取元数据；`get` / `export` 只有用户明确要求某个值或导出时才用。
2. 不在最终回答、命令输出、日志、提交信息、URL、issue、截图中出现明文。除非用户明确要求返回该值，否则只报告“存在 / 版本 / 目录”。
3. 优先 `taboo run` 把密钥注入子进程，而不是先 `get` 再手工拼接。`export` 会把整个目录打到 stdout，Agent 会把它读进上下文，能不用就不用。
4. 不读 `.mcp.json`、`~/.taboo/session.json`、`.env` 或任何保存 client_secret 的文件。配置由用户或宿主提供。
5. `403` 就是拒绝：不要换机器身份、不要改 locator 到根目录、不要用 `export` 绕过。报告缺少的 scope 或审批。
6. locator 必须由用户或项目配置显式给出，格式 `owner/project/folder`；根目录是 `owner/project`。不要从 cwd 猜 owner/project。
7. `export` / `run` 默认只处理 locator 指向的目录；`--recursive` 只在用户明确要整个子树或项目时加。
8. MCP 只读，没有写、导出或轮换工具；需要写时走 CLI，并先得到用户明确同意。
9. 写操作（`set` / `import`）必须先确认目标 locator、key 和影响范围；不要自动覆盖已有 key。重试时复用同一个 `--idempotency-key`。
10. 不把 secret 作为命令行参数传给 `set`（进程列表可见），除非用户接受；优先 `import` 一个 0600 临时文件并立即删除。

## 选择接入方式

按顺序判断：

1. **MCP**：工具列表里有 `secrets.list` / `secrets.get`（通常名为 `mcp__taboo__secrets_list` / `mcp__taboo__secrets_get`）。MCP 固定一个项目（`TABOO_PROJECT_ID`），只读。
2. **CLI**：`taboo` 在 PATH，或当前工作区是 taboo 仓库（`node apps/cli/taboo.js`）。
3. 都没有：告诉用户需要安装或配置；不要自己改 MCP 配置或安装全局包。

## CLI

环境：

- `TABOO_SERVER`（默认 `http://localhost:7100`）
- `TABOO_SESSION_FILE`（可选，隔离会话）
- 已有会话就复用；没有会话时让用户登录，不要猜密码或 TOTP。

定位符：

- `owner/project` = 根目录
- `owner/project/folder` = 子目录；不要带空格
- 目录段是 slug；不确定时先 `list` 父目录确认

命令：

```sh
# 元数据，不含值
taboo list owner/project/folder

# 只在用户明确要这个值时
taboo get owner/project/folder KEY

# 首选：把该目录注入子进程，不打印值
taboo run owner/project/folder -- sh -c 'test -n "$KEY" && echo present'

# 用户明确要整个项目或子树时
taboo export owner/project --recursive

# 写入（先确认）
taboo set owner/project/folder KEY VALUE --idempotency-key <stable-id>

# 或先预览再导入
taboo import owner/project/folder file.env --preview
taboo import owner/project/folder file.env --format dotenv
```

仓库内没有全局安装时，把 `taboo` 换成 `node apps/cli/taboo.js`。

## MCP

- `secrets.list`：key / folder / version / tags，无值；`path` 是物化路径 `/team/`，缺省整个项目。
- `secrets.get`：默认 `••••`；只有 `reveal: true` 才返回明文（需要 `reveal` scope，记审计）。
- 没有写工具；不要尝试用 list/get 拼写入。
- 固定 `TABOO_PROJECT_ID`，不能切组织或项目；需要别的项目就换配置重启，或走 CLI。
- 被拒绝时按 403 处理，不换身份。

## 常见错误

| 现象 | 处理 |
|---|---|
| `401 UNAUTHORIZED` | 会话过期；让用户重新登录，不读 session 文件 |
| `403 FORBIDDEN` | 缺 read / reveal / write scope 或审批；报告，不换身份 |
| `404 LOCATOR_NOT_FOUND` / `NOT_FOUND` | locator 或目录不存在；回到上一层 `list` 确认，不退回整项目 |
| `409` | 版本或导入冲突；报告，不自动覆盖；用 `--preview` 或让用户选 `skip` / `overwrite` |
| `503` / `5xx` | 最多重试一次；仍失败就报告 |
| MCP `access denied` | 同 403；不要通过 CLI 绕路 |

## 交付前

- 最终回答里没有明文（除非用户明确要求）。
- 没有把 secret 写入文件、Git、日志、URL。
- 没有为了让命令成功而扩大 locator、换身份或加 `--recursive`。
- 写入操作有用户确认，且目标 locator 正确。
