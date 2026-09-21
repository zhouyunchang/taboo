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
// 用户以 IdP `sub` 为稳定主键绑定；密码登录与 SSO 可并存（本地账号被链接后仍可用密码）。
// 新用户经 JIT 创建时不可映射为 owner。IdP 登录视为已完成二次校验。
package oidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
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
	DB              *sql.DB
	MasterKey       []byte
	JWTSecret       string
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
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	if host != "" {
		return host
	}
	return r.RemoteAddr
}

// ---------- OpenID Discovery + JWKS（内存缓存 1h） ----------

type discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	Issuer                string `json:"issuer"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
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

func (s *Service) verifyIDToken(raw, issuer, clientID, nonce string, ks *jwks) (map[string]any, error) {
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
	if nonce != "" {
		got, _ := claims["nonce"].(string)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(nonce)) != 1 {
			return nil, errors.New("id_token nonce mismatch")
		}
	}
	return claims, nil
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
	p.RoleMap = map[string]string{}
	_ = json.Unmarshal([]byte(roleMapJSON), &p.RoleMap)
	secret, err := tc.Decrypt(s.MasterKey, secretEnc)
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

func schemeHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	p, err := s.providerByOrgSlug(slug, r.URL.Query().Get("pid"))
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	doc, _, err := discoCache.get(p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", err.Error()))
		return
	}
	verifier, challenge, err := newPKCE()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	nonce := tc.NewID()
	state, err := tc.SignJWTClaims(map[string]any{
		"typ": "oidc_state", "pid": p.ID, "org": p.OrgID, "slug": slug,
		"nonce": nonce, "cv": verifier,
	}, s.JWTSecret, 10*time.Minute)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	redirectURI := schemeHost(r) + "/api/v1/auth/oidc/" + slug + "/callback"
	q := url.Values{
		"client_id":             {p.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {oauthScopes(p.Scopes)},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, doc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
}

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
	pid, _ := stateClaims["pid"].(string)
	p, err := s.providerByOrgSlug(slug, pid)
	if err != nil || p.ID != pid {
		writeErr(w, apperr.NotFound)
		return
	}
	doc, ks, err := discoCache.get(p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", err.Error()))
		return
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {q.Get("code")},
		"redirect_uri":  {schemeHost(r) + "/api/v1/auth/oidc/" + slug + "/callback"},
		"client_id":     {p.ClientID},
		"code_verifier": {strClaim(stateClaims, "cv")},
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
	nonce := strClaim(stateClaims, "nonce")
	claims, err := s.verifyIDToken(tok.IDToken, p.Issuer, p.ClientID, nonce, ks)
	if err != nil {
		writeErr(w, apperr.New(401, "ID_TOKEN_INVALID", err.Error()))
		return
	}
	sub := claimString(claims, "sub")
	if sub == "" {
		writeErr(w, apperr.New(400, "NO_SUBJECT", "id_token missing sub"))
		return
	}
	email := identityEmail(claims, p)
	name := claimString(claims, "name", "preferred_username", "email")
	if name == "" {
		name = email
	}
	userID, isNew, bindErr := s.bindUser(p, sub, email, name)
	if bindErr != nil {
		writeErr(w, bindErr)
		return
	}
	role := capJITRole(MapRole(claims, p.RoleClaim, p.RoleMap, p.DefaultRole))
	if memErr := s.ensureMembership(p, userID, role, isNew); memErr != nil {
		writeErr(w, memErr)
		return
	}
	u := &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser}
	auth.Audit(s.DB, p.OrgID, u, "auth.oidc.login", "user/"+email,
		map[string]any{"provider": p.Name, "jit_role": role, "new_user": isNew, "sub": sub}, ipOf(r))
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	logoutURL := ""
	if doc.EndSessionEndpoint != "" {
		lq := url.Values{
			"client_id":                {p.ClientID},
			"id_token_hint":            {tok.IDToken},
			"post_logout_redirect_uri": {schemeHost(r) + "/"},
		}
		logoutURL = doc.EndSessionEndpoint + "?" + lq.Encode()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	payload, _ := json.Marshal(map[string]string{"access": tk.Access, "refresh": tk.Refresh, "logout": logoutURL})
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>taboo SSO 登录成功</title>
<script>
var t = ` + string(payload) + `;
localStorage.setItem('taboo.access', t.access);
localStorage.setItem('taboo.refresh', t.refresh);
if (t.logout) localStorage.setItem('taboo.idp_logout', t.logout);
else localStorage.removeItem('taboo.idp_logout');
location.href = '/';
</script><p>SSO 登录成功，正在跳转…</p>`))
}

