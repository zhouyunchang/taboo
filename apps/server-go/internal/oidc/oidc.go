// OIDC SSO（M5 #13）—— 组织级 IdP 配置 + 登录流程 + JIT 入组 + 角色映射
//
//	GET    /api/v1/auth/oidc/:orgSlug/login      跳转 IdP 授权页（公开，限流）
//	GET    /api/v1/auth/oidc/:orgSlug/callback   code 换 token，验签 id_token，JIT 入组
//	GET    /api/v1/orgs/:slug/oidc               列表（不含 client_secret）
//	POST   /api/v1/orgs/:slug/oidc               创建（owner）
//	DELETE /api/v1/orgs/:slug/oidc/:id           删除（owner）
//
// 与邮箱密码、2FA 共存：OIDC 登录视为 IdP 已完成二次校验，不再要求本地 TOTP。
package oidc

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

var validRoles = map[string]bool{"viewer": true, "developer": true, "admin": true, "owner": true}

var roleRank = map[string]int{"viewer": 1, "developer": 2, "admin": 3, "owner": 4}

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	JWTSecret string
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func ipOf(r *http.Request) string { return r.RemoteAddr }

// ---------- OpenID Discovery + JWKS（内存缓存 1h） ----------

type discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	Issuer                string `json:"issuer"`
}

type cachedEntry struct {
	doc       *discovery
	jwks      *jwks
	fetchedAt time.Time
}

type cache struct {
	mu   sync.Mutex
	data map[string]*cachedEntry // providerID → entry
}

func newCache() *cache { return &cache{data: map[string]*cachedEntry{}} }

var discoCache = newCache()

func (c *cache) get(providerID, issuer string) (*discovery, *jwks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.data[providerID]; ok && time.Since(e.fetchedAt) < time.Hour {
		return e.doc, e.jwks, nil
	}
	doc, err := fetchDiscovery(issuer)
	if err != nil {
		return nil, nil, err
	}
	ks, err := fetchJWKS(doc.JWKSURI)
	if err != nil {
		return nil, nil, err
	}
	c.data[providerID] = &cachedEntry{doc: doc, jwks: ks, fetchedAt: time.Now()}
	return doc, ks, nil
}

func (c *cache) drop(providerID string) {
	c.mu.Lock()
	delete(c.data, providerID)
	c.mu.Unlock()
}

func fetchDiscovery(issuer string) (*discovery, error) {
	u := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, errors.New("oidc discovery: HTTP " + res.Status)
	}
	var doc discovery
	if err := json.NewDecoder(io.LimitReader(res.Body, 256*1024)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return nil, errors.New("oidc discovery: incomplete metadata")
	}
	return &doc, nil
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
}

func fetchJWKS(jwksURI string) (*jwks, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Get(jwksURI)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, errors.New("oidc jwks: HTTP " + res.Status)
	}
	var ks jwks
	if err := json.NewDecoder(io.LimitReader(res.Body, 512*1024)).Decode(&ks); err != nil {
		return nil, err
	}
	return &ks, nil
}

// rsaPublicKey JWK(n,e) → *rsa.PublicKey
func (k *jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	if k.Kty != "RSA" {
		return nil, errors.New("unsupported jwk kty")
	}
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
}

