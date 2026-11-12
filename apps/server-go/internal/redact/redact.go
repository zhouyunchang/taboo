// 请求日志脱敏：默认所有字符串值都写成 [REDACTED]，只有白名单里的元数据字段（name/env/role 等）原样保留；
// password/secret/token/value 等敏感字段无论类型一律脱敏。
package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
)

const marker = "[REDACTED]"

// maxLogged 是访问日志解析正文的上限；更大的正文只记“已省略”。
const maxLogged = 64 << 10

var sensitive = map[string]bool{
	"password": true, "passwd": true, "secret": true, "token": true, "value": true,
	"match": true, "content": true, "ciphertext": true, "authorization": true,
	"connectionstring": true, "clientsecret": true, "refresh": true, "accesstoken": true,
	"refreshtoken": true, "privatekey": true, "apikey": true, "totp": true, "code": true,
	"recoverycode": true, "dek": true, "masterkey": true, "access": true,
}

// visible 是允许原样写日志的字符串字段（规范化后的键名）。其它字符串字段一律脱敏，
// 这样 connection、new_password、批量 {KEY: value} 映射、webhook/sync 配置等新字段默认安全。
var visible = map[string]bool{
	"name": true, "slug": true, "env": true, "environment": true, "role": true, "type": true,
	"kind": true, "format": true, "file": true, "ruleid": true, "level": true, "key": true,
	"email": true, "status": true, "action": true, "path": true, "folder": true, "description": true,
	"mode": true, "provider": true, "scope": true, "id": true, "projectid": true, "orgid": true,
}

const maxVisible = 128

func normKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	return strings.ReplaceAll(key, "-", "")
}

func sensitiveKey(key string) bool { return sensitive[normKey(key)] }

// JSON 递归脱敏。非 JSON 文本不写进日志。
func JSON(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var v any
	if json.Unmarshal(body, &v) != nil {
		return "[non-json omitted]"
	}
	redactValue(&v)
	out, err := json.Marshal(v)
	if err != nil {
		return "[non-json omitted]"
	}
	if len(out) > 4096 {
		out = out[:4096]
	}
	return string(out)
}

func redactValue(v *any) {
	switch x := (*v).(type) {
	case map[string]any:
		for k, child := range x {
			if sensitiveKey(k) {
				x[k] = marker
				continue
			}
			if str, ok := child.(string); ok {
				if !visible[normKey(k)] || len(str) > maxVisible {
					x[k] = marker
				}
				continue
			}
			redactValue(&child)
			x[k] = child
		}
	case []any:
		for i := range x {
			child := x[i]
			if _, ok := child.(string); ok {
				x[i] = marker
				continue
			}
			redactValue(&child)
			x[i] = child
		}
	}
}

// Query 只保留参数名，敏感参数的值替换掉。
func Query(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return marker
	}
	for key, list := range values {
		if sensitiveKey(key) {
			for i := range list {
				list[i] = marker
			}
			values[key] = list
		}
	}
	return values.Encode()
}

// AccessLog 记录方法、路径、状态和脱敏后的查询/正文。不记录 Authorization。
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var logged string
		if r.Body != nil && r.Body != http.NoBody && r.Method != http.MethodGet && r.Method != http.MethodHead {
			// 只窥视前 64KB 用于日志，然后把已读部分与剩余流拼回去：处理器看到的正文不被截断。
			head, err := io.ReadAll(io.LimitReader(r.Body, maxLogged+1))
			r.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(head), r.Body), r.Body}
			switch {
			case err != nil:
			case len(head) > maxLogged:
				logged = "[large body omitted]"
			default:
				logged = JSON(head)
			}
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.Info("http",
			"request_id", httpx.RequestID(r),
			"method", r.Method,
			"path", r.URL.Path,
			"query", Query(r.URL.RawQuery),
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"body", logged,
		)
	})
}
