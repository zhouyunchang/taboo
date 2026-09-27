// OIDC 外部用户源（Keycloak / Authentik / 任意 OIDC）—— 对标 Proxmox Realm
//
//	GET    /api/v1/auth/sources                      登录页可见的实例级 Realm
//	GET    /api/v1/auth/oidc/:orgSlug/login          跳转 IdP（?pid= 指定配置）
//	GET    /api/v1/auth/oidc/:orgSlug/callback       code 换 token，验签，绑定用户，JIT 入组
//	GET    /api/v1/orgs/:slug/oidc                   列表
//	POST   /api/v1/orgs/:slug/oidc                   创建
//	PATCH  /api/v1/orgs/:slug/oidc/:id               更新（不含必填 secret）
//	DELETE /api/v1/orgs/:slug/oidc/:id               删除
//
// 外部用户以验证后的 (issuer, sub) 绑定，不按邮箱链接本地用户。
// 新用户经显式启用的 JIT 入组；SSO 不被视为 MFA 证明。
package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
)

var validRoles = map[string]bool{"viewer": true, "developer": true, "admin": true, "owner": true}

var roleRank = map[string]int{"viewer": 1, "developer": 2, "admin": 3, "owner": 4}

type Service struct {
	DB              *sql.DB
	MasterKey       []byte
	JWTSecret       string
	PublicOrigin    string
	IssuerAllowlist []string
	SessionProfile  string
	DisableRegister bool
	DisablePassword bool
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func ipOf(r *http.Request) string {
	return httpx.ClientIP(r)
}

func newPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// ---------- Provider 模型 ----------

type provider struct {
	Revision      string
	ID            string
	OrgID         string
	OrgSlug       string
	Name          string
	Issuer        string
	ClientID      string
	ClientSecret  string
	Scopes        string
	RoleClaim     string
	UsernameClaim string
	RoleMap       map[string]string
	DefaultRole   string
	PublicLogin   bool
	Autocreate    bool
	SyncRole      bool
	Enabled       bool
}

const providerSelect = `SELECT o.id, o.org_id, g.slug, o.name, o.issuer, o.client_id, o.client_secret_enc,
		o.scopes, o.role_claim, COALESCE(o.username_claim,'email'), o.role_map, o.default_role,
		COALESCE(o.public_login,0), COALESCE(o.autocreate,1), COALESCE(o.sync_role,1), o.enabled
		FROM oidc_providers o JOIN orgs g ON g.id = o.org_id`

func (s *Service) scanProvider(scanner interface {
	Scan(dest ...any) error
}) (*provider, error) {
	var p provider
	var secretEnc, roleMapJSON string
	var pub, auto, syncR, en int
	err := scanner.Scan(&p.ID, &p.OrgID, &p.OrgSlug, &p.Name, &p.Issuer, &p.ClientID, &secretEnc,
		&p.Scopes, &p.RoleClaim, &p.UsernameClaim, &roleMapJSON, &p.DefaultRole,
		&pub, &auto, &syncR, &en)
	if err != nil {
		return nil, err
	}
	p.PublicLogin, p.Autocreate, p.SyncRole, p.Enabled = pub == 1, auto == 1, syncR == 1, en == 1
	p.Revision, err = auth.ProviderRevision(s.DB, p.ID)
	if err != nil {
		return nil, err
	}
	p.RoleMap = map[string]string{}
	_ = json.Unmarshal([]byte(roleMapJSON), &p.RoleMap)
	secret, err := tc.Open(s.MasterKey, secretEnc, tc.Context{Purpose: "oidc-client", Org: p.OrgID, Object: p.ID, Version: 1})
	if err != nil {
		return nil, err
	}
	p.ClientSecret = secret
	return &p, nil
}

func (s *Service) providerByOrgSlug(slug, pid string) (*provider, error) {
	if pid != "" {
		row := s.DB.QueryRow(providerSelect+` WHERE g.slug = ? AND o.id = ? AND o.enabled = 1`, slug, pid)
		return s.scanProvider(row)
	}
	row := s.DB.QueryRow(providerSelect+` WHERE g.slug = ? AND o.enabled = 1 ORDER BY o.created_at LIMIT 1`, slug)
	return s.scanProvider(row)
}

// ---------- 登录流程 ----------

// ListSources GET /api/v1/auth/sources —— 登录页 Realm 列表（仅 public_login）
func (s *Service) ListSources(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`SELECT o.id, o.name, g.slug FROM oidc_providers o
		JOIN orgs g ON g.id = o.org_id WHERE o.enabled = 1 AND o.public_login = 1 ORDER BY o.name`)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type src struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		LoginURL string `json:"login_url"`
	}
	out := []src{}
	for rows.Next() {
		var id, name, slug string
		if err := rows.Scan(&id, &name, &slug); err != nil {
			continue
		}
		out = append(out, src{ID: id, Name: name, LoginURL: "/api/v1/auth/oidc/" + slug + "/login?pid=" + url.QueryEscape(id)})
	}
	writeJSON(w, 200, map[string]any{
		"register_enabled": !s.DisableRegister,
		"password_enabled": !s.DisablePassword,
		"sources":          out,
	})
}