func strClaim(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func identityEmail(claims map[string]any, p *provider) string {
	keys := []string{p.UsernameClaim, "email", "preferred_username"}
	v := strings.ToLower(claimString(claims, keys...))
	if strings.Contains(v, "@") {
		return v
	}
	sub := claimString(claims, "sub")
	host := "idp.local"
	if u, err := url.Parse(p.Issuer); err == nil && u.Host != "" {
		host = u.Host
	}
	if sub == "" {
		sub = "user"
	}
	return strings.ToLower(sub) + "@" + host
}

func (s *Service) bindUser(p *provider, sub, email, name string) (userID string, isNew bool, err *apperr.Error) {
	errScan := s.DB.QueryRow(`SELECT id FROM users WHERE auth_source = ? AND external_sub = ?`, p.ID, sub).Scan(&userID)
	if errScan == nil {
		_, _ = s.DB.Exec(`UPDATE users SET name = ?, email = ? WHERE id = ?`, name, email, userID)
		return userID, false, nil
	}
	if !errors.Is(errScan, sql.ErrNoRows) {
		return "", false, apperr.New(500, "INTERNAL", errScan.Error())
	}
	var src string
	errScan = s.DB.QueryRow(`SELECT id, auth_source FROM users WHERE email = ?`, email).Scan(&userID, &src)
	if errScan == nil {
		if src != "local" && src != p.ID {
			return "", false, apperr.New(409, "IDENTITY_CONFLICT", "email already bound to another identity source")
		}
		if _, e := s.DB.Exec(`UPDATE users SET name = ?, auth_source = ?, external_sub = ? WHERE id = ?`,
			name, p.ID, sub, userID); e != nil {
			return "", false, apperr.New(500, "INTERNAL", e.Error())
		}
		return userID, false, nil
	}
	if !errors.Is(errScan, sql.ErrNoRows) {
		return "", false, apperr.New(500, "INTERNAL", errScan.Error())
	}
	if !p.Autocreate {
		return "", false, apperr.New(403, "AUTOCREATE_DISABLED", "this identity source does not auto-create users")
	}
	userID = tc.NewID()
	hash, _ := tc.HashPassword(tc.NewID() + tc.NewID())
	if _, e := s.DB.Exec(`INSERT INTO users (id, email, name, password_hash, auth_source, external_sub)
		VALUES (?, ?, ?, ?, ?, ?)`, userID, email, name, hash, p.ID, sub); e != nil {
		return "", false, apperr.New(500, "INTERNAL", e.Error())
	}
	return userID, true, nil
}

func (s *Service) ensureMembership(p *provider, userID, role string, isNew bool) *apperr.Error {
	var current string
	err := s.DB.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, p.OrgID, userID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.DB.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)`,
			p.OrgID, userID, role); e != nil {
			return apperr.New(500, "INTERNAL", e.Error())
		}
		return nil
	}
	if err != nil {
		return apperr.New(500, "INTERNAL", err.Error())
	}
	if !p.SyncRole || current == role || current == "owner" {
		return nil
	}
	var owners int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = ? AND role = 'owner'`, p.OrgID).Scan(&owners)
	if current == "owner" && owners <= 1 {
		return nil
	}
	if _, e := s.DB.Exec(`UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ?`, role, p.OrgID, userID); e != nil {
		return apperr.New(500, "INTERNAL", e.Error())
	}
	_ = isNew
	return nil
}

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
	if u.Kind != auth.KindUser || !auth.Can(db, u, orgID, "", "manage", "") {
		writeErr(w, apperr.Forbidden)
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
	pub, auto, syncR := btoi(boolDefault(b.PublicLogin, true)), btoi(boolDefault(b.Autocreate, true)), btoi(boolDefault(b.SyncRole, true))
	if _, err := s.DB.Exec(`INSERT INTO oidc_providers
		(id, org_id, name, issuer, client_id, client_secret_enc, scopes, role_claim, role_map, default_role,
		 username_claim, public_login, autocreate, sync_role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, orgID, b.Name, b.Issuer, b.ClientID, secretEnc,
		b.Scopes, b.RoleClaim, string(roleMapJSON), b.DefaultRole,
		b.UsernameClaim, pub, auto, syncR); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "oidc.create", "org/"+slug+"/oidc/"+b.Name,
		map[string]any{"issuer": b.Issuer, "default_role": b.DefaultRole, "public_login": pub == 1}, ipOf(r))
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
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM oidc_providers WHERE id = ? AND org_id = ?`, id, orgID).Scan(&name); err != nil {
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
	if b.Issuer != "" {
		if _, _, err := discoCache.get("probe-"+tc.NewID(), b.Issuer); err != nil {
			writeErr(w, apperr.New(400, "IDP_UNREACHABLE", err.Error()))
			return
		}
		discoCache.drop(id)
	}
	roleMapJSON, _ := json.Marshal(b.RoleMap)
	sets := []string{
		"name = COALESCE(NULLIF(?, ''), name)",
		"issuer = COALESCE(NULLIF(?, ''), issuer)",
		"client_id = COALESCE(NULLIF(?, ''), client_id)",
		"scopes = COALESCE(NULLIF(?, ''), scopes)",
		"role_claim = COALESCE(NULLIF(?, ''), role_claim)",
		"username_claim = COALESCE(NULLIF(?, ''), username_claim)",
		"role_map = ?",
		"default_role = COALESCE(NULLIF(?, ''), default_role)",
	}
	args := []any{b.Name, b.Issuer, b.ClientID, b.Scopes, b.RoleClaim, b.UsernameClaim, string(roleMapJSON), b.DefaultRole}
	if b.ClientSecret != "" {
		enc, err := tc.Encrypt(s.MasterKey, b.ClientSecret)
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
	if _, err := s.DB.Exec(`UPDATE oidc_providers SET `+strings.Join(sets, ", ")+` WHERE id = ? AND org_id = ?`, args...); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "oidc.update", "org/"+slug+"/oidc/"+name, nil, ipOf(r))
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
	if _, err := s.DB.Exec(`DELETE FROM oidc_providers WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	discoCache.drop(id)
	auth.Audit(s.DB, orgID, auth.From(r), "oidc.delete", "org/"+slug+"/oidc/"+name, nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

func normalizeProviderInput(b *providerInput, creating bool) *apperr.Error {
	if b.Issuer != "" {
		if !strings.HasPrefix(b.Issuer, "https://") && !strings.HasPrefix(b.Issuer, "http://") {
			return apperr.New(400, "INVALID", "issuer must be a URL")
		}
		b.Issuer = strings.TrimSuffix(b.Issuer, "/")
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
	if b.RoleMap == nil {
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
