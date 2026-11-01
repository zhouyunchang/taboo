// 请求日志脱敏：password/secret/token/value 等字段写成 [REDACTED]。
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

var sensitive = map[string]bool{
	"password": true, "passwd": true, "secret": true, "token": true, "value": true,
	"match": true, "content": true, "ciphertext": true, "authorization": true,
	"connectionstring": true, "clientsecret": true, "refresh": true, "accesstoken": true,
	"refreshtoken": true, "privatekey": true, "apikey": true, "totp": true, "code": true,
	"recoverycode": true, "dek": true, "masterkey": true, "access": true,
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return sensitive[key]
}

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
			redactValue(&child)
			x[k] = child
		}
	case []any:
		for i := range x {
			child := x[i]
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
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+8192))
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			if err == nil {
				logged = JSON(body)
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
