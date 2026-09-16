// 登录限流：5 次/分钟/IP —— 对齐设计文档 §8 安全清单
package auth

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
)

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	limit    int
	window   time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{attempts: map[string][]time.Time{}, limit: limit, window: window}
}

func (l *rateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	valid := l.attempts[ip][:0]
	for _, t := range l.attempts[ip] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	l.attempts[ip] = valid
	if len(valid) >= l.limit {
		return false
	}
	l.attempts[ip] = append(l.attempts[ip], now)
	return true
}

// LoginRateLimit 中间件：同一 IP（忽略源端口）每窗口限流（默认 5 次/分钟）
func LoginRateLimit(window time.Duration, limit int) func(http.Handler) http.Handler {
	l := newRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.allow(clientIP(r)) {
				writeErr(w, apperr.TooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP 提取客户端主机 IP（RemoteAddr 为 IP:port 形式；无端口时原样返回）
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