// verifyIDToken 校验 RS256 签名 + iss/aud/exp；返回 claims
func (s *Service) verifyIDToken(raw, issuer, clientID string, ks *jwks) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id_token")
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hdrJSON, &hdr) != nil {
		return nil, errors.New("malformed id_token header")
	}
	if hdr.Alg != "RS256" {
		return nil, errors.New("unsupported id_token alg: " + hdr.Alg)
	}
	var key *rsa.PublicKey
	for i := range ks.Keys {
		if ks.Keys[i].Kid == hdr.Kid || (hdr.Kid == "" && ks.Keys[i].Use == "sig") {
			if pk, err := ks.Keys[i].rsaPublicKey(); err == nil {
				key = pk
				break
			}
		}
	}
	if key == nil {
		return nil, errors.New("no matching jwk")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return nil, err
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	if iss, _ := claims["iss"].(string); iss != issuer {
		return nil, errors.New("id_token issuer mismatch")
	}
	audOK := false
	switch aud := claims["aud"].(type) {
	case string:
		audOK = aud == clientID
	case []any:
		for _, a := range aud {
			if a == clientID {
				audOK = true
			}
		}
	}
	if !audOK {
		return nil, errors.New("id_token audience mismatch")
	}
	if exp, ok := claims["exp"].(float64); !ok || int64(exp) < time.Now().Unix() {
		return nil, errors.New("id_token expired")
	}
	return claims, nil
}

// ---------- Provider 模型 ----------

type provider struct {
	ID             string
	OrgID          string
	Name           string
	Issuer         string
	ClientID       string
	ClientSecret   string // 解密后
	Scopes         string
	RoleClaim      string
	RoleMap        map[string]string // group/claim 值 → 角色
	DefaultRole    string
	Enabled        bool
}

func (s *Service) providerByOrgSlug(slug string) (*provider, error) {
	var p provider
	var secretEnc string
	var roleMapJSON string
	var en int
	err := s.DB.QueryRow(`SELECT o.id, o.org_id, o.name, o.issuer, o.client_id, o.client_secret_enc,
		o.scopes, o.role_claim, o.role_map, o.default_role, o.enabled
		FROM oidc_providers o JOIN orgs g ON g.id = o.org_id
		WHERE g.slug = ? AND o.enabled = 1 ORDER BY o.created_at LIMIT 1`, slug).
		Scan(&p.ID, &p.OrgID, &p.Name, &p.Issuer, &p.ClientID, &secretEnc,
			&p.Scopes, &p.RoleClaim, &roleMapJSON, &p.DefaultRole, &en)
	if err != nil {
		return nil, err
	}
	p.Enabled = en == 1
	p.RoleMap = map[string]string{}
	_ = json.Unmarshal([]byte(roleMapJSON), &p.RoleMap)
	secret, err := tc.Decrypt(s.MasterKey, secretEnc)
	if err != nil {
		return nil, err
	}
	p.ClientSecret = secret
	return &p, nil
}

// ---------- 登录流程 ----------

func schemeHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// PublicRoutes 挂载公开登录端点（登录同窗口限流由调用方加）
func (s *Service) PublicRoutes(r chi.Router) {
	r.Route("/auth/oidc/{slug}", func(r chi.Router) {
		r.Get("/login", s.Login)
		r.Get("/callback", s.Callback)
	})
}