// ---------- 组织级 CRUD ----------

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

func manageOnly(db *sql.DB, w http.ResponseWriter, r *http.Request, orgID string) bool {
	u := auth.From(r)
	if u.Kind != auth.KindUser || !auth.Can(db, u, orgID, "", auth.DelegationAction(u), "") {
		auth.Deny(db, w, r, orgID, auth.DelegationAction(u), "org/oidc")
		return false
	}
	return true
}

func (s *Service) Routes(r chi.Router) {
	r.Route("/orgs/{slug}/oidc", func(r chi.Router) {
		r.Get("/", s.List)
		r.Post("/", s.Create)
		r.Patch("/{id}", s.Update)
		r.Delete("/{id}", s.Delete)
		r.Get("/{id}/security", s.Security)
		r.Put("/{id}/security", s.PutSecurity)
	})
}

type providerOut struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Issuer        string            `json:"issuer"`
	ClientID      string            `json:"client_id"`
	Scopes        string            `json:"scopes"`
	RoleClaim     string            `json:"role_claim"`
	UsernameClaim string            `json:"username_claim"`
	RoleMap       map[string]string `json:"role_map"`
	DefaultRole   string            `json:"default_role"`
	PublicLogin   bool              `json:"public_login"`
	Autocreate    bool              `json:"autocreate"`
	SyncRole      bool              `json:"sync_role"`
	Enabled       bool              `json:"enabled"`
	CreatedAt     string            `json:"created_at"`
	LoginURL      string            `json:"login_url"`
}

func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, issuer, client_id, scopes, role_claim,
		COALESCE(username_claim,'email'), role_map, default_role,
		COALESCE(public_login,0), COALESCE(autocreate,1), COALESCE(sync_role,1), enabled, created_at
		FROM oidc_providers WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []providerOut{}
	for rows.Next() {
		var x providerOut
		var roleMapJSON string
		var pub, auto, syncR, en int
		if err := rows.Scan(&x.ID, &x.Name, &x.Issuer, &x.ClientID, &x.Scopes, &x.RoleClaim,
			&x.UsernameClaim, &roleMapJSON, &x.DefaultRole, &pub, &auto, &syncR, &en, &x.CreatedAt); err != nil {
			continue
		}
		x.PublicLogin, x.Autocreate, x.SyncRole, x.Enabled = pub == 1, auto == 1, syncR == 1, en == 1
		x.RoleMap = map[string]string{}
		_ = json.Unmarshal([]byte(roleMapJSON), &x.RoleMap)
		x.LoginURL = "/api/v1/auth/oidc/" + slug + "/login?pid=" + url.QueryEscape(x.ID)
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"providers": out})
}

