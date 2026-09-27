package oidc

import (
	"crypto/subtle"
	"database/sql"
	"net/http"
	"net/url"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

// Local logout must remain available during an IdP outage. CSRF is checked
// independently of discovery, refresh, membership and online introspection.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	origin, err := s.origin()
	if err != nil || r.Header.Get("Origin") != origin {
		writeErr(w, apperr.Forbidden)
		return
	}
	c, err := r.Cookie(auth.BrowserCookie)
	if err != nil || len(c.Value) != 43 {
		w.WriteHeader(204)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(auth.BrowserCSRF(c.Value))) != 1 {
		writeErr(w, apperr.New(403, "CSRF_REQUIRED", "reload before retrying"))
		return
	}
	hash := tc.SHA256(c.Value)
	var user, org, provider, enc string
	var revision int
	err = s.DB.QueryRow(`SELECT s.user_id,s.org_id,s.provider_id,COALESCE(c.ciphertext,''),COALESCE(c.revision,1) FROM browser_sessions s LEFT JOIN browser_credentials c ON c.token_hash=s.token_hash WHERE s.token_hash=?`, hash).Scan(&user, &org, &provider, &enc, &revision)
	if err != nil && err != sql.ErrNoRows {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	logoutURL := ""
	if enc != "" {
		if b, e := s.openSession(org, hash, revision, enc); e == nil && b.LogoutEndpoint != "" {
			q := url.Values{"id_token_hint": {b.IDToken}, "post_logout_redirect_uri": {origin + "/"}}
			logoutURL = b.LogoutEndpoint + "?" + q.Encode()
		}
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM browser_sessions WHERE token_hash=?`, hash); err != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	if org != "" {
		if err = auth.AuditReq(tx, r, org, &auth.Actor{ID: user, Kind: auth.KindUser, AuthenticationSource: provider}, "auth.logout", "session", nil); err != nil {
			writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "logout could not be recorded"))
			return
		}
	}
	if tx.Commit() != nil {
		writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.BrowserCookie, Value: "", Path: "/", HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	w.Header().Set("Cache-Control", "no-store")
	if logoutURL != "" {
		writeJSON(w, 200, map[string]any{"logout_url": logoutURL})
		return
	}
	w.WriteHeader(204)
}