// Login GET /login —— 302 跳转到 IdP 授权端点
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	p, err := s.providerByOrgSlug(chi.URLParam(r, "slug"))
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	doc, _, err := discoCache.get(p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", err.Error()))
		return
	}
	state, err := tc.SignJWTClaims(map[string]any{
		"typ": "oidc_state", "pid": p.ID, "org": p.OrgID, "nonce": tc.NewID(),
	}, s.JWTSecret, 10*time.Minute)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	redirectURI := schemeHost(r) + "/api/v1/auth/oidc/" + chi.URLParam(r, "slug") + "/callback"
	q := url.Values{
		"client_id":     {p.ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {p.Scopes},
		"state":         {state},
	}
	http.Redirect(w, r, doc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
}

// Callback GET /callback —— 换 token + 验签 + JIT 入组 + 发本地会话
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errCode := q.Get("error"); errCode != "" {
		writeErr(w, apperr.New(400, "OIDC_ERROR", errCode+": "+q.Get("error_description")))
		return
	}
	stateClaims, err := tc.VerifyJWT(q.Get("state"), s.JWTSecret)
	if err != nil || stateClaims["typ"] != "oidc_state" {
		writeErr(w, apperr.New(400, "BAD_STATE", "invalid or expired state"))
		return
	}
	slug := chi.URLParam(r, "slug")
	p, err := s.providerByOrgSlug(slug)
	if err != nil || p.ID != stateClaims["pid"] {
		writeErr(w, apperr.NotFound)
		return
	}
	doc, ks, err := discoCache.get(p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", err.Error()))
		return
	}
	// code → token
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {q.Get("code")},
		"redirect_uri": {schemeHost(r) + "/api/v1/auth/oidc/" + slug + "/callback"},
	}
	req, err := http.NewRequest("POST", doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.ClientID), url.QueryEscape(p.ClientSecret))
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", err.Error()))
		return
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		writeErr(w, apperr.New(502, "TOKEN_EXCHANGE", string(body)))
		return
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil || tok.IDToken == "" {
		writeErr(w, apperr.New(502, "TOKEN_EXCHANGE", "no id_token in response"))
		return
	}
	claims, err := s.verifyIDToken(tok.IDToken, p.Issuer, p.ClientID, ks)
	if err != nil {
		writeErr(w, apperr.New(401, "ID_TOKEN_INVALID", err.Error()))
		return
	}
	email, _ := claims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		writeErr(w, apperr.New(400, "NO_EMAIL", "id_token missing email claim"))
		return
	}
	name, _ := claims["name"].(string)
	if name == "" {
		name = email
	}
	// JIT：用户不存在则创建（随机密码占位，无法密码登录）
	var userID string
	err = s.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID)
	isNewUser := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNewUser {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if isNewUser {
		userID = tc.NewID()
		hash, _ := tc.HashPassword(tc.NewID() + tc.NewID()) // 不可知随机密码：仅 SSO 可登录
		if _, err := s.DB.Exec(`INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)`,
			userID, email, name, hash); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	// JIT 入组：按 claim 映射角色（取命中中最高角色），默认 viewer；已是成员则保留原角色
	role := mapRole(claims, p.RoleClaim, p.RoleMap, p.DefaultRole)
	_, member := auth.Membership(s.DB, p.OrgID, userID)
	if !member {
		if _, err := s.DB.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)`,
			p.OrgID, userID, role); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	u := &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser}
	auth.Audit(s.DB, p.OrgID, u, "auth.oidc.login", "user/"+email,
		map[string]any{"provider": p.Name, "jit_role": role, "new_user": isNewUser}, ipOf(r))
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// 回写前端会话：SPA 从 localStorage 读 token，用中间页落盘后跳首页
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	b, _ := json.Marshal(map[string]string{"access": tk.Access, "refresh": tk.Refresh})
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>taboo SSO 登录成功</title>
<script>
var t = ` + string(b) + `;
localStorage.setItem('taboo.access', t.access);
localStorage.setItem('taboo.refresh', t.refresh);
location.href = '/';
</script><p>SSO 登录成功，正在跳转…</p>`))
}

// mapRole claim → 角色：角色映射命中取最高角色，否则默认角色（viewer）
func mapRole(claims map[string]any, claim string, roleMap map[string]string, def string) string {
	best := ""
	if def != "" && validRoles[def] {
		best = def
	} else {
		best = "viewer"
	}
	bestRank := roleRank[best]
	var values []string
	switch v := claims[claim].(type) {
	case string:
		values = []string{v}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				values = append(values, s)
			}
		}
	}
	for _, gv := range values {
		if r, ok := roleMap[gv]; ok && validRoles[r] && roleRank[r] > bestRank {
			best, bestRank = r, roleRank[r]
		}
	}
	return best
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
	if u.Kind != auth.KindUser || !auth.Can(db, u, orgID, "", "manage", "") {
		writeErr(w, apperr.Forbidden)
		return false
	}
	return true
}

// Routes 组织级配置（需登录）
func (s *Service) Routes(r chi.Router) {
	r.Route("/orgs/{slug}/oidc", func(r chi.Router) {
		r.Get("/", s.List)
		r.Post("/", s.Create)
		r.Delete("/{id}", s.Delete)
	})
}

