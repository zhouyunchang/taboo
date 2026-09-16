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
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type Service struct {
	DB        *sql.DB
	JWTSecret string
}

type ScopeIn struct {
	ProjectID  string `json:"project_id"`
	EnvSlug    string `json:"env"`
	Permission string `json:"permission"` // read | write
}

type ScopeOut struct {
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`
	EnvSlug     string `json:"env"`
	Permission  string `json:"permission"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func ipOf(r *http.Request) string { return r.RemoteAddr }

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
	if !auth.Can(s.DB, u, orgID, "", "manage", "") {
		writeErr(w, apperr.Forbidden)
		return
	}
	var b struct {
		Name     string    `json:"name"`
		TokenTTL int       `json:"token_ttl"`
		Scopes   []ScopeIn `json:"scopes"`
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
	// 校验并解析 scope：显式 (project, env, permission)，禁止通配（设计文档 §6）
	type resolved struct {
		projectID, envID, permission string
	}
	resolvedScopes := make([]resolved, 0, len(b.Scopes))
	for _, sc := range b.Scopes {
		if sc.Permission != "read" && sc.Permission != "write" {
			writeErr(w, apperr.New(400, "INVALID", "permission must be read or write"))
			return
		}
		if sc.ProjectID == "" || sc.EnvSlug == "" || sc.EnvSlug == "*" || sc.ProjectID == "*" {
			writeErr(w, apperr.New(400, "INVALID", "wildcard scope not allowed"))
			return
		}
		// env 必须属于 project，project 必须属于本组织
		var envID, orgCheck string
		err := s.DB.QueryRow(`SELECT e.id, p.org_id FROM environments e
			JOIN projects p ON p.id = e.project_id
			WHERE e.project_id = ? AND e.slug = ?`, sc.ProjectID, sc.EnvSlug).Scan(&envID, &orgCheck)
		if err != nil || orgCheck != orgID {
			writeErr(w, apperr.New(400, "INVALID", "invalid scope project/env"))
			return
		}
		resolvedScopes = append(resolvedScopes, resolved{sc.ProjectID, envID, sc.Permission})
	}

	id := tc.NewID()
	clientID := "mi_" + tc.NewID()
	secretBytes := make([]byte, 32)
	_, _ = rand.Read(secretBytes)
	clientSecret := hex.EncodeToString(secretBytes)

	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO machine_identities (id, org_id, name, auth_type, client_id, secret_hash, token_ttl)
		VALUES (?, ?, ?, 'client_credentials', ?, ?, ?)`,
		id, orgID, b.Name, clientID, tc.SHA256(clientSecret), b.TokenTTL); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	for _, rs := range resolvedScopes {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO identity_scopes (identity_id, project_id, env_id, permission)
			VALUES (?, ?, ?, ?)`, id, rs.projectID, rs.envID, rs.permission); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, u, "identity.create", "org/"+slug+"/identity/"+b.Name,
		map[string]any{"client_id": clientID, "scopes": len(resolvedScopes)}, ipOf(r))
	// client_secret 仅本次响应返回一次
	writeJSON(w, 201, map[string]any{
		"id": id, "name": b.Name, "client_id": clientID,
		"client_secret": clientSecret, "token_ttl": b.TokenTTL,
		"scopes": scopeOuts(s.DB, id),
	})
}

// List GET /orgs/:slug/identities
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, client_id, status, token_ttl, created_at
		FROM machine_identities WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type item struct {
		ID        string     `json:"id"`
		Name      string     `json:"name"`
		ClientID  string     `json:"client_id"`
		Status    string     `json:"status"`
		TokenTTL  int        `json:"token_ttl"`
		CreatedAt string     `json:"created_at"`
		Scopes    []ScopeOut `json:"scopes"`
	}
	out := []item{}
	ids := []string{}
	for rows.Next() {
		var it item
		_ = rows.Scan(&it.ID, &it.Name, &it.ClientID, &it.Status, &it.TokenTTL, &it.CreatedAt)
		out = append(out, it)
		ids = append(ids, it.ID)
	}
	rows.Close() // 先释放连接（MaxOpenConns=1），再逐条查 scope，避免池耗尽死锁
	for i := range out {
		out[i].Scopes = scopeOuts(s.DB, ids[i])
	}
	auth.Audit(s.DB, orgID, auth.From(r), "identity.list", "org/"+slug+"/identities", nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"identities": out})
}

// Revoke POST /orgs/:slug/identities/:id/revoke —— 吊销立即生效（loadIdentity 拒绝非 active）
func (s *Service) Revoke(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	u := auth.From(r)
	if !auth.Can(s.DB, u, orgID, "", "manage", "") {
		writeErr(w, apperr.Forbidden)
		return
	}
	id := chi.URLParam(r, "id")
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM machine_identities WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&name); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, err := s.DB.Exec(`UPDATE machine_identities SET status = 'revoked' WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, u, "identity.revoke", "org/"+slug+"/identity/"+name, nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"id": id, "status": "revoked"})
}

// Token POST /identities/token（公开，限流保护）—— client_credentials 换短期 JWT
func (s *Service) Token(w http.ResponseWriter, r *http.Request) {
	var b struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	var id, orgID, name, secretHash string
	var status string
	var ttl int
	err := s.DB.QueryRow(`SELECT id, org_id, name, secret_hash, status, token_ttl
		FROM machine_identities WHERE client_id = ?`, b.ClientID).
		Scan(&id, &orgID, &name, &secretHash, &status, &ttl)
	// 统一错误，不区分“不存在”与“秘密错误”
	if err != nil || status != "active" || !auth.CheckIdentitySecret(b.ClientSecret, secretHash) {
		writeErr(w, apperr.BadCredentials)
		return
	}
	access, err := tc.SignJWTClaims(map[string]any{
		"sub": id, "typ": auth.KindIdentity, "name": name, "org": orgID,
	}, s.JWTSecret, time.Duration(ttl)*time.Second)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, &auth.Actor{ID: id, Name: name, Kind: auth.KindIdentity},
		"identity.token", "identity/"+name, map[string]any{"ttl": ttl}, ipOf(r))
	writeJSON(w, 200, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": ttl,
	})
}

func scopeOuts(db *sql.DB, identityID string) []ScopeOut {
	rows, err := db.Query(`SELECT s.project_id, p.slug, e.slug, s.permission
		FROM identity_scopes s
		JOIN projects p ON p.id = s.project_id
		JOIN environments e ON e.id = s.env_id
		WHERE s.identity_id = ?
		ORDER BY p.slug, e.sort_order`, identityID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ScopeOut{}
	for rows.Next() {
		var sc ScopeOut
		_ = rows.Scan(&sc.ProjectID, &sc.ProjectSlug, &sc.EnvSlug, &sc.Permission)
		out = append(out, sc)
	}
	return out
}
