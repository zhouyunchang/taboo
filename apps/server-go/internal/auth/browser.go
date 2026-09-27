package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

const BrowserCookie = "taboo.session"

func RandomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func BrowserCSRF(raw string) string {
	s := sha256.Sum256([]byte("taboo.browser.csrf\x00" + raw))
	return hex.EncodeToString(s[:])
}

// ProviderRevision is deliberately based on all authentication and mapping
// settings. Any change invalidates pending logins and existing BFF sessions.
func ProviderRevision(db interface{ QueryRow(string, ...any) *sql.Row }, id string) (string, error) {
	var values [13]string
	err := db.QueryRow(`SELECT id,org_id,issuer,client_id,client_secret_enc,scopes,role_claim,
 username_claim,role_map,default_role,CAST(autocreate AS TEXT),CAST(sync_role AS TEXT),CAST(enabled AS TEXT)
 FROM oidc_providers WHERE id=? AND enabled=1`, id).Scan(
		&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9], &values[10], &values[11], &values[12])
	if err != nil {
		return "", err
	}
	return ProviderRevisionValues(values), nil
}

func ProviderRevisionValues(values [13]string) string {
	b, _ := json.Marshal(values)
	return tc.SHA256(string(b))
}

func SetBrowserCookie(w http.ResponseWriter, raw, origin string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: BrowserCookie, Value: raw, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}

func browserActor(db *sql.DB, r *http.Request) (*Actor, error) {
	c, err := r.Cookie(BrowserCookie)
	if err != nil || len(c.Value) != 43 {
		return nil, apperr.Unauthorized
	}
	var user, org, provider, revision, origin string
	err = db.QueryRow(`SELECT s.user_id,s.org_id,s.provider_id,s.config_hash,s.origin FROM browser_sessions s
 JOIN org_members m ON m.org_id=s.org_id AND m.user_id=s.user_id
 WHERE s.token_hash=? AND s.expires_at>?`, tc.SHA256(c.Value), time.Now().Unix()).Scan(&user, &org, &provider, &revision, &origin)
	if err != nil {
		return nil, apperr.Unauthorized
	}
	current, err := ProviderRevision(db, provider)
	if err != nil || current != revision {
		return nil, apperr.Unauthorized
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if r.Header.Get("Origin") != origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(BrowserCSRF(c.Value))) != 1 {
			return nil, apperr.New(403, "CSRF_REQUIRED", "reload the page before retrying")
		}
	}
	a, err := loadUser(db, user)
	if err != nil {
		return nil, err
	}
	a.OrganizationID, a.AuthenticationSource, a.BrowserOrigin = org, provider, origin
	return a, nil
}

func BrowserSession(db *sql.DB, policies ...func(*Actor) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		a, err := browserActor(db, r)
		if err == nil {
			for _, allowed := range policies {
				if !allowed(a) {
					err = apperr.Unauthorized
					break
				}
			}
		}
		if err != nil {
			result := map[string]any{"authenticated": false}
			if _, e := r.Cookie(BrowserCookie); e == nil {
				result["authentication"] = "oidc"
				http.SetCookie(w, &http.Cookie{Name: BrowserCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
			}
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		c, _ := r.Cookie(BrowserCookie)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": true, "csrf_token": BrowserCSRF(c.Value), "organization_id": a.OrganizationID, "authentication": "oidc", "revocation_profile": "BOUNDED"})
	}
}

func BrowserLogout(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := From(r)
		if a == nil || a.AuthenticationSource == "" {
			writeErr(w, apperr.Unauthorized)
			return
		}
		c, _ := r.Cookie(BrowserCookie)
		if _, err := db.Exec(`DELETE FROM browser_sessions WHERE token_hash=?`, tc.SHA256(c.Value)); err != nil {
			writeErr(w, apperr.New(503, "SESSION_UNAVAILABLE", "try again"))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: BrowserCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