type providerInput struct {
	Name          string            `json:"name"`
	Issuer        string            `json:"issuer"`
	ClientID      string            `json:"client_id"`
	ClientSecret  string            `json:"client_secret"`
	Scopes        string            `json:"scopes"`
	RoleClaim     string            `json:"role_claim"`
	UsernameClaim string            `json:"username_claim"`
	RoleMap       map[string]string `json:"role_map"`
	DefaultRole   string            `json:"default_role"`
	PublicLogin   *bool             `json:"public_login"`
	Autocreate    *bool             `json:"autocreate"`
	SyncRole      *bool             `json:"sync_role"`
	Enabled       *bool             `json:"enabled"`
}

func boolDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	var b providerInput
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Name == "" || b.Issuer == "" ||
		b.ClientID == "" || b.ClientSecret == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if err := normalizeProviderInput(&b, true); err != nil {
		writeErr(w, err)
		return
	}
	if !s.allowedIssuer(b.Issuer) {
		writeErr(w, apperr.New(400, "ISSUER_NOT_ALLOWED", "ask the operator to allow this exact issuer"))
		return
	}
	if _, _, err := discoCache.get("probe-"+tc.NewID(), b.Issuer); err != nil {
		writeErr(w, apperr.New(400, "IDP_UNREACHABLE", "identity provider unavailable or incompatible"))
		return
	}
	id := tc.NewID()
	secretEnc, err := tc.Seal(s.MasterKey, b.ClientSecret, tc.Context{Purpose: "oidc-client", Org: orgID, Object: id, Version: 1})
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	roleMapJSON, _ := json.Marshal(b.RoleMap)
	pub, auto, syncR := btoi(boolDefault(b.PublicLogin, true)), btoi(boolDefault(b.Autocreate, false)), btoi(boolDefault(b.SyncRole, true))
	if err := s.mutateProvider(r, orgID, "oidc.create", "org/"+slug+"/oidc/"+id, `INSERT INTO oidc_providers
		(id, org_id, name, issuer, client_id, client_secret_enc, scopes, role_claim, role_map, default_role,
		 username_claim, public_login, autocreate, sync_role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, orgID, b.Name, b.Issuer, b.ClientID, secretEnc,
		b.Scopes, b.RoleClaim, string(roleMapJSON), b.DefaultRole,
		b.UsernameClaim, pub, auto, syncR); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": b.Name})
}

func (s *Service) Update(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	id := chi.URLParam(r, "id")
	var name, existingIssuer, existingClient string
	if err := s.DB.QueryRow(`SELECT name,issuer,client_id FROM oidc_providers WHERE id = ? AND org_id = ?`, id, orgID).Scan(&name, &existingIssuer, &existingClient); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var b providerInput
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if err := normalizeProviderInput(&b, false); err != nil {
		writeErr(w, err)
		return
	}
	if b.Issuer != "" && b.Issuer != existingIssuer || b.ClientID != "" && b.ClientID != existingClient {
		var n int
		if err := s.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM federation_admissions WHERE provider_id=?)+(SELECT COUNT(*) FROM users WHERE auth_source=?)`, id, id).Scan(&n); err != nil || n > 0 {
			writeErr(w, apperr.New(409, "NEW_CONNECTION_REQUIRED", "create a new connection and prove identity replacement"))
			return
		}
	}
	if b.Issuer != "" {
		if !s.allowedIssuer(b.Issuer) {
			writeErr(w, apperr.New(400, "ISSUER_NOT_ALLOWED", "ask the operator to allow this exact issuer"))
			return
		}
		if _, _, err := discoCache.get("probe-"+tc.NewID(), b.Issuer); err != nil {
			writeErr(w, apperr.New(400, "IDP_UNREACHABLE", "identity provider unavailable or incompatible"))
			return
		}
		discoCache.drop(id)
	}
	sets := []string{
		"name = COALESCE(NULLIF(?, ''), name)",
		"issuer = COALESCE(NULLIF(?, ''), issuer)",
		"client_id = COALESCE(NULLIF(?, ''), client_id)",
		"scopes = COALESCE(NULLIF(?, ''), scopes)",
		"role_claim = COALESCE(NULLIF(?, ''), role_claim)",
		"username_claim = COALESCE(NULLIF(?, ''), username_claim)",
		"default_role = COALESCE(NULLIF(?, ''), default_role)",
	}
	args := []any{b.Name, b.Issuer, b.ClientID, b.Scopes, b.RoleClaim, b.UsernameClaim, b.DefaultRole}
	if b.RoleMap != nil {
		encoded, _ := json.Marshal(b.RoleMap)
		sets = append(sets, "role_map = ?")
		args = append(args, string(encoded))
	}
	if b.ClientSecret != "" {
		enc, err := tc.Seal(s.MasterKey, b.ClientSecret, tc.Context{Purpose: "oidc-client", Org: orgID, Object: id, Version: 1})
		if err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		sets = append(sets, "client_secret_enc = ?")
		args = append(args, enc)
	}
	if b.PublicLogin != nil {
		sets = append(sets, "public_login = ?")
		args = append(args, btoi(*b.PublicLogin))
	}
	if b.Autocreate != nil {
		sets = append(sets, "autocreate = ?")
		args = append(args, btoi(*b.Autocreate))
	}
	if b.SyncRole != nil {
		sets = append(sets, "sync_role = ?")
		args = append(args, btoi(*b.SyncRole))
	}
	if b.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, btoi(*b.Enabled))
	}
	args = append(args, id, orgID)
	if err := s.mutateProvider(r, orgID, "oidc.update", "org/"+slug+"/oidc/"+name, `UPDATE oidc_providers SET `+strings.Join(sets, ", ")+` WHERE id = ? AND org_id = ?`, args...); err != nil {
		if e, ok := err.(*apperr.Error); ok {
			writeErr(w, e)
		} else {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "connection unchanged"))
		}
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "updated": true})
}

