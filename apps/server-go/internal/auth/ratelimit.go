// 登录限流：5 次/分钟/IP —— 对齐设计文档 §8 安全清单。
// 有数据库时计数在 rate_counters，多副本共用；库失败时拒绝而不是放行。
package auth

import (
	"database/sql"
	"log/slog"
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

// Hit records one attempt in the shared counter. The bool is false when the
// window is already full. A database error is returned so callers fail closed.
func Hit(db *sql.DB, bucket string, window time.Duration, limit int) (bool, error) {
	if db == nil || limit <= 0 {
		return true, nil
	}
	if len(bucket) > 200 {
		bucket = bucket[:200]
	}
	now := time.Now().Unix()
	size := int64(window.Seconds())
	if size < 1 {
		size = 1
	}
	start := now / size * size
	var hits int
	err := db.QueryRow(`INSERT INTO rate_counters(bucket, window_start, hits) VALUES(?,?,1)
		ON CONFLICT(bucket, window_start) DO UPDATE SET hits=rate_counters.hits+1 RETURNING hits`, bucket, start).Scan(&hits)
	if err != nil {
		return false, err
	}
	return hits <= limit, nil
}

// LoginRateLimit 中间件：同一 IP（忽略源端口）每窗口限流（默认 5 次/分钟）。
// db 为空时退回进程内计数，只用于没有数据库的路由装配自检。
func LoginRateLimit(db *sql.DB, window time.Duration, limit int) func(http.Handler) http.Handler {
	l := newRateLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if db == nil {
				if !l.allow(clientIP(r)) {
					writeErr(w, apperr.TooManyRequests)
				} else {
					next.ServeHTTP(w, r)
				}
				return
			}
			ok, err := Hit(db, "login:"+r.URL.Path+":"+clientIP(r), window, limit)
			if err != nil {
				slog.Error("login rate limit unavailable", "err", err)
				writeErr(w, apperr.New(503, "RATE_LIMIT_UNAVAILABLE", "try again"))
				return
			}
			if !ok {
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
