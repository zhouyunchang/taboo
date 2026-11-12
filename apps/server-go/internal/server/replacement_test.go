package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testoidc"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
)

func replacementProvider(t *testing.T, f *federationFixture) *testoidc.Provider {
	op := testoidc.New(t)
	enc, _ := tc.Seal(f.master, "fixture-secret", tc.Context{Purpose: "oidc-client", Org: "a", Object: "provider-new", Version: 1})
	f.exec(`INSERT INTO oidc_providers(id,org_id,name,issuer,client_id,client_secret_enc,autocreate,scopes,default_role) VALUES('provider-new','a','New provider',?,'taboo-test',?,0,'openid','admin')`, op.Server.URL, enc)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.h = New(&Deps{DB: f.db, MasterKey: f.master, JWTSecret: "fixture-jwt", PublicOrigin: fixtureOrigin, SessionProfile: "BOUNDED", IssuerAllowlist: []string{f.op.Server.URL, op.Server.URL}, DEKs: tc.NewDEKCache(), LoginRate: 10000, WorkerCtx: ctx})
	return op
}
func replacementBegin(t *testing.T, f *federationFixture, c *http.Cookie, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/v1/auth/oidc/a/replace?pid=provider-new", strings.NewReader(body))
	r.Header.Set("Origin", fixtureOrigin)
	if c != nil {
		r.AddCookie(c)
		r.Header.Set("X-CSRF-Token", auth.BrowserCSRF(c.Value))
	} else {
		token, _ := tc.SignJWT("local", "collision@example.test", "fixture-jwt", time.Minute)
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func replacementCallback(t *testing.T, f *federationFixture, op *testoidc.Provider, start *httptest.ResponseRecorder, c *http.Cookie, claims map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	checkStatus(t, start, 200)
	var body struct {
		URL string `json:"authorization_url"`
	}
	json.Unmarshal(start.Body.Bytes(), &body)
	u, err := url.Parse(body.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("prompt") != "login" {
		t.Fatal("replacement must require new proof")
	}
	code := op.Code(q, claims)
	r := httptest.NewRequest("GET", "/api/v1/auth/oidc/a/callback?"+url.Values{"state": {q.Get("state")}, "code": {code}}.Encode(), nil)
	r.AddCookie(cookieOf(t, start, "taboo.oidc.flow"))
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func TestFederationIdentityReplacementPreservesData(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	op := replacementProvider(t, f)
	var uid string
	f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='subject-a'`).Scan(&uid)
	dek, _ := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
	f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"K","value":"unchanged"}`, ""), 200)
	f.exec(`INSERT INTO member_action_grants(org_id,user_id,project_id,action) VALUES('a',?,'project-a','read')`, uid)
	var before string
	f.db.QueryRow(`SELECT ciphertext FROM secret_versions`).Scan(&before)
	start := replacementBegin(t, f, c, "")
	w := replacementCallback(t, f, op, start, c, map[string]any{"sub": "new-pairwise-sub"})
	checkStatus(t, w, 303)
	next := cookieOf(t, w, auth.BrowserCookie)
	var got, role, after string
	if err := f.db.QueryRow(`SELECT user_id FROM external_identities WHERE issuer=? AND subject='new-pairwise-sub'`, op.Server.URL).Scan(&got); err != nil || got != uid {
		t.Fatal("local identity not retained", err)
	}
	f.db.QueryRow(`SELECT role FROM org_members WHERE org_id='a' AND user_id=?`, uid).Scan(&role)
	if role != "viewer" {
		t.Fatal("mapped role raised permissions", role)
	}
	f.db.QueryRow(`SELECT ciphertext FROM secret_versions`).Scan(&after)
	if before == "" || before != after {
		t.Fatal("provider replacement changed secret ciphertext")
	}
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
	checkStatus(t, f.request("GET", "/api/v1/projects/project-a/secrets", next, false), 200)
	checkStatus(t, replacementCallback(t, f, op, start, c, nil), 400)
	f.exec(`UPDATE oidc_providers SET enabled=0 WHERE id='provider-a'`)
	checkStatus(t, f.request("GET", "/api/v1/me", next, false), 200)
}
func TestFederationIdentityReplacementProofFailures(t *testing.T) {
	for _, scenario := range []string{"missing_old_cookie", "old_session_revoked", "old_provider_changed", "audit_failure", "already_bound"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture(t, "")
			c := f.login("a", nil)
			op := replacementProvider(t, f)
			start := replacementBegin(t, f, c, "")
			checkStatus(t, start, 200)
			var uid string
			f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='subject-a'`).Scan(&uid)
			expected := 400
			switch scenario {
			case "missing_old_cookie":
				c = nil
			case "old_session_revoked":
				f.exec(`DELETE FROM browser_sessions`)
			case "old_provider_changed":
				f.exec(`UPDATE oidc_providers SET default_role='admin' WHERE id='provider-a'`)
			case "audit_failure":
				testdb.FailAudit(t, f.db)
				expected = 503
			case "already_bound":
				f.exec(`INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,'new-sub','local')`, op.Server.URL)
				expected = 409
			}
			checkStatus(t, replacementCallback(t, f, op, start, c, map[string]any{"sub": "new-sub"}), expected)
			var got string
			f.db.QueryRow(`SELECT user_id FROM external_identities WHERE issuer=? AND subject='subject-a'`, f.op.Server.URL).Scan(&got)
			if got != uid {
				t.Fatal("failed replacement lost original identity")
			}
		})
	}
}
func TestFederationLocalIdentityReplacementRequiresCredentials(t *testing.T) {
	for _, mfa := range []bool{false, true} {
		t.Run(map[bool]string{false: "password", true: "password_and_totp"}[mfa], func(t *testing.T) {
			f := fixture(t, "")
			op := replacementProvider(t, f)
			hash, _ := tc.HashPassword("fixture-password")
			f.exec(`UPDATE users SET password_hash=? WHERE id='local'`, hash)
			checkStatus(t, replacementBegin(t, f, nil, `{"password":"wrong"}`), 401)
			body := map[string]string{"password": "fixture-password"}
			if mfa {
				secret, _ := totp.GenerateSecret()
				enc, _ := tc.Seal(f.master, secret, tc.Context{Purpose: "user-totp", Object: "local", Version: 1})
				f.exec(`UPDATE users SET totp_enabled=1,totp_secret_enc=? WHERE id='local'`, enc)
				checkStatus(t, replacementBegin(t, f, nil, `{"password":"fixture-password"}`), 401)
				body["code"], _ = totp.Code(secret, totp.NowStep())
			}
			raw, _ := json.Marshal(body)
			start := replacementBegin(t, f, nil, string(raw))
			checkStatus(t, start, 200)
			if mfa {
				checkStatus(t, replacementBegin(t, f, nil, string(raw)), 401)
			}
			w := replacementCallback(t, f, op, start, nil, map[string]any{"sub": "local-migrated"})
			checkStatus(t, w, 303)
			var uid string
			f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='local-migrated'`).Scan(&uid)
			if uid != "local" {
				t.Fatal("local owner not retained")
			}
			checkStatus(t, f.localRequest("GET", "/api/v1/me", "", ""), 401)
			checkStatus(t, f.request("GET", "/api/v1/me", cookieOf(t, w, auth.BrowserCookie), false), 200)
		})
	}
}