func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	id := chi.URLParam(r, "id")
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM oidc_providers WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&name); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if err := s.mutateProvider(r, orgID, "oidc.delete", "org/"+slug+"/oidc/"+name, `DELETE FROM oidc_providers WHERE id = ?`, id); err != nil {
		if e, ok := err.(*apperr.Error); ok {
			writeErr(w, e)
		} else {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "connection unchanged"))
		}
		return
	}
	discoCache.drop(id)
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

func normalizeProviderInput(b *providerInput, creating bool) *apperr.Error {
	if b.Issuer != "" {
		if !strings.HasPrefix(b.Issuer, "https://") && !strings.HasPrefix(b.Issuer, "http://") {
			return apperr.New(400, "INVALID", "issuer must be a URL")
		}
		// Issuer comparisons are exact; do not silently normalize trailing slashes.
	}
	if b.Scopes == "" && creating {
		b.Scopes = "openid,email,profile"
	}
	if b.RoleClaim == "" && creating {
		b.RoleClaim = "groups"
	}
	if b.UsernameClaim == "" && creating {
		b.UsernameClaim = "email"
	}
	if b.DefaultRole == "" && creating {
		b.DefaultRole = "viewer"
	}
	if b.DefaultRole != "" && !validRoles[b.DefaultRole] {
		return apperr.New(400, "INVALID", "default_role must be viewer/developer/admin/owner")
	}
	if b.RoleMap == nil && creating {
		b.RoleMap = map[string]string{}
	}
	for _, r := range b.RoleMap {
		if !validRoles[r] {
			return apperr.New(400, "INVALID", "role_map target must be viewer/developer/admin/owner")
		}
	}
	return nil
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Service) allowedIssuer(issuer string) bool {
	for _, allowed := range s.IssuerAllowlist {
		if allowed == issuer {
			return true
		}
	}
	return false
}

// Configuration mutation and its audit record have one commit boundary.
func (s *Service) mutateProvider(r *http.Request, org, action, resource, query string, args ...any) error {
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE orgs SET name=name WHERE id=?`, org); err != nil {
		return err
	}
	if _, err = tx.ExecContext(r.Context(), query, args...); err != nil {
		return err
	}
	if !auth.ExternalOwnerAvailable(tx, org) {
		return apperr.New(409, "EXTERNAL_OWNER_REQUIRED", "another proven external owner is required before removing this connection")
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), action, resource, nil); err != nil {
		return err
	}
	return tx.Commit()
}
