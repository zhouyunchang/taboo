# taboo agent skill

给 AI Agent 用的 taboo（禁制）操作指南：CLI + 只读 MCP server，包含 locator、权限、`--recursive` 和“不要把明文带进对话/日志”的硬性规则。

## 安装

仓库内源文件是 `skills/taboo/SKILL.md`，不要复制出第二份长期维护的副本；优先用 symlink。

### DeepSeek Harness / Claude Code（用户级）

```sh
mkdir -p ~/.agents/skills ~/.claude/skills
ln -sfn "$PWD/skills/taboo" ~/.agents/skills/taboo
ln -sfn "$PWD/skills/taboo" ~/.claude/skills/taboo
```

在仓库根目录执行；`$PWD/skills/taboo` 不存在时先确认当前目录。

DeepSeek Harness 与 Claude Code 都会从用户级 skills 目录发现 `SKILL.md`；装完需要新开一个会话才会出现在技能列表里。

### Claude Code（项目级）

```sh
mkdir -p .claude/skills
ln -sfn ../../skills/taboo .claude/skills/taboo
```

### Kimi Code

Kimi 支持 `--skills-dir`，直接指向仓库的 `skills/`：

```sh
kimi --skills-dir "$PWD/skills" -p 'Use the taboo skill to list keys in owner/project, no values.'
```

## 前置条件

- 已安装 `taboo` CLI，或当前工作区就是 taboo 仓库（用 `node apps/cli/taboo.js`）。
- CLI 已登录（本地账号或机器身份），或已配置 taboo MCP server。
- MCP 配置见 [apps/mcp/README.md](../../apps/mcp/README.md)；不要把 client_secret 提交到仓库。

## 验证

先在本地起一个临时 taboo（SQLite 即可），创建个人空间、项目、目录和测试密钥，然后让 Agent 只读取元数据：

```sh
TABOO_SERVER=http://127.0.0.1:17100 \
TABOO_SESSION_FILE=/tmp/taboo-skill-session.json \
kimi --skills-dir "$PWD/skills" -p \
'Use the taboo skill. List keys in owner/project/team. Report only key names; do not reveal values.'
```

期望：输出只有 key 名，没有值；如果 Agent 主动 `get` 或 `export`，说明 skill 没有生效或规则被忽略。

再验证注入和拒绝路径：

1. 用 `taboo run owner/project/team -- sh -c 'test -n "$KEY" && echo present'`，期望 `present`，不要打印 `$KEY`。
2. 用只读机器身份请求 `reveal`，期望 `403`；Agent 不应换身份或改 locator 重试。
3. 用 `--recursive` 前先确认用户是否真的要整个子树。

## 维护

- CLI 命令、locator、`export` / `run` 的目录语义变化时，同步更新 `SKILL.md`。
- MCP 配置和工具说明以 [apps/mcp/README.md](../../apps/mcp/README.md) 为准，skill 只保留 Agent 必须遵守的规则和最常用命令。
- 不要把真实 secret、client_secret、session 内容写进 skill 或测试用例。
