package httpx

import (
	"net"
	"net/http"
	"strings"
)

// DefaultTrustedProxies 未配置 TABOO_TRUSTED_PROXIES 时信任的直连对端：本机与私网（容器网络里的反向代理）。
const DefaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// ParseTrusted 解析逗号分隔的 IP/CIDR 列表；空串用默认值，"none" 表示不信任任何代理头。非法项忽略。
func ParseTrusted(raw string) []*net.IPNet {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = DefaultTrustedProxies
	}
	if strings.EqualFold(raw, "none") {
		return nil
	}
	var out []*net.IPNet
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			if ip := net.ParseIP(item); ip != nil {
				if ip.To4() != nil {
					item += "/32"
				} else {
					item += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(item); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// RealIP 取代 chi 的 middleware.RealIP。只有直连对端在 trusted 列表里时才采信代理头，
// 且只认 X-Real-IP（NPM/nginx 用 $remote_addr 覆盖写入），其次是 X-Forwarded-For 最右一跳。
// True-Client-IP 一律忽略：NPM 会原样透传客户端自带的该头，chi 的 RealIP 却优先采信它。
func RealIP(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if peer := net.ParseIP(ClientIP(r)); peer != nil && contains(trusted, peer) {
				if ip := forwardedIP(r); ip != "" {
					r.RemoteAddr = ip
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip.String()
		}
		return ""
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func contains(list []*net.IPNet, ip net.IP) bool {
	for _, n := range list {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
