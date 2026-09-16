# taboo MCP Server

AI 编程 Agent（Claude Code、Kimi、Cursor 等）通过标准 **MCP 协议**受控访问 taboo 密钥 —— **只读、细粒度 scope、全审计**（M4 #10，设计文档 §7.3）。

## 安全模型

- **仅两个 Tool**：`secrets.list`（元数据，永不含值）/ `secrets.get`（单值，**默认掩码**，显式 `reveal: true` 才返回明文并记 reveal 审计）
- **认证 = 机器身份**：`client_id + client_secret` 经环境变量注入，scope 显式限定 项目 + 环境 + read；**吊销 identity 立即切断访问**（下一次 tool call 即被拒绝，服务端留审计）
- 每次取值以 `identity:xxx` 落审计日志，可在 Web Dashboard 审计页追溯

## 构建

```bash
cd apps/mcp
go build -o taboo-mcp .
```

## 配置（各 Agent 的 `.mcp.json`）

先在 Web Dashboard「机器身份」创建身份：**权限 read、环境按需（如 dev）、项目 = 目标项目**。

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
        "TABOO_PROJECT_ID": "项目 UUID",
        "TABOO_ENV": "dev"
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
      "env": { "TABOO_CLIENT_ID": "mi_xxx", "TABOO_CLIENT_SECRET": "...", "TABOO_PROJECT_ID": "...", "TABOO_ENV": "dev" }
    }
  }
}
```

## Tool 参考

| Tool | 参数 | 说明 |
|---|---|---|
| `secrets.list` | `env?` `path?` | 列出 key / 文件夹 / 版本 / 标签（无值） |
| `secrets.get` | `key`（必填）`env?` `path?` `reveal?` | 默认返回 `ab••••••••cd` 掩码；`reveal: true` 返回明文（记审计） |

## 验收冒烟

```bash
cd apps/server-go && go run ./cmd/server &   # 先起 API（:7100）
cd apps/mcp && go build -o taboo-mcp.exe . && node smoke.js
```

覆盖：initialize / tools 列表 / 掩码默认 / 显式 reveal / dev scope 访问 prod 拒绝 / **吊销后调用拒绝 + 审计留痕**。
