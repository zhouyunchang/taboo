package oidc

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	federation "github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oidc"
)

const flowCookie = "taboo.oidc.flow"

func (s *Service) origin() (string, error) {
	if s.SessionProfile != "BOUNDED" {
		return "", errors.New("explicit BOUNDED session profile required")
	}
	u, err := federation.URL(s.PublicOrigin)
	if err != nil || (u.Path != "" && u.Path != "/") {
		return "", errors.New("public origin required")
	}
	return u.Scheme + "://" + u.Host, nil
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	origin, err := s.origin()
	if err != nil {
		writeErr(w, apperr.New(503, "OIDC_CONFIGURATION_REQUIRED", "configure TABOO_PUBLIC_ORIGIN and TABOO_OIDC_SESSION_PROFILE=BOUNDED before using external sign-in"))
		return
	}
	slug := chi.URLParam(r, "slug")
	p, err := s.providerByOrgSlug(slug, r.URL.Query().Get("pid"))
	if err != nil || !s.allowedIssuer(p.Issuer) {
		writeErr(w, apperr.NotFound)
		return
	}
	doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", "identity provider unavailable or incompatible"))
		return
	}
	state, err := auth.RandomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	browser, err := auth.RandomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	verifier, challenge, err := newPKCE()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	nonce, err := auth.RandomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	enc, err := tc.Seal(s.MasterKey, verifier, tc.Context{Purpose: "oidc-pkce", Org: p.OrgID, Object: tc.SHA256(state), Version: 1})
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	revision := p.Revision
	redirect := origin + "/api/v1/auth/oidc/" + url.PathEscape(slug) + "/callback"
	expires := time.Now().Add(5 * time.Minute)
	if _, err = s.DB.ExecContext(r.Context(), `DELETE FROM oidc_login_requests WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	_, err = s.DB.ExecContext(r.Context(), `INSERT INTO oidc_login_requests(state_hash,browser_hash,provider_id,config_hash,verifier_enc,nonce,redirect_uri,expires_at) VALUES(?,?,?,?,?,?,?,?)`, tc.SHA256(state), tc.SHA256(browser), p.ID, revision, enc, nonce, redirect, expires.Unix())
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: browser, Path: "/api/v1/auth/oidc/" + slug, HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 300})
	q := url.Values{"client_id": {p.ClientID}, "redirect_uri": {redirect}, "response_type": {"code"}, "scope": {oauthScopes(p.Scopes)}, "state": {state}, "nonce": {nonce}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	http.Redirect(w, r, doc.AuthorizationEndpoint+"?"+q.Encode(), http.StatusFound)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	origin, err := s.origin()
	if err != nil {
		writeErr(w, apperr.New(503, "OIDC_CONFIGURATION_REQUIRED", "sign-in unavailable"))
		return
	}
	q := r.URL.Query()
	bad := func() { writeErr(w, apperr.New(400, "BAD_STATE", "sign-in expired or invalid; start again")) }
	c, err := r.Cookie(flowCookie)
	if err != nil || len(c.Value) != 43 || len(q["state"]) != 1 || len(q.Get("state")) != 43 || len(q["code"]) > 1 || len(q["error"]) > 1 {
		bad()
		return
	}
	var pid, revision, enc, nonce, redirect string
	// Consume atomically before any outbound request. A failed exchange requires
	// a new authorization flow, never a replay of the old code.
	err = s.DB.QueryRowContext(r.Context(), `DELETE FROM oidc_login_requests WHERE state_hash=? AND browser_hash=? AND expires_at>? RETURNING provider_id,config_hash,verifier_enc,nonce,redirect_uri`, tc.SHA256(q.Get("state")), tc.SHA256(c.Value), time.Now().Unix()).Scan(&pid, &revision, &enc, &nonce, &redirect)
	if err != nil {
		bad()
		return
	}
	slug := chi.URLParam(r, "slug")
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/api/v1/auth/oidc/" + slug, HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	p, err := s.providerByOrgSlug(slug, pid)
	if err != nil || !s.allowedIssuer(p.Issuer) || p.Revision != revision || redirect != origin+"/api/v1/auth/oidc/"+url.PathEscape(slug)+"/callback" {
		bad()
		return
	}
	current, err := auth.ProviderRevision(s.DB, pid)
	if err != nil || current != revision {
		bad()
		return
	}
	if q.Get("error") != "" {
		writeErr(w, apperr.New(400, "OIDC_CANCELLED", "external sign-in was not completed"))
		return
	}
	if q.Get("code") == "" {
		bad()
		return
	}
	if iss := q.Get("iss"); iss != "" && iss != p.Issuer {
		bad()
		return
	}
	verifier, err := tc.Open(s.MasterKey, enc, tc.Context{Purpose: "oidc-pkce", Org: p.OrgID, Object: tc.SHA256(q.Get("state")), Version: 1})
	if err != nil {
		bad()
		return
	}
	doc, _, err := discoCache.getContext(r.Context(), p.ID, p.Issuer)
	if err != nil {
		writeErr(w, apperr.New(502, "IDP_UNREACHABLE", "identity provider unavailable"))
		return
	}
	identity, err := doc.Exchange(r.Context(), p.ClientID, p.ClientSecret, q.Get("code"), verifier, redirect, nonce)
	if err != nil {
		writeErr(w, apperr.New(401, "OIDC_AUTHENTICATION_FAILED", "external sign-in could not be verified"))
		return
	}
	raw, err := auth.RandomToken()
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	expires := time.Now().Add(15 * time.Minute)
	if identity.ExpiresAt.Before(expires) {
		expires = identity.ExpiresAt
	}
	if !expires.After(time.Now()) {
		bad()
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	current, err = auth.ProviderRevision(tx, p.ID)
	if err != nil || current != revision {
		bad()
		return
	}
	uid, err := bindIdentity(tx, p, identity.Subject, identity.Claims)
	if err != nil {
		writeErr(w, apperr.New(403, "OIDC_ADMISSION_REQUIRED", "ask an organization administrator to authorize this identity"))
		return
	}
	actor := &auth.Actor{ID: uid, Kind: auth.KindUser, AuthenticationSource: p.ID, Name: claimString(identity.Claims, "name", "preferred_username")}
	if err = auth.AuditReq(tx, r, p.OrgID, actor, "auth.oidc.login", "organization/"+p.OrgID, map[string]any{"provider_id": p.ID}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "sign-in could not be recorded"))
		return
	}
	if _, err = tx.Exec(`DELETE FROM browser_sessions WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	if old, err := r.Cookie(auth.BrowserCookie); err == nil {
		if _, err = tx.Exec(`DELETE FROM browser_sessions WHERE token_hash=?`, tc.SHA256(old.Value)); err != nil {
			writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
			return
		}
	}
	_, err = tx.Exec(`INSERT INTO browser_sessions(token_hash,user_id,org_id,provider_id,config_hash,origin,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, tc.SHA256(raw), uid, p.OrgID, p.ID, revision, origin, expires.Unix(), time.Now().Unix())
	if err != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "LOGIN_UNAVAILABLE", "try again"))
		return
	}
	auth.SetBrowserCookie(w, raw, origin, expires)
	// No token, identity payload, or provider error is rendered into script.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func bindIdentity(tx *sql.Tx, p *provider, subject string, claims map[string]any) (string, error) {
	var uid, status string
	err := tx.QueryRow(`SELECT e.user_id,u.status FROM external_identities e JOIN users u ON u.id=e.user_id WHERE e.issuer=? AND e.subject=?`, p.Issuer, subject).Scan(&uid, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		// Migrate only an exact previous provider/sub association; never email.
		err = tx.QueryRow(`SELECT id,status FROM users WHERE auth_source=? AND external_sub=?`, p.ID, subject).Scan(&uid, &status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if errors.Is(err, sql.ErrNoRows) {
			if !p.Autocreate {
				return "", errors.New("admission required")
			}
			uid, status = tc.NewID(), "active"
			name := claimString(claims, "name", "preferred_username")
			if name == "" {
				name = "External user"
			}
			// A reserved address only satisfies the legacy schema's unique email
			// column. It is never an identity-linking or ownership proof.
			email := "oidc-" + tc.SHA256(p.Issuer+"\x00"+subject) + "@external.invalid"
			_, err = tx.Exec(`INSERT INTO users(id,email,name,password_hash,auth_source,external_sub) VALUES(?,?,?,'!',?,?)`, uid, email, name, p.ID, subject)
			if err != nil {
				return "", err
			}
		}
		if _, err = tx.Exec(`INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)`, p.Issuer, subject, uid); err != nil {
			return "", err
		}
	}
	if status != "active" {
		return "", errors.New("inactive")
	}
	var role string
	mappedRole := capJITRole(MapRoleExact(claims, p.RoleClaim, p.RoleMap, p.DefaultRole))
	ownedRole := ""
	err = tx.QueryRow(`SELECT role FROM org_members WHERE org_id=? AND user_id=?`, p.OrgID, uid).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		var admitted int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM federation_admissions WHERE org_id=? AND user_id=?`, p.OrgID, uid).Scan(&admitted); err != nil {
			return "", err
		}
		if admitted > 0 || !p.Autocreate {
			return "", errors.New("admission required")
		}
		role = mappedRole
		ownedRole = role
		if _, err = tx.Exec(`INSERT INTO org_members(org_id,user_id,role) VALUES(?,?,?)`, p.OrgID, uid, role); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	// Only grants still owned by this connection participate in claim sync.
	// Explicit local role changes clear last_role, preserving local governance.
	var previous, owner string
	lookup := tx.QueryRow(`SELECT last_role,provider_id FROM federation_admissions WHERE org_id=? AND user_id=?`, p.OrgID, uid).Scan(&previous, &owner)
	if lookup != nil && !errors.Is(lookup, sql.ErrNoRows) {
		return "", lookup
	}
	if lookup == nil && owner == p.ID && previous != "" && previous == role && p.SyncRole {
		if _, err = tx.Exec(`UPDATE org_members SET role=? WHERE org_id=? AND user_id=?`, mappedRole, p.OrgID, uid); err != nil {
			return "", err
		}
		if _, err = tx.Exec(`UPDATE federation_admissions SET last_role=? WHERE org_id=? AND user_id=?`, mappedRole, p.OrgID, uid); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(`INSERT INTO federation_admissions(org_id,provider_id,user_id,last_role) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, p.OrgID, p.ID, uid, ownedRole); err != nil {
		return "", err
	}
	return uid, nil
}
