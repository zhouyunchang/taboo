# taboo MCP Server

AI 编程 Agent（Claude Code、Kimi、Cursor 等）通过标准 **MCP 协议**受控访问 taboo 密钥 —— **只读、按项目授权、全审计**（M4 #10，设计文档 §7.3）。

## 安全模型

- **仅两个 Tool**：`secrets.list`（元数据，永不含值）/ `secrets.get`（单值，**默认掩码**，显式 `reveal: true` 才返回明文并记 reveal 审计）
- **认证 = 机器身份**：`client_id + client_secret` 换取短期 token；每个 scope 绑定一个项目，**没有环境维度**
- **权限分档**：
  - `read`：`secrets.list` + `secrets.get`（掩码）
  - `reveal`：在读取之上允许 `secrets.get` 的 `reveal: true`（明文，写 `secrets.reveal` 审计）
  - `write`：MCP 不使用，MCP 始终只读
- **吊销 identity 立即切断访问**：下一次 tool call 即被拒绝，服务端留审计
- 每次取值以 `identity:xxx` 落审计日志，可在 Web Dashboard 审计页追溯

## 前置条件

1. 控制台 → 组织 → **机器身份** → 新建身份。
2. 在 scope 里选择目标项目和权限：只读接入选 `read`；只有确实需要 Agent 主动取明文时才选 `reveal`。建议默认只给 `read`。
3. 记下**项目 UUID**（控制台项目页，或 `GET /api/v1/orgs/{slug}/projects`）。MCP 用 `TABOO_PROJECT_ID` 固定到这一个项目；要换项目就改配置并重启 MCP 进程。
4. `client_secret` 只展示一次，丢失只能重建身份。

## 构建

```bash
cd apps/mcp
go build -o taboo-mcp .
```

## 配置

MCP Server 只读这几个环境变量：

| 变量 | 必填 | 说明 |
|---|---|---|
| `TABOO_SERVER` | 否 | API 地址，默认 `http://localhost:7100` |
| `TABOO_CLIENT_ID` | 是 | 机器身份 client_id |
| `TABOO_CLIENT_SECRET` | 是 | 机器身份 client_secret，只展示一次 |
| `TABOO_PROJECT_ID` | 是 | 目标项目 UUID（scope 内） |

### Claude Code / Cursor（`~/.claude.json` 或项目 `.mcp.json`）

```json
{
  "mcpServers": {
    "taboo": {
      "command": "/abs/path/to/taboo-mcp",
      "env": {
        "TABOO_SERVER": "http://localhost:7100",
        "TABOO_CLIENT_ID": "mi_xxx",
        "TABOO_CLIENT_SECRET": "只展示一次，妥善保存",
        "TABOO_PROJECT_ID": "项目 UUID"
      }
    }
  }
}
```

### Kimi（项目 `.mcp.json`）

```json
{
  "mcpServers": {
    "taboo": {
      "command": "/abs/path/to/taboo-mcp",
      "env": {
        "TABOO_SERVER": "http://localhost:7100",
        "TABOO_CLIENT_ID": "mi_xxx",
        "TABOO_CLIENT_SECRET": "只展示一次，妥善保存",
        "TABOO_PROJECT_ID": "项目 UUID"
      }
    }
  }
}
```

### 密钥处理

- `.mcp.json` 里的 `TABOO_CLIENT_SECRET` 是明文。不要提交到 Git；仓库默认没有忽略 `.mcp.json`，使用前自行加入 `.gitignore`，或放在用户级配置（如 `~/.claude.json`）并限制权限（`chmod 600`）。
- 每个 Agent 用独立机器身份，只授予需要的项目与权限；换 Agent 或撤权时按身份吊销，不要共用 secret。
- Agent 能读到工作区的 `.mcp.json`；不要把长期凭证放在 Agent 可读的项目目录里。

## Tool 参考

| Tool | 参数 | 说明 |
|---|---|---|
| `secrets.list` | `path?` | 列出 key / 文件夹 / 版本 / 标签（无值）。`path` 是物化路径，如 `/team/`；缺省为整个项目 |
| `secrets.get` | `key`（必填）`path?` `reveal?` | 默认返回 `••••` 掩码；`reveal: true` 返回明文（需 `reveal` 权限，记审计） |

说明：

- 掩码固定为 `••••`，不随时间或密钥长度变化；不要用掩码长度推断明文长度。
- `reveal` 是独立权限。只用 `read` 身份时，`secrets.get` 带 `reveal: true` 会返回 `403 FORBIDDEN`。
- MCP 不支持写入、导出、轮换或切换组织；需要这些操作走 CLI / API，并按最小权限单独授权。

## 验收冒烟

```bash
cd apps/server-go && go run ./cmd/server &   # 先起 API（:7100）
cd apps/mcp && go build -o taboo-mcp.exe . && node smoke.js
```

覆盖：`initialize` / tools 列表 / 掩码默认 / 显式 reveal / 吊销后调用拒绝 / 审计留痕。冒烟会创建一次性账号和一个 `reveal` 机器身份，跑完直接退出；期望最后一行是 `ALL MCP CHECKS PASSED`。
