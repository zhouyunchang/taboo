package server

import (
	"context"
	"database/sql"
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
)

const fixtureOrigin = "http://127.0.0.1:18888"

type federationFixture struct {
	t      *testing.T
	db     *sql.DB
	h      http.Handler
	op     *testoidc.Provider
	master []byte
}

func fixture(t *testing.T, issuer string) *federationFixture {
	t.Helper()
	f := &federationFixture{t: t, master: make([]byte, 32)}
	if issuer == "" {
		f.op = testoidc.New(t)
		issuer = f.op.Server.URL
	}
	db := testdb.Open(t)
	f.db = db
	t.Cleanup(func() { db.Close() })
	for _, org := range []string{"a", "b"} {
		f.exec(`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES(?,?,?,'unused')`, org, org, org)
		f.exec(`INSERT INTO projects(id,org_id,name,slug) VALUES(?,?,?,?)`, "project-"+org, org, "default", "default")
		f.exec(`INSERT INTO environments(id,project_id,name,slug) VALUES(?,?,?,?)`, "env-"+org, "project-"+org, "dev", "dev")
		enc, e := tc.Encrypt(f.master, "fixture-secret")
		if e != nil {
			t.Fatal(e)
		}
		f.exec(`INSERT INTO oidc_providers(id,org_id,name,issuer,client_id,client_secret_enc,autocreate,scopes) VALUES(?,?,?,?,?,?,1,'openid')`, "provider-"+org, org, "OpenID Connect", issuer, "taboo-test", enc)
	}
	f.exec(`INSERT INTO users(id,email,name,password_hash) VALUES('local','collision@example.test','Local','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','local','owner')`)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.h = New(&Deps{DB: db, MasterKey: f.master, JWTSecret: "fixture-jwt", PublicOrigin: fixtureOrigin, SessionProfile: "BOUNDED", IssuerAllowlist: []string{issuer}, DEKs: tc.NewDEKCache(), LoginRate: 10000, WorkerCtx: ctx})
	return f
}
func (f *federationFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.db.Exec(q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *federationFixture) request(method, path string, cookie *http.Cookie, csrf bool) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		r.AddCookie(cookie)
		if csrf {
			r.Header.Set("Origin", fixtureOrigin)
			r.Header.Set("X-CSRF-Token", auth.BrowserCSRF(cookie.Value))
		}
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
func checkStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
	}
}
func cookieOf(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %s missing", name)
	return nil
}
func (f *federationFixture) begin(org string) (url.Values, *http.Cookie) {
	f.t.Helper()
	w := f.request("GET", "/api/v1/auth/oidc/"+org+"/login", nil, false)
	checkStatus(f.t, w, 302)
	u, e := url.Parse(w.Header().Get("Location"))
	if e != nil {
		f.t.Fatal(e)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("scope") != "openid" || q.Get("nonce") == "" {
		f.t.Fatal("incorrect authorization request")
	}
	return q, cookieOf(f.t, w, "taboo.oidc.flow")
}
func (f *federationFixture) callback(org string, q url.Values, c *http.Cookie, claims map[string]any) *httptest.ResponseRecorder {
	code := f.op.Code(q, claims)
	return f.request("GET", "/api/v1/auth/oidc/"+org+"/callback?"+url.Values{"state": {q.Get("state")}, "code": {code}}.Encode(), c, false)
}
func (f *federationFixture) login(org string, claims map[string]any) *http.Cookie {
	f.t.Helper()
	q, c := f.begin(org)
	w := f.callback(org, q, c, claims)
	checkStatus(f.t, w, 303)
	return cookieOf(f.t, w, auth.BrowserCookie)
}
func TestFederationBoundedSession(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge > 900 {
		t.Fatal("unsafe cookie")
	}
	w := f.request("GET", "/api/v1/auth/session", c, false)
	checkStatus(t, w, 200)
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	if session["authenticated"] != true || session["revocation_profile"] != "BOUNDED" {
		t.Fatal(session)
	}
	checkStatus(t, f.request("GET", "/api/v1/orgs/a/projects", c, false), 200)
	checkStatus(t, f.request("GET", "/api/v1/orgs/b/projects", c, false), 403)
	checkStatus(t, f.request("GET", "/api/v1/projects/project-b/secrets?env=dev", c, false), 403)
	checkStatus(t, f.request("GET", "/api/v1/projects/project-a/export?env=dev", c, false), 403)
	checkStatus(t, f.request("POST", "/api/v1/auth/logout", c, false), 403)
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(c)
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("X-CSRF-Token", auth.BrowserCSRF(c.Value))
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	checkStatus(t, w, 403)
	checkStatus(t, f.request("POST", "/api/v1/auth/logout", c, true), 204)
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
}
func TestFederationIdentityBoundaries(t *testing.T) {
	f := fixture(t, "")
	f.login("a", map[string]any{"email": "collision@example.test", "email_verified": true})
	var uid string
	if e := f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='subject-a'`).Scan(&uid); e != nil || uid == "local" {
		t.Fatalf("email linked local identity: %s %v", uid, e)
	}
	f.login("b", nil)
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM external_identities`).Scan(&n)
	if n != 1 {
		t.Fatalf("same issuer/sub split: %d", n)
	}
	op2 := testoidc.New(t)
	f.op = op2
	f.h = New(&Deps{DB: f.db, MasterKey: f.master, JWTSecret: "fixture-jwt", PublicOrigin: fixtureOrigin, SessionProfile: "BOUNDED", IssuerAllowlist: []string{op2.Server.URL}, DEKs: tc.NewDEKCache(), LoginRate: 10000})
	f.exec(`UPDATE oidc_providers SET issuer=? WHERE id='provider-b'`, op2.Server.URL)
	// Same subject from another issuer creates a distinct identity.
	wq, wc := f.begin("b")
	w := f.callback("b", wq, wc, nil)
	checkStatus(t, w, 303)
	f.db.QueryRow(`SELECT COUNT(*) FROM external_identities`).Scan(&n)
	if n != 2 {
		t.Fatalf("different issuers merged: %d", n)
	}
	// A previously bound legacy account cannot be reassigned to a new issuer.
	f.exec(`UPDATE oidc_providers SET issuer=? WHERE id='provider-a'`, op2.Server.URL)
	wq, wc = f.begin("a")
	checkStatus(t, f.callback("a", wq, wc, map[string]any{"sub": "new-subject"}), 303)
}
func TestFederationProtocolRejections(t *testing.T) {
	for _, tt := range []struct {
		name   string
		claims map[string]any
	}{
		{"nonce", map[string]any{"nonce": "wrong"}}, {"issuer", map[string]any{"iss": "https://wrong.test"}},
		{"audience", map[string]any{"aud": "other"}}, {"azp", map[string]any{"azp": "other"}},
		{"multiple_audiences", map[string]any{"aud": []string{"taboo-test", "other"}}},
		{"expired", map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}}, {"future_iat", map[string]any{"iat": time.Now().Add(time.Hour).Unix()}},
		{"no_subject", map[string]any{"sub": nil}}, {"no_iat", map[string]any{"iat": nil}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := fixture(t, "")
			q, c := f.begin("a")
			checkStatus(t, f.callback("a", q, c, tt.claims), 401)
		})
	}
	t.Run("cookie_state_pkce_replay", func(t *testing.T) {
		f := fixture(t, "")
		q, c := f.begin("a")
		checkStatus(t, f.callback("a", q, nil, nil), 400)
		bad := *c
		bad.Value = strings.Repeat("x", 43)
		checkStatus(t, f.callback("a", q, &bad, nil), 400)
		checkStatus(t, f.callback("a", q, c, nil), 303)
		checkStatus(t, f.callback("a", q, c, nil), 400)
		q, c = f.begin("a")
		q.Set("code_challenge", "wrong")
		checkStatus(t, f.callback("a", q, c, nil), 401)
		q, c = f.begin("a")
		checkStatus(t, f.callback("b", q, c, nil), 400)
	})
}
func TestFederationRevocation(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE users SET status='disabled' WHERE auth_source='provider-a'`,
		`UPDATE oidc_providers SET enabled=0 WHERE id='provider-a'`,
		`UPDATE oidc_providers SET default_role='admin' WHERE id='provider-a'`,
		`DELETE FROM org_members WHERE org_id='a' AND user_id!='local'`,
		`UPDATE browser_sessions SET expires_at=0`,
	} {
		t.Run(mutation, func(t *testing.T) {
			f := fixture(t, "")
			c := f.login("a", nil)
			f.exec(mutation)
			checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
		})
	}
	t.Run("removed_member_not_readmitted", func(t *testing.T) {
		f := fixture(t, "")
		f.login("a", nil)
		f.exec(`DELETE FROM org_members WHERE org_id='a' AND user_id!='local'`)
		q, c := f.begin("a")
		checkStatus(t, f.callback("a", q, c, nil), 403)
	})
	t.Run("no_JIT", func(t *testing.T) {
		f := fixture(t, "")
		f.exec(`UPDATE oidc_providers SET autocreate=0`)
		q, c := f.begin("a")
		checkStatus(t, f.callback("a", q, c, nil), 403)
	})
	t.Run("pending_config_change", func(t *testing.T) {
		f := fixture(t, "")
		q, c := f.begin("a")
		f.exec(`UPDATE oidc_providers SET default_role='admin'`)
		checkStatus(t, f.callback("a", q, c, nil), 400)
	})
}
func TestFederationJWKRotation(t *testing.T) {
	f := fixture(t, "")
	f.login("a", nil)
	f.op.Rotate(t)
	f.login("a", nil)
}
func TestFederationAuditFailureRollsBack(t *testing.T) {
	f := fixture(t, "")
	testdb.FailAudit(t, f.db)
	q, c := f.begin("a")
	checkStatus(t, f.callback("a", q, c, nil), 503)
	for _, table := range []string{"external_identities", "browser_sessions", "federation_admissions"} {
		var n int
		f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
		if n != 0 {
			t.Fatalf("%s not rolled back", table)
		}
	}
}

func TestFederationRoleOwnership(t *testing.T) {
	f := fixture(t, "")
	f.exec(`UPDATE oidc_providers SET role_map='{"/admins":"admin"}' WHERE id='provider-a'`)
	f.login("a", map[string]any{"groups": []string{"/admins"}})
	role := func() string {
		var r string
		f.db.QueryRow(`SELECT role FROM org_members WHERE org_id='a' AND user_id!='local'`).Scan(&r)
		return r
	}
	if role() != "admin" {
		t.Fatal("configured full claim did not grant admin")
	}
	f.login("a", map[string]any{"groups": []string{"/other/admins"}})
	if role() != "viewer" {
		t.Fatal("unconfigured basename retained elevated role")
	}
	var uid string
	f.db.QueryRow(`SELECT user_id FROM external_identities`).Scan(&uid)
	token, _ := tc.SignJWT("local", "collision@example.test", "fixture-jwt", time.Minute)
	req := httptest.NewRequest("PATCH", "/api/v1/orgs/a/members/"+uid, strings.NewReader(`{"role":"developer"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	checkStatus(t, w, 200)
	f.login("a", map[string]any{"groups": []string{"/admins"}})
	if role() != "developer" {
		t.Fatal("federation overwrote explicit local role")
	}
}
func TestFederationLocalTokenSeparation(t *testing.T) {
	f := fixture(t, "")
	f.login("a", nil)
	var uid string
	f.db.QueryRow(`SELECT user_id FROM external_identities`).Scan(&uid)
	for _, pair := range []struct {
		sub, typ string
		status   int
	}{{"local", "user", 200}, {"local", "totp_challenge", 401}, {"local", "oidc_state", 401}, {uid, "user", 401}} {
		raw, _ := tc.SignJWTClaims(map[string]any{"sub": pair.sub, "typ": pair.typ}, "fixture-jwt", time.Minute)
		r := httptest.NewRequest("GET", "/api/v1/me", nil)
		r.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		checkStatus(t, w, pair.status)
	}
}
func TestFederationExplicitPolicy(t *testing.T) {
	f := fixture(t, "")
	cookie := f.login("a", nil)
	// Removing the operator trust/profile takes effect even for an old session.
	f.h = New(&Deps{DB: f.db, MasterKey: f.master, JWTSecret: "fixture-jwt", PublicOrigin: fixtureOrigin, SessionProfile: "BOUNDED", DEKs: tc.NewDEKCache(), LoginRate: 1000})
	checkStatus(t, f.request("GET", "/api/v1/me", cookie, false), 401)
	checkStatus(t, f.request("GET", "/api/v1/auth/oidc/a/login", nil, false), 404)
	w := f.request("GET", "/api/v1/auth/session", cookie, false)
	if !strings.Contains(w.Body.String(), `"authenticated":false`) || !strings.Contains(w.Body.String(), `"authentication":"oidc"`) {
		t.Fatal("invalid external session can fall back to local storage")
	}
}

func TestFederationProviderPatchAndAudit(t *testing.T) {
	f := fixture(t, "")
	f.exec(`UPDATE oidc_providers SET role_map='{"/team/admin":"admin"}' WHERE id='provider-a'`)
	token, _ := tc.SignJWT("local", "collision@example.test", "fixture-jwt", time.Minute)
	patch := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PATCH", "/api/v1/orgs/a/oidc/provider-a", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		return w
	}
	checkStatus(t, patch(`{"public_login":false}`), 200)
	var mapping string
	f.db.QueryRow(`SELECT role_map FROM oidc_providers WHERE id='provider-a'`).Scan(&mapping)
	if mapping != `{"/team/admin":"admin"}` {
		t.Fatal("unrelated patch erased mapping")
	}
	testdb.FailAudit(t, f.db)
	checkStatus(t, patch(`{"enabled":false}`), 503)
	var enabled int
	f.db.QueryRow(`SELECT enabled FROM oidc_providers WHERE id='provider-a'`).Scan(&enabled)
	if enabled != 1 {
		t.Fatal("unaudited configuration committed")
	}
}

func TestFederationOrganizationChoicesDoNotChangeScope(t *testing.T) {
	f := fixture(t, "")
	a := f.login("a", nil)
	checkStatus(t, f.request("GET", "/api/v1/auth/organizations", nil, false), 401)
	choices := f.request("GET", "/api/v1/auth/organizations", a, false)
	checkStatus(t, choices, 200)
	if strings.Contains(choices.Body.String(), `"id":"b"`) {
		t.Fatal("unjoined organization exposed")
	}
	_ = f.login("b", nil)
	choices = f.request("GET", "/api/v1/auth/organizations", a, false)
	checkStatus(t, choices, 200)
	if !strings.Contains(choices.Body.String(), `"id":"b"`) {
		t.Fatal("joined organization absent")
	}
	checkStatus(t, f.request("GET", "/api/v1/orgs/b/projects", a, false), 403)
	f.exec(`UPDATE oidc_providers SET enabled=0 WHERE id='provider-b'`)
	choices = f.request("GET", "/api/v1/auth/organizations", a, false)
	if strings.Contains(choices.Body.String(), `"id":"b"`) {
		t.Fatal("disabled connection exposed")
	}
}
