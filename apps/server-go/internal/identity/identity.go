// 机器身份服务（M2 #3）—— 对齐设计文档 §2 / §5 / §6 / §7.1
//
//	POST /api/v1/orgs/:slug/identities              创建（client_secret 仅返回一次）
//	GET  /api/v1/orgs/:slug/identities              列表（不含 secret）
//	POST /api/v1/orgs/:slug/identities/:id/revoke   吊销（立即生效）
//	POST /api/v1/identities/token                   client_credentials 换短期 JWT
package identity

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

type Service struct {
	DB        *sql.DB
	JWTSecret string
}

type ScopeIn struct {
	ProjectID  string `json:"project_id"`
	Permission string `json:"permission"` // read | reveal | write
}

type ScopeOut struct {
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`
	Permission  string `json:"permission"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

// orgOf 校验组织存在 + 当前用户为成员（机器身份本身不可管理身份）
func (s *Service) orgOf(w http.ResponseWriter, r *http.Request) (orgID, slug string, ok bool) {
	slug = chi.URLParam(r, "slug")
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&orgID); err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false
	}
	u := auth.From(r)
	if u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	if _, member := auth.Membership(s.DB, orgID, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	return orgID, slug, true
}

// Create POST /orgs/:slug/identities
func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	u := auth.From(r)
	if !auth.Require(s.DB, w, r, orgID, "", auth.DelegationAction(auth.From(r)), "") {
		return
	}
	var b struct {
		Name            string    `json:"name"`
		TokenTTL        int       `json:"token_ttl"`
		Scopes          []ScopeIn `json:"scopes"`
		AllowedCIDRs    []string  `json:"allowed_cidrs"`
		ListScanReports bool      `json:"list_scan_reports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Name == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.TokenTTL <= 0 || b.TokenTTL > 3600 {
		b.TokenTTL = 900 // 默认 15min，上限 1h
	}
	if len(b.Scopes) == 0 {
		writeErr(w, apperr.New(400, "INVALID", "at least one scope required (no wildcard)"))
		return
	}
	cidrs, err := auth.ParseCIDRs(b.AllowedCIDRs)
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "allowed_cidrs must be IP addresses or CIDR blocks"))
		return
	}
	listScans := 0
	if b.ListScanReports {
		listScans = 1
	}
	// 校验并解析 scope：显式 (project, permission)，禁止通配。
	type resolved struct {
		projectID, permission string
	}
	resolvedScopes := make([]resolved, 0, len(b.Scopes))
	for _, sc := range b.Scopes {
		if sc.Permission != "read" && sc.Permission != "reveal" && sc.Permission != "write" {
			writeErr(w, apperr.New(400, "INVALID", "permission must be read, reveal or write"))
			return
		}
		if sc.ProjectID == "" || sc.ProjectID == "*" {
			writeErr(w, apperr.New(400, "INVALID", "wildcard scope not allowed"))
			return
		}
		var orgCheck string
		err := s.DB.QueryRow(`SELECT org_id FROM projects WHERE id = ?`, sc.ProjectID).Scan(&orgCheck)
		if err != nil || orgCheck != orgID {
			writeErr(w, apperr.New(400, "INVALID", "invalid scope project"))
			return
		}
		resolvedScopes = append(resolvedScopes, resolved{sc.ProjectID, sc.Permission})
	}

	id := tc.NewID()
	clientID := "mi_" + tc.NewID()
	secretBytes := make([]byte, 32)
	_, _ = rand.Read(secretBytes)
	clientSecret := hex.EncodeToString(secretBytes)

	tx, err := s.DB.Begin()
	if err != nil {
		slog.Error("identity create failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO machine_identities (id, org_id, name, auth_type, client_id, secret_hash, token_ttl, allowed_cidrs, list_scan_reports)
		VALUES (?, ?, ?, 'client_credentials', ?, ?, ?, ?, ?)`,
		id, orgID, b.Name, clientID, tc.SHA256(clientSecret), b.TokenTTL, auth.EncodeCIDRs(cidrs), listScans); err != nil {
		slog.Error("identity create failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	for _, rs := range resolvedScopes {
		if _, err := tx.Exec(`INSERT INTO identity_scopes (identity_id, project_id, permission)
			VALUES (?, ?, ?) ON CONFLICT(identity_id,project_id) DO UPDATE SET permission=excluded.permission`, id, rs.projectID, rs.permission); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	if !auth.CommitMutation(tx, w, r, orgID, u, "identity.create", "org/"+slug+"/identity/"+b.Name, map[string]any{"client_id": clientID, "scopes": len(resolvedScopes), "allowed_cidrs": len(cidrs), "list_scan_reports": b.ListScanReports}) {
		return
	}
	// client_secret 仅本次响应返回一次
	writeJSON(w, 201, map[string]any{
		"id": id, "name": b.Name, "client_id": clientID,
		"client_secret": clientSecret, "token_ttl": b.TokenTTL,
		"allowed_cidrs": cidrs, "list_scan_reports": b.ListScanReports,
		"scopes": scopeOuts(s.DB, id),
	})
}

// List GET /orgs/:slug/identities
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.Admin, "") {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, client_id, status, token_ttl, created_at, allowed_cidrs, list_scan_reports
		FROM machine_identities WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		slog.Error("identity list failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	defer rows.Close()
	type item struct {
		ID              string     `json:"id"`
		Name            string     `json:"name"`
		ClientID        string     `json:"client_id"`
		Status          string     `json:"status"`
		TokenTTL        int        `json:"token_ttl"`
		CreatedAt       string     `json:"created_at"`
		AllowedCIDRs    []string   `json:"allowed_cidrs"`
		ListScanReports bool       `json:"list_scan_reports"`
		Scopes          []ScopeOut `json:"scopes"`
	}
	out := []item{}
	ids := []string{}
	for rows.Next() {
		var it item
		var cidrs string
		var listAll int
		_ = rows.Scan(&it.ID, &it.Name, &it.ClientID, &it.Status, &it.TokenTTL, &it.CreatedAt, &cidrs, &listAll)
		it.AllowedCIDRs = decodeCIDRs(cidrs)
		it.ListScanReports = listAll == 1
		out = append(out, it)
		ids = append(ids, it.ID)
	}
	rows.Close() // 先释放连接（MaxOpenConns=1），再逐条查 scope，避免池耗尽死锁
	for i := range out {
		out[i].Scopes = scopeOuts(s.DB, ids[i])
	}
	_ = auth.AuditReq(s.DB, r, orgID, auth.From(r), "identity.list", "org/"+slug+"/identities", nil)
	writeJSON(w, 200, map[string]any{"identities": out})
}

// Revoke POST /orgs/:slug/identities/:id/revoke —— 吊销立即生效（loadIdentity 拒绝非 active）
func (s *Service) Revoke(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	u := auth.From(r)
	if !auth.Require(s.DB, w, r, orgID, "", auth.DelegationAction(auth.From(r)), "") {
		return
	}
	id := chi.URLParam(r, "id")
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM machine_identities WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&name); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	tx, ok := auth.BeginMutation(s.DB, w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE machine_identities SET status='revoked' WHERE id=? AND org_id=?`, id, orgID); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "change was not committed"))
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, u, "identity.revoke", "org/"+slug+"/identity/"+name, nil) {
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": "revoked"})
}

// Update PATCH /orgs/:slug/identities/:id 修改 IP 白名单和扫描报告可见范围。
func (s *Service) Update(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	u := auth.From(r)
	if !auth.Require(s.DB, w, r, orgID, "", auth.DelegationAction(u), "") {
		return
	}
	var b struct {
		AllowedCIDRs    *[]string `json:"allowed_cidrs"`
		ListScanReports *bool     `json:"list_scan_reports"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b); err != nil || (b.AllowedCIDRs == nil && b.ListScanReports == nil) {
		writeErr(w, apperr.InvalidInput)
		return
	}
	id := chi.URLParam(r, "id")
	var name, authType string
	if err := s.DB.QueryRow(`SELECT name, auth_type FROM machine_identities WHERE id = ? AND org_id = ?`, id, orgID).Scan(&name, &authType); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if authType != "client_credentials" && authType != "external_oauth" {
		writeErr(w, apperr.NotFound)
		return
	}
	sets := []string{}
	args := []any{}
	meta := map[string]any{}
	if b.AllowedCIDRs != nil {
		cidrs, err := auth.ParseCIDRs(*b.AllowedCIDRs)
		if err != nil {
			writeErr(w, apperr.New(400, "INVALID", "allowed_cidrs must be IP addresses or CIDR blocks"))
			return
		}
		sets = append(sets, "allowed_cidrs = ?")
		args = append(args, auth.EncodeCIDRs(cidrs))
		meta["allowed_cidrs"] = len(cidrs)
	}
	if b.ListScanReports != nil {
		flag := 0
		if *b.ListScanReports {
			flag = 1
		}
		sets = append(sets, "list_scan_reports = ?")
		args = append(args, flag)
		meta["list_scan_reports"] = *b.ListScanReports
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	args = append(args, id, orgID)
	if _, err := tx.Exec(`UPDATE machine_identities SET `+joinComma(sets)+` WHERE id = ? AND org_id = ?`, args...); err != nil {
		slog.Error("identity update failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, u, "identity.update", "org/"+slug+"/identity/"+name, meta) {
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "updated": true})
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func decodeCIDRs(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil || list == nil {
		return []string{}
	}
	return list
}

// Token POST /identities/token（公开，限流保护）—— client_credentials 换短期 JWT
func (s *Service) Token(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	var id, orgID, name, secretHash, cidrs string
	var status string
	var ttl int
	err := s.DB.QueryRow(`SELECT id, org_id, name, secret_hash, status, token_ttl, allowed_cidrs
		FROM machine_identities WHERE client_id = ? AND auth_type='client_credentials'`, b.ClientID).
		Scan(&id, &orgID, &name, &secretHash, &status, &ttl, &cidrs)
	// 统一错误，不区分“不存在”与“秘密错误”
	if err != nil || status != "active" || !auth.CheckIdentitySecret(b.ClientSecret, secretHash) {
		_ = auth.AuditReq(s.DB, r, "-", nil, "identity.token.failed", "identity/"+b.ClientID, map[string]any{"reason": "bad_credentials"})
		writeErr(w, apperr.BadCredentials)
		return
	}
	if !auth.IPAllowed(cidrs, httpx.ClientIP(r)) {
		_ = auth.AuditReq(s.DB, r, orgID, &auth.Actor{ID: id, Name: name, Kind: auth.KindIdentity}, "identity.token.failed", "identity/"+name, map[string]any{"reason": "ip_not_allowed"})
		writeErr(w, apperr.New(403, "IP_NOT_ALLOWED", "client address is not allowed for this machine identity"))
		return
	}
	access, err := tc.SignJWTClaims(map[string]any{
		"sub": id, "typ": auth.KindIdentity, "name": name, "org": orgID,
	}, s.JWTSecret, time.Duration(ttl)*time.Second)
	if err != nil {
		slog.Error("identity token failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err := auth.AuditReq(s.DB, r, orgID, &auth.Actor{ID: id, Name: name, Kind: auth.KindIdentity},
		"identity.token", "identity/"+name, map[string]any{"ttl": ttl}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	writeJSON(w, 200, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": ttl,
	})
}

func scopeOuts(db *sql.DB, identityID string) []ScopeOut {
	rows, err := db.Query(`SELECT s.project_id, p.slug, s.permission
		FROM identity_scopes s
		JOIN projects p ON p.id = s.project_id
		WHERE s.identity_id = ?
		ORDER BY p.slug`, identityID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ScopeOut{}
	for rows.Next() {
		var sc ScopeOut
		_ = rows.Scan(&sc.ProjectID, &sc.ProjectSlug, &sc.Permission)
		out = append(out, sc)
	}
	return out
}
