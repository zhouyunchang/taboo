package github

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
)

const (
	basePath     = "/api/v1/auth/github"
	flowCookie   = "taboo.github.flow"
	ticketCookie = "taboo.github.ticket"
	flowTTL      = 10 * time.Minute
	ticketTTL    = 60 * time.Second
)

// Service serves /api/v1/auth/github/*. DB must be the trusted control pool
// (public pre-authentication routes, same as password login).
type Service struct {
	Cfg          Config
	DB           *sql.DB
	MasterKey    []byte
	JWTSecret    string
	PublicOrigin string
	HTTP         *http.Client
}

func (s *Service) Enabled() bool { return s != nil && s.Cfg.Enabled() && s.PublicOrigin != "" }

func (s *Service) CallbackURL() string {
	return strings.TrimRight(s.PublicOrigin, "/") + basePath + "/callback"
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

// internalErr logs the cause server-side and returns a generic 500 (no DB details to clients).
func internalErr(w http.ResponseWriter, err error) {
	slog.Error("github auth internal error", "err", err)
	writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
}

var errDisabled = apperr.New(404, "GITHUB_DISABLED", "GitHub login is not configured")

// ---------- sealed cookies ----------

type flowState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Mode     string `json:"m"` // login | link
	User     string `json:"u,omitempty"`
	Exp      int64  `json:"e"`
}

type ticket struct {
	User  string `json:"u"`
	Nonce string `json:"n"`
	Exp   int64  `json:"e"`
}

func (s *Service) seal(kind string, v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return tc.Seal(s.MasterKey, string(b), tc.Context{Purpose: "github-" + kind, Object: kind, Version: 1})
}

func (s *Service) open(kind, payload string, v any) error {
	plain, err := tc.Open(s.MasterKey, payload, tc.Context{Purpose: "github-" + kind, Object: kind, Version: 1})
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(plain), v)
}

func (s *Service) secure() bool { return strings.HasPrefix(s.PublicOrigin, "https://") }

func (s *Service) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration, site http.SameSite) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: basePath, HttpOnly: true, Secure: s.secure(),
		SameSite: site, MaxAge: int(ttl.Seconds())})
}

func (s *Service) clearCookie(w http.ResponseWriter, name string, site http.SameSite) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: basePath, HttpOnly: true, Secure: s.secure(), SameSite: site, MaxAge: -1})
}

// ---------- authorization request ----------

func (s *Service) authorizationURL(w http.ResponseWriter, mode, user string) (string, error) {
	state, err := auth.RandomToken()
	if err != nil {
		return "", err
	}
	verifier, err := auth.RandomToken()
	if err != nil {
		return "", err
	}
	sealed, err := s.seal("flow", flowState{State: state, Verifier: verifier, Mode: mode, User: user, Exp: time.Now().Add(flowTTL).Unix()})
	if err != nil {
		return "", err
	}
	// Lax: the cookie must accompany GitHub's top-level redirect back to the callback.
	s.setCookie(w, flowCookie, sealed, flowTTL, http.SameSiteLaxMode)
	sum := sha256.Sum256([]byte(verifier))
	scope := "read:user user:email"
	if s.Cfg.needsOrgScope() {
		scope += " read:org"
	}
	q := url.Values{
		"client_id":             {s.Cfg.ClientID},
		"redirect_uri":          {s.CallbackURL()},
		"scope":                 {scope},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"false"},
	}
	return s.Cfg.OAuthURL + "/login/oauth/authorize?" + q.Encode(), nil
}

// Login starts "Sign in with GitHub" (top-level browser navigation).
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, errDisabled)
		return
	}
	u, err := s.authorizationURL(w, "login", "")
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "cannot start GitHub login"))
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

// ---------- callback ----------

type ghUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// denial is a user-visible rejection reason (redirected as ?github=error&reason=...).
type denial struct{ reason string }

func (d denial) Error() string { return d.reason }

func (s *Service) finish(w http.ResponseWriter, r *http.Request, outcome, reason string) {
	q := url.Values{"github": {outcome}}
	if reason != "" {
		q.Set("reason", reason)
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusFound)
}