type providerOut struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Issuer      string            `json:"issuer"`
	ClientID    string            `json:"client_id"`
	Scopes      string            `json:"scopes"`
	RoleClaim   string            `json:"role_claim"`
	RoleMap     map[string]string `json:"role_map"`
	DefaultRole string            `json:"default_role"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   string            `json:"created_at"`
	LoginURL    string            `json:"login_url"` // /api/v1/auth/oidc/{slug}/login
}

// List GET /
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, issuer, client_id, scopes, role_claim, role_map,
		default_role, enabled, created_at FROM oidc_providers WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []providerOut{}
	for rows.Next() {
		var x providerOut
		var roleMapJSON string
		var en int
		if err := rows.Scan(&x.ID, &x.Name, &x.Issuer, &x.ClientID, &x.Scopes, &x.RoleClaim,
			&roleMapJSON, &x.DefaultRole, &en, &x.CreatedAt); err != nil {
			continue
		}
		x.Enabled = en == 1
		x.RoleMap = map[string]string{}
		_ = json.Unmarshal([]byte(roleMapJSON), &x.RoleMap)
		x.LoginURL = "/api/v1/auth/oidc/" + slug + "/login"
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"providers": out})
}

// Create POST /
func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	var b struct {
		Name        string            `json:"name"`
		Issuer      string            `json:"issuer"`
		ClientID    string            `json:"client_id"`
		ClientSecret string           `json:"client_secret"`
		Scopes      string            `json:"scopes"`
		RoleClaim   string            `json:"role_claim"`
		RoleMap     map[string]string `json:"role_map"`
		DefaultRole string            `json:"default_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Name == "" || b.Issuer == "" ||
		b.ClientID == "" || b.ClientSecret == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if !strings.HasPrefix(b.Issuer, "https://") && !strings.HasPrefix(b.Issuer, "http://") {
		writeErr(w, apperr.New(400, "INVALID", "issuer must be a URL"))
		return
	}
	if b.Scopes == "" {
		b.Scopes = "openid,email,profile"
	}
	if b.RoleClaim == "" {
		b.RoleClaim = "groups"
	}
	if b.DefaultRole == "" {
		b.DefaultRole = "viewer"
	}
	if !validRoles[b.DefaultRole] {
		writeErr(w, apperr.New(400, "INVALID", "default_role must be viewer/developer/admin/owner"))
		return
	}
	for _, r := range b.RoleMap {
		if !validRoles[r] {
			writeErr(w, apperr.New(400, "INVALID", "role_map target must be viewer/developer/admin/owner"))
			return
		}
	}
	// 连通性自检：discovery 可达（缓存预热）
	if _, _, err := discoCache.get("probe-"+tc.NewID(), b.Issuer); err != nil {
		writeErr(w, apperr.New(400, "IDP_UNREACHABLE", err.Error()))
		return
	}
	secretEnc, err := tc.Encrypt(s.MasterKey, b.ClientSecret)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	roleMapJSON, _ := json.Marshal(b.RoleMap)
	id := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO oidc_providers
		(id, org_id, name, issuer, client_id, client_secret_enc, scopes, role_claim, role_map, default_role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, orgID, b.Name, strings.TrimSuffix(b.Issuer, "/"), b.ClientID, secretEnc,
		b.Scopes, b.RoleClaim, string(roleMapJSON), b.DefaultRole); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "oidc.create", "org/"+slug+"/oidc/"+b.Name,
		map[string]any{"issuer": b.Issuer, "default_role": b.DefaultRole}, ipOf(r))
	writeJSON(w, 201, map[string]any{"id": id, "name": b.Name})
}

// Delete DELETE /{id}
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
	if _, err := s.DB.Exec(`DELETE FROM oidc_providers WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	discoCache.drop(id)
	auth.Audit(s.DB, orgID, auth.From(r), "oidc.delete", "org/"+slug+"/oidc/"+name, nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}
