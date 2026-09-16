// MCP 协议（2025-06-18 schema）最小实现：stdio 传输、newline-delimited JSON-RPC
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	errParse    = -32700
	errMethod   = -32601
	errInvalid  = -32602
	errInternal = -32603
	errToolCall = -32000
)

// tool / inputSchema（MCP tools capability）
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(s string) *toolResult {
	return &toolResult{Content: []contentBlock{{Type: "text", Text: s}}}
}

func errorResult(format string, args ...any) *toolResult {
	return &toolResult{Content: []contentBlock{{Type: "text", Text: fmt.Sprintf(format, args...)}}, IsError: true}
}

// serveStdio newline-delimited JSON-RPC 主循环；handler 按 method 分发，返回 result（notification 返回 nil）
func serveStdio(in io.Reader, out io.Writer, handler func(method string, params json.RawMessage) (any, *rpcError)) {
	rw := bufio.NewReadWriter(bufio.NewReader(in), bufio.NewWriter(out))
	enc := json.NewEncoder(rw)
	for {
		line, err := rw.ReadBytes('\n')
		if len(line) > 0 {
			var req rpcRequest
			if jerr := json.Unmarshal(line, &req); jerr != nil {
				_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: errParse, Message: "parse error"}})
				rw.Flush()
			} else if req.Method == "" {
				// 响应/未知帧，忽略
			} else if req.ID == nil || string(req.ID) == "null" {
				// notification：initialized / cancelled 等，无需应答
				_, _ = handler(req.Method, req.Params)
				rw.Flush()
			} else {
				result, rpcErr := handler(req.Method, req.Params)
				resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
				if rpcErr != nil {
					resp.Error = rpcErr
				} else {
					resp.Result = result
				}
				_ = enc.Encode(resp)
				rw.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