func (s *Service) deny(w http.ResponseWriter, r *http.Request, actor *auth.Actor, resource, reason string, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	meta["reason"] = reason
	_ = auth.AuditReq(s.DB, r, "-", actor, "auth.github.denied", resource, meta)
	s.finish(w, r, "error", reason)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, errDisabled)
		return
	}
	c, err := r.Cookie(flowCookie)
	s.clearCookie(w, flowCookie, http.SameSiteLaxMode)
	var flow flowState
	if err != nil || s.open("flow", c.Value, &flow) != nil || flow.Exp < time.Now().Unix() {
		s.deny(w, r, nil, "github", "flow_expired", nil)
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(flow.State)) != 1 {
		s.deny(w, r, nil, "github", "state_mismatch", nil)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		s.deny(w, r, nil, "github", "authorization_denied", map[string]any{"github_error": e})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		s.deny(w, r, nil, "github", "missing_code", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := s.exchange(ctx, code, flow.Verifier)
	if err != nil {
		s.deny(w, r, nil, "github", "token_exchange_failed", nil)
		return
	}
	// The GitHub token is only used inside this request and is revoked right
	// after; Taboo never stores it.
	defer s.revokeToken(token)
	gu, email, err := s.identify(ctx, token)
	if err != nil {
		var d denial
		if errors.As(err, &d) {
			s.deny(w, r, nil, "github", d.reason, nil)
		} else {
			s.deny(w, r, nil, "github", "github_api_failed", nil)
		}
		return
	}
	resource := "github/" + gu.Login
	meta := map[string]any{"github_id": gu.ID, "github_login": gu.Login}
	if err := s.admitted(ctx, token, gu.Login); err != nil {
		var d denial
		if errors.As(err, &d) {
			s.deny(w, r, nil, resource, d.reason, meta)
		} else {
			s.deny(w, r, nil, resource, "github_api_failed", meta)
		}
		return
	}
	subject := strconv.FormatInt(gu.ID, 10)
	if flow.Mode == "link" {
		s.link(w, r, flow.User, subject, resource, meta)
		return
	}
	userID, err := s.resolve(r, gu, email, subject, meta)
	if err != nil {
		var d denial
		if errors.As(err, &d) {
			s.deny(w, r, nil, resource, d.reason, meta)
		} else {
			s.deny(w, r, nil, resource, "internal", meta)
		}
		return
	}
	nonce, _ := auth.RandomToken()
	sealed, err := s.seal("ticket", ticket{User: userID, Nonce: nonce, Exp: time.Now().Add(ticketTTL).Unix()})
	if err != nil {
		s.deny(w, r, nil, resource, "internal", meta)
		return
	}
	s.setCookie(w, ticketCookie, sealed, ticketTTL, http.SameSiteStrictMode)
	s.finish(w, r, "ok", "")
}

// resolve maps the GitHub account to a Taboo user: existing binding by
// (issuer, numeric id) first; never by email. Unlinked accounts are rejected
// unless AutoCreate is on.
func (s *Service) resolve(r *http.Request, gu ghUser, email, subject string, meta map[string]any) (string, error) {
	var uid, status, uemail, name string
	err := s.DB.QueryRow(`SELECT u.id,u.status,u.email,u.name FROM external_identities e JOIN users u ON u.id=e.user_id WHERE e.issuer=? AND e.subject=?`,
		s.Cfg.Issuer(), subject).Scan(&uid, &status, &uemail, &name)
	if err == nil {
		if status != "active" {
			return "", denial{"account_disabled"}
		}
		org := firstOrg(s.DB, uid)
		if err := auth.AuditReq(s.DB, r, org, &auth.Actor{ID: uid, Email: uemail, Name: name, Kind: auth.KindUser}, "auth.github.login", "user/"+uemail, meta); err != nil {
			return "", err
		}
		return uid, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if !s.Cfg.AutoCreate {
		return "", denial{"not_linked"}
	}
	var taken string
	if s.DB.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&taken) == nil {
		// Never silently attach to an existing account by email: the owner
		// must sign in and link GitHub explicitly.
		return "", denial{"email_in_use"}
	}
	display := gu.Name
	if display == "" {
		display = gu.Login
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	created, aerr := orgsvc.CreateLocalUser(tx, s.MasterKey, email, display, "!")
	if aerr != nil {
		return "", aerr
	}
	if _, err := tx.Exec(`INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)`, s.Cfg.Issuer(), subject, created.UserID); err != nil {
		return "", err
	}
	actor := &auth.Actor{ID: created.UserID, Email: email, Name: display, Kind: auth.KindUser}
	if s.Cfg.JoinOrg != "" {
		var joinID, joinKind string
		if err := tx.QueryRow(`SELECT id, kind FROM orgs WHERE slug=?`, s.Cfg.JoinOrg).Scan(&joinID, &joinKind); err != nil {
			return "", denial{"join_org_missing"}
		}
		if joinKind == orgsvc.KindPersonal {
			return "", denial{"join_org_personal"}
		}
		if _, err := tx.Exec(`INSERT INTO org_members(org_id,user_id,role) VALUES(?,?,?)`, joinID, created.UserID, s.Cfg.JoinRole); err != nil {
			return "", err
		}
		if err := auth.Audit(tx, joinID, actor, "auth.github.provision", "user/"+email, merge(meta, map[string]any{"role": s.Cfg.JoinRole}), clientIP(r)); err != nil {
			return "", err
		}
	}
	if err := auth.Audit(tx, created.OrgID, actor, "auth.github.provision", "user/"+email, meta, clientIP(r)); err != nil {
		return "", err
	}
	if err := auth.Audit(tx, created.OrgID, actor, "auth.github.login", "user/"+email, meta, clientIP(r)); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return created.UserID, nil
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func clientIP(r *http.Request) string {
	if h := r.RemoteAddr; h != "" {
		if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
			return h[:i]
		}
		return h
	}
	return ""
}

func firstOrg(db *sql.DB, user string) string {
	var org string
	if db.QueryRow(`SELECT org_id FROM org_members WHERE user_id=? ORDER BY org_id LIMIT 1`, user).Scan(&org) != nil {
		return "-"
	}
	return org
}

func (s *Service) link(w http.ResponseWriter, r *http.Request, userID, subject, resource string, meta map[string]any) {
	var email, name, status string
	if err := s.DB.QueryRow(`SELECT email,name,status FROM users WHERE id=?`, userID).Scan(&email, &name, &status); err != nil || status != "active" {
		s.deny(w, r, nil, resource, "account_disabled", meta)
		return
	}
	actor := &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser}
	var owner string
	if s.DB.QueryRow(`SELECT user_id FROM external_identities WHERE issuer=? AND subject=?`, s.Cfg.Issuer(), subject).Scan(&owner) == nil {
		reason := "already_linked_elsewhere"
		if owner == userID {
			reason = "already_linked"
		}
		s.deny(w, r, actor, resource, reason, meta)
		return
	}
	var existing string
	if s.DB.QueryRow(`SELECT issuer FROM external_identities WHERE user_id=?`, userID).Scan(&existing) == nil {
		s.deny(w, r, actor, resource, "external_identity_exists", meta)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		s.deny(w, r, actor, resource, "internal", meta)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)`, s.Cfg.Issuer(), subject, userID); err != nil {
		s.deny(w, r, actor, resource, "already_linked_elsewhere", meta)
		return
	}
	if err := auditUser(tx, userID, actor, "auth.github.link", "user/"+email, meta, clientIP(r)); err != nil || tx.Commit() != nil {
		s.deny(w, r, actor, resource, "internal", meta)
		return
	}
	s.finish(w, r, "linked", "")
}

// auditUser writes the event into every org the user belongs to (or "-").
func auditUser(tx *sql.Tx, userID string, actor *auth.Actor, action, resource string, meta map[string]any, ip string) error {
	rows, err := tx.Query(`SELECT org_id FROM org_members WHERE user_id=?`, userID)
	if err != nil {
		return err
	}
	var orgs []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			rows.Close()
			return err
		}
		orgs = append(orgs, o)
	}
	rows.Close()
	if len(orgs) == 0 {
		orgs = []string{"-"}
	}
	for _, o := range orgs {
		if err := auth.Audit(tx, o, actor, action, resource, meta, ip); err != nil {
			return err
		}
	}
	return nil
}

// ---------- ticket → Taboo tokens ----------

// Session exchanges the one-shot ticket cookie set by the callback for normal
// Taboo tokens (same shape as password login, including the TOTP challenge).
func (s *Service) Session(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, errDisabled)
		return
	}
	if o := r.Header.Get("Origin"); o == "" || o != strings.TrimRight(s.PublicOrigin, "/") {
		writeErr(w, apperr.New(403, "ORIGIN_MISMATCH", "request origin not allowed"))
		return
	}
	c, err := r.Cookie(ticketCookie)
	s.clearCookie(w, ticketCookie, http.SameSiteStrictMode)
	var t ticket
	if err != nil || s.open("ticket", c.Value, &t) != nil || t.Exp < time.Now().Unix() {
		writeErr(w, apperr.New(401, "GITHUB_TICKET_INVALID", "GitHub sign-in expired; please try again"))
		return
	}
	var u auth.Actor
	var status string
	var totp int
	if err := s.DB.QueryRow(`SELECT id,email,name,status,totp_enabled FROM users WHERE id=?`, t.User).Scan(&u.ID, &u.Email, &u.Name, &status, &totp); err != nil || status != "active" {
		writeErr(w, apperr.New(401, "GITHUB_TICKET_INVALID", "GitHub sign-in expired; please try again"))
		return
	}
	u.Kind = auth.KindUser
	if totp == 1 {
		challenge, err := tc.SignJWTClaims(map[string]any{"sub": u.ID, "typ": "totp"}, s.JWTSecret, 5*time.Minute)
		if err != nil {
			internalErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"totp_required": true, "challenge": challenge})
		return
	}
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, &u)
	if err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"user": u, "tokens": tk})
}

// ---------- account linking (authenticated local user) ----------

func (s *Service) localActor(w http.ResponseWriter, r *http.Request) *auth.Actor {
	a := auth.From(r)
	if a == nil || a.Kind != auth.KindUser {
		writeErr(w, apperr.Unauthorized)
		return nil
	}
	if a.OrganizationID != "" {
		writeErr(w, apperr.New(403, "FEDERATED_SESSION", "organization SSO sessions cannot link GitHub"))
		return nil
	}
	return a
}

// LinkStatus: GET /auth/github/link
func (s *Service) LinkStatus(w http.ResponseWriter, r *http.Request) {
	a := s.localActor(w, r)
	if a == nil {
		return
	}
	out := map[string]any{"enabled": s.Enabled(), "linked": false}
	var subject string
	if s.DB.QueryRow(`SELECT subject FROM external_identities WHERE issuer=? AND user_id=?`, s.Cfg.Issuer(), a.ID).Scan(&subject) == nil {
		out["linked"] = true
		out["github_id"] = subject
	}
	var hash string
	_ = s.DB.QueryRow(`SELECT password_hash FROM users WHERE id=?`, a.ID).Scan(&hash)
	out["has_password"] = hash != "" && hash != "!"
	writeJSON(w, 200, out)
}

// StartLink: POST /auth/github/link → {authorization_url}
func (s *Service) StartLink(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeErr(w, errDisabled)
		return
	}
	a := s.localActor(w, r)
	if a == nil {
		return
	}
	var x string
	if s.DB.QueryRow(`SELECT issuer FROM external_identities WHERE user_id=?`, a.ID).Scan(&x) == nil {
		writeErr(w, apperr.New(409, "ALREADY_LINKED", "this account already has an external identity"))
		return
	}
	u, err := s.authorizationURL(w, "link", a.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "cannot start GitHub link"))
		return
	}
	writeJSON(w, 200, map[string]any{"authorization_url": u})
}

// Unlink: DELETE /auth/github/link. Removes the binding and revokes every
// refresh token of the user (GitHub-started sessions are not distinguishable),
// so existing sessions end within the 15-minute access-token lifetime.
func (s *Service) Unlink(w http.ResponseWriter, r *http.Request) {
	a := s.localActor(w, r)
	if a == nil {
		return
	}
	var hash, email string
	if err := s.DB.QueryRow(`SELECT password_hash,email FROM users WHERE id=?`, a.ID).Scan(&hash, &email); err != nil {
		writeErr(w, apperr.Unauthorized)
		return
	}
	if hash == "" || hash == "!" {
		writeErr(w, apperr.New(409, "NO_OTHER_LOGIN", "this account has no password; unlinking GitHub would lock it out"))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM external_identities WHERE issuer=? AND user_id=?`, s.Cfg.Issuer(), a.ID)
	if err != nil {
		internalErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, apperr.New(404, "NOT_LINKED", "GitHub is not linked"))
		return
	}
	if _, err := tx.Exec(`DELETE FROM refresh_tokens WHERE user_id=?`, a.ID); err != nil {
		internalErr(w, err)
		return
	}
	if err := auditUser(tx, a.ID, a, "auth.github.unlink", "user/"+email, nil, clientIP(r)); err != nil {
		internalErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"linked": false, "refresh_tokens_revoked": true})
}

