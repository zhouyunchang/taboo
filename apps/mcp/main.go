// taboo MCP Server 入口（M4 #10）—— 配置经环境变量（.mcp.json 注入）：
//
//	TABOO_SERVER       API 地址，默认 http://localhost:7100
//	TABOO_CLIENT_ID    机器身份 client_id（scope 须限定 项目+环境+read）
//	TABOO_CLIENT_SECRET
//	TABOO_PROJECT_ID   目标项目（scope 内）
//	TABOO_ENV          默认环境，默认 dev
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	taboo "github.com/zhouyunchang/taboo/packages/sdk-go"
)

const (
	serverName    = "taboo"
	serverVersion = "0.4.0"
	protocolVer   = "2025-06-18"
)

var (
	client  *taboo.Client
	project string
	envDef  string
)

func mask(s string) string {
	if len(s) <= 4 {
		return "••••"
	}
	return s[:2] + strings.Repeat("•", len(s)-4) + s[len(s)-2:]
}

func dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVer,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []mcpTool{
			{
				Name:        "secrets.list",
				Description: "列出密钥元数据（key/folder/version/tags，永不包含明文值）。参数均可选。",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"env":  map[string]any{"type": "string", "description": "环境 slug，默认 " + envDef},
						"path": map[string]any{"type": "string", "description": "文件夹物化路径 /a/b/，缺省整个环境"},
					},
				},
			},
			{
				Name:        "secrets.get",
				Description: "读取单个密钥。默认返回掩码值；仅当显式 reveal=true 才返回明文（记 reveal 审计）。",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key":    map[string]any{"type": "string", "description": "密钥 key（必填）"},
						"env":    map[string]any{"type": "string", "description": "环境 slug，默认 " + envDef},
						"path":   map[string]any{"type": "string", "description": "文件夹物化路径，默认根目录 /"},
						"reveal": map[string]any{"type": "boolean", "description": "显式请求明文（默认 false，返回掩码）"},
					},
					"required": []string{"key"},
				},
			},
		}}, nil
	case "tools/call":
		var p toolCallParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: errInvalid, Message: "invalid params"}
		}
		return callTool(p)
	default:
		return nil, &rpcError{Code: errMethod, Message: "method not found: " + method}
	}
}

func callTool(p toolCallParams) (any, *rpcError) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var args struct {
		Key    string `json:"key"`
		Env    string `json:"env"`
		Path   string `json:"path"`
		Reveal bool   `json:"reveal"`
	}
	_ = json.Unmarshal(p.Arguments, &args)
	e := args.Env
	if e == "" {
		e = envDef
	}

	switch p.Name {
	case "secrets.list":
		items, err := client.ListSecrets(ctx, project, e, args.Path)
		if err != nil {
			return toolErr(err), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d secret(s)%s\n", len(items), scopeNote(e, args.Path))
		for _, s := range items {
			fmt.Fprintf(&b, "- %s\tv%d\t%s\ttags=%v\n", s.Key, s.Version, s.Folder, s.Tags)
		}
		return textResult(b.String()), nil

	case "secrets.get":
		if args.Key == "" {
			return errorResult("missing required argument: key"), nil
		}
		sv, err := client.GetSecret(ctx, project, e, args.Path, args.Key)
		if err != nil {
			return toolErr(err), nil
		}
		value := mask(sv.Value)
		note := "masked (reveal=false)"
		if args.Reveal {
			value = sv.Value
			note = "REVEALED — 已记审计"
		}
		return textResult(fmt.Sprintf("%s=%s\n# folder=%s v%d · %s", sv.Key, value, sv.Folder, sv.Version, note)), nil

	default:
		return errorResult("unknown tool: %s", p.Name), nil
	}
}

// toolErr 区分鉴权失败（identity 被吊销/越权）与一般错误，均结构化返回
func toolErr(err error) *toolResult {
	var ae *taboo.ApiError
	if ok := asApiError(err, &ae); ok {
		if ae.Status == 401 || ae.Status == 403 {
			return errorResult("access denied (%d %s): %s — 请检查机器身份 scope 或是否已被吊销", ae.Status, ae.Code, ae.Message)
		}
		return errorResult("api error (%d %s): %s", ae.Status, ae.Code, ae.Message)
	}
	return errorResult("request failed: %v", err)
}

func asApiError(err error, target **taboo.ApiError) bool {
	for err != nil {
		if ae, ok := err.(*taboo.ApiError); ok {
			*target = ae
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func scopeNote(e, p string) string {
	if p != "" {
		return fmt.Sprintf(" env=%s path=%s", e, p)
	}
	return fmt.Sprintf(" env=%s", e)
}

func main() {
	base := getenv("TABOO_SERVER", "http://localhost:7100")
	cid := os.Getenv("TABOO_CLIENT_ID")
	csec := os.Getenv("TABOO_CLIENT_SECRET")
	project = os.Getenv("TABOO_PROJECT_ID")
	envDef = getenv("TABOO_ENV", "dev")
	if cid == "" || csec == "" || project == "" {
		fmt.Fprintln(os.Stderr, "taboo-mcp: TABOO_CLIENT_ID / TABOO_CLIENT_SECRET / TABOO_PROJECT_ID are required")
		os.Exit(2)
	}
	client = taboo.New(base).WithIdentity(cid, csec)
	serveStdio(os.Stdin, os.Stdout, dispatch)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
