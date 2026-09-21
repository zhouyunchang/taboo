package oidc

import "strings"

// claimValues 读取点分路径 claim（如 groups、realm_access.roles）。
func claimValues(claims map[string]any, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" || claims == nil {
		return nil
	}
	var cur any = claims
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return asStringList(cur)
}

func asStringList(v any) []string {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, i := range x {
			if s, ok := i.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// groupAliases 展开 Keycloak 组路径：/taboo/admins → 全路径、去斜杠、末段。
func groupAliases(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	seen := map[string]bool{v: true}
	out := []string{v}
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	if strings.HasPrefix(v, "/") {
		add(strings.TrimPrefix(v, "/"))
	}
	if i := strings.LastIndex(v, "/"); i >= 0 && i < len(v)-1 {
		add(v[i+1:])
	}
	return out
}

func pickRole(def string) string {
	if validRoles[def] {
		return def
	}
	return "viewer"
}

// MapRole claim → 角色：映射命中取最高角色，否则默认角色。
// Keycloak 组路径与 realm_access.roles 点分路径均可。
func MapRole(claims map[string]any, claim string, roleMap map[string]string, def string) string {
	best := pickRole(def)
	bestRank := roleRank[best]
	if len(roleMap) == 0 {
		return best
	}
	for _, gv := range claimValues(claims, claim) {
		for _, alias := range groupAliases(gv) {
			if r, ok := roleMap[alias]; ok && validRoles[r] && roleRank[r] > bestRank {
				best, bestRank = r, roleRank[r]
			}
		}
	}
	return best
}

// capJITRole 外部 IdP 不能把新用户直接映射成 owner（owner 保留给本地引导账号）。
func capJITRole(role string) string {
	if role == "owner" {
		return "admin"
	}
	return role
}

func oauthScopes(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "openid email profile"
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]string, 0, len(parts)+1)
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if !seen["openid"] {
		out = append([]string{"openid"}, out...)
	}
	return strings.Join(out, " ")
}

func claimString(claims map[string]any, keys ...string) string {
	for _, k := range keys {
		for _, v := range claimValues(claims, k) {
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}
