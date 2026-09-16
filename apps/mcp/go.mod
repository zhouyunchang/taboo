// taboo MCP Server（M4 #10）—— AI Agent 以标准 MCP 协议受控访问密钥：只读、细粒度 scope、全审计
// 仅暴露两个 Tool：secrets.list（元数据，永不含值）/ secrets.get（单值，默认掩码，显式 reveal 才返回明文）
// 认证走机器身份：client_id/secret 限定 项目+环境+read，吊销即断（验收标准）
module github.com/zhouyunchang/taboo/apps/mcp

go 1.23

require github.com/zhouyunchang/taboo/packages/sdk-go v0.0.0

replace github.com/zhouyunchang/taboo/packages/sdk-go => ../../packages/sdk-go
