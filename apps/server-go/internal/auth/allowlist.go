package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
)

// ErrCIDR 表示白名单不是合法的 IP 或 CIDR。
var ErrCIDR = errors.New("invalid cidr")

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// ParseCIDRs 校验并规范化可选 IP/CIDR 白名单。空列表表示不限制。
func ParseCIDRs(in []string) ([]string, error) {
	if len(in) > 32 {
		return nil, ErrCIDR
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" || len(item) > 64 {
			return nil, ErrCIDR
		}
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				return nil, ErrCIDR
			}
			if ip.To4() != nil {
				item += "/32"
			} else {
				item += "/128"
			}
		}
		_, network, err := net.ParseCIDR(item)
		if err != nil || network == nil {
			return nil, ErrCIDR
		}
		norm := network.String()
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out, nil
}

// IPAllowed 空白名单放行。无法解析的客户端地址在设置了白名单时拒绝。
func IPAllowed(stored, clientIP string) bool {
	stored = strings.TrimSpace(stored)
	if stored == "" || stored == "[]" || stored == "null" {
		return true
	}
	var list []string
	if json.Unmarshal([]byte(stored), &list) != nil {
		return false
	}
	if len(list) == 0 {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}
	for _, item := range list {
		_, network, err := net.ParseCIDR(item)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// IdentityIPAllowed 读取机器身份白名单。查询失败时拒绝，避免把限制当成放行。
func IdentityIPAllowed(db rowQuerier, id, clientIP string) bool {
	var stored string
	if err := db.QueryRow(`SELECT COALESCE(allowed_cidrs, '') FROM machine_identities WHERE id = ?`, id).Scan(&stored); err != nil {
		return false
	}
	return IPAllowed(stored, clientIP)
}

// EncodeCIDRs 把规范化后的列表存成 JSON。空列表存空串，表示不限制。
func EncodeCIDRs(list []string) string {
	if len(list) == 0 {
		return ""
	}
	b, _ := json.Marshal(list)
	return string(b)
}