// ---------- GitHub API ----------

func (s *Service) exchange(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"client_id": {s.Cfg.ClientID}, "client_secret": {s.Cfg.ClientSecret},
		"code": {code}, "redirect_uri": {s.CallbackURL()}, "code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.Cfg.OAuthURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var b struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&b); err != nil {
		return "", err
	}
	if resp.StatusCode != 200 || b.AccessToken == "" {
		return "", fmt.Errorf("token exchange failed: %d %s", resp.StatusCode, b.Error)
	}
	return b.AccessToken, nil
}

func (s *Service) api(ctx context.Context, token, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.Cfg.APIURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func (s *Service) identify(ctx context.Context, token string) (ghUser, string, error) {
	var u ghUser
	code, err := s.api(ctx, token, "/user", &u)
	if err != nil || code != 200 || u.ID == 0 || u.Login == "" {
		return u, "", fmt.Errorf("GET /user: %d %v", code, err)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	code, err = s.api(ctx, token, "/user/emails", &emails)
	if err != nil || code != 200 {
		return u, "", fmt.Errorf("GET /user/emails: %d %v", code, err)
	}
	for _, e := range emails {
		if e.Primary && e.Verified && e.Email != "" {
			return u, strings.ToLower(strings.TrimSpace(e.Email)), nil
		}
	}
	return u, "", denial{"no_verified_primary_email"}
}

// admitted enforces the operator's access policy on every sign-in and link.
func (s *Service) admitted(ctx context.Context, token, login string) error {
	if s.Cfg.AllowAny {
		return nil
	}
	l := strings.ToLower(login)
	for _, u := range s.Cfg.AllowedUsers {
		if u == l {
			return nil
		}
	}
	type membership struct {
		State string `json:"state"`
	}
	for _, org := range s.Cfg.AllowedOrgs {
		var m membership
		code, err := s.api(ctx, token, "/user/memberships/orgs/"+url.PathEscape(org), &m)
		if err != nil {
			return err
		}
		if code == 200 && m.State == "active" {
			return nil
		}
	}
	for _, t := range s.Cfg.AllowedTeams {
		p := strings.SplitN(t, "/", 2)
		var m membership
		code, err := s.api(ctx, token, "/orgs/"+url.PathEscape(p[0])+"/teams/"+url.PathEscape(p[1])+"/memberships/"+url.PathEscape(login), &m)
		if err != nil {
			return err
		}
		if code == 200 && m.State == "active" {
			return nil
		}
	}
	return denial{"not_allowed"}
}

// revokeToken deletes the short-lived GitHub user token (best effort). The
// OAuth grant itself stays, so users are not re-prompted on the next login.
func (s *Service) revokeToken(token string) {
	body, _ := json.Marshal(map[string]string{"access_token": token})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "DELETE", s.Cfg.APIURL+"/applications/"+url.PathEscape(s.Cfg.ClientID)+"/token", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.SetBasicAuth(s.Cfg.ClientID, s.Cfg.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if resp, err := s.client().Do(req); err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
	}
}
