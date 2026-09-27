package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func (f *federationFixture) sessionPolicy(profile string, extra map[string]any) {
	f.t.Helper()
	p := map[string]any{"profile": profile, "session_ttl": 900, "max_auth_age": 900, "required_acr": []string{}, "required_amr": []string{}, "online_audience": "taboo-test", "rp_logout": false}
	for k, v := range extra {
		p[k] = v
	}
	body, _ := json.Marshal(p)
	checkStatus(f.t, f.localRequest("PUT", "/api/v1/orgs/a/oidc/provider-a/security", string(body), ""), 200)
}

func (f *federationFixture) alterCredential(c *http.Cookie, change func(map[string]any)) {
	f.t.Helper()
	hash := tc.SHA256(c.Value)
	var ciphertext string
	var revision int
	if err := f.db.QueryRow(`SELECT ciphertext,revision FROM browser_credentials WHERE token_hash=?`, hash).Scan(&ciphertext, &revision); err != nil {
		f.t.Fatal(err)
	}
	context := tc.Context{Purpose: "oidc-session", Org: "a", Object: hash, Version: revision}
	raw, err := tc.Open(f.master, ciphertext, context)
	if err != nil {
		f.t.Fatal(err)
	}
	var data map[string]any
	if json.Unmarshal([]byte(raw), &data) != nil {
		f.t.Fatal("invalid session")
	}
	change(data)
	plain, _ := json.Marshal(data)
	encrypted, err := tc.Seal(f.master, string(plain), context)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`UPDATE browser_credentials SET ciphertext=? WHERE token_hash=?`, encrypted, hash)
}

func TestFederationOnlineAndBoundedRevocation(t *testing.T) {
	for _, profile := range []string{"BOUNDED", "ONLINE_REQUIRED"} {
		t.Run(profile, func(t *testing.T) {
			f := fixture(t, "")
			f.sessionPolicy(profile, nil)
			c := f.login("a", nil)
			checkStatus(t, f.request("GET", "/api/v1/me", c, false), 200)
			f.op.Disable()
			expected := 200
			if profile == "ONLINE_REQUIRED" {
				expected = 401
			}
			checkStatus(t, f.request("GET", "/api/v1/me", c, false), expected)
			f.op.Outage()
			expected = 200
			if profile == "ONLINE_REQUIRED" {
				expected = 503
			}
			checkStatus(t, f.request("GET", "/api/v1/me", c, false), expected)
			// Local expiry is authoritative regardless of upstream state.
			f.exec(`UPDATE browser_sessions SET expires_at=? WHERE token_hash=?`, time.Now().Unix()-1, tc.SHA256(c.Value))
			checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
		})
	}
}

func TestFederationStrongAuthenticationProof(t *testing.T) {
	f := fixture(t, "")
	f.sessionPolicy("BOUNDED", map[string]any{"required_acr": []string{"urn:fixture:loa2"}, "required_amr": []string{"pwd", "otp"}, "max_auth_age": 300})
	weak := f.login("a", map[string]any{"acr": "urn:fixture:loa1", "amr": []string{"pwd"}})
	var uid string
	f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='subject-a'`).Scan(&uid)
	checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/members/"+uid, `{"role":"owner"}`, ""), 200)
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"LOCAL_OR_EXTERNAL"}`, weak), 403)
	strong := f.login("a", map[string]any{"acr": "urn:fixture:loa2", "amr": []string{"pwd", "otp"}})
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"LOCAL_OR_EXTERNAL"}`, strong), 200)
	f.alterCredential(strong, func(b map[string]any) { b["AuthTime"] = time.Now().Unix() - 301 })
	checkStatus(t, f.request("GET", "/api/v1/me", strong, false), 200)
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"LOCAL_OR_EXTERNAL"}`, strong), 403)
	for _, value := range []any{nil, time.Now().Unix() + 60, time.Now().Unix() - 3600} {
		q, c := f.begin("a")
		checkStatus(t, f.callback("a", q, c, map[string]any{"auth_time": value}), 401)
	}
}

func TestFederationRefreshRotatesOnceAndKeepsDeadline(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	hash := tc.SHA256(c.Value)
	var deadline int64
	f.db.QueryRow(`SELECT expires_at FROM browser_sessions WHERE token_hash=?`, hash).Scan(&deadline)
	f.alterCredential(c, func(b map[string]any) { b["IdentityExpires"] = time.Now().Unix() - 1 })
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- f.request("GET", "/api/v1/me", c, false).Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 200 && code != 503 {
			t.Fatalf("concurrent refresh status %d", code)
		}
	}
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 200)
	if f.op.RefreshCount() != 1 {
		t.Fatal("refresh token exchanged more than once")
	}
	var after int64
	var revision int
	f.db.QueryRow(`SELECT expires_at FROM browser_sessions WHERE token_hash=?`, hash).Scan(&after)
	f.db.QueryRow(`SELECT revision FROM browser_credentials WHERE token_hash=?`, hash).Scan(&revision)
	if after != deadline || revision != 2 {
		t.Fatal("refresh extended session or failed CAS")
	}
	f.op.RefreshOverrides(map[string]any{"sub": "another-subject"})
	f.alterCredential(c, func(b map[string]any) { b["IdentityExpires"] = time.Now().Unix() - 1 })
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 401)
	if f.op.RefreshCount() != 2 {
		t.Fatal("failed refresh was replayed")
	}
}

func TestFederationRefreshAuditFailureDoesNotReuseToken(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	f.alterCredential(c, func(b map[string]any) { b["IdentityExpires"] = time.Now().Unix() - 1 })
	testdb.FailAudit(t, f.db)
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 503)
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 503)
	if f.op.RefreshCount() != 1 {
		t.Fatal("refresh reused after uncertain durable result")
	}
}

func TestFederationLogoutWorksDuringOutage(t *testing.T) {
	f := fixture(t, "")
	f.sessionPolicy("ONLINE_REQUIRED", map[string]any{"rp_logout": true})
	c := f.login("a", nil)
	f.op.Outage()
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 503)
	checkStatus(t, f.request("POST", "/api/v1/auth/logout", c, false), 403)
	w := f.request("POST", "/api/v1/auth/logout", c, true)
	checkStatus(t, w, 200)
	var response struct {
		URL string `json:"logout_url"`
	}
	if json.Unmarshal(w.Body.Bytes(), &response) != nil {
		t.Fatal("logout response")
	}
	parsed, err := url.Parse(response.URL)
	if err != nil || parsed.Scheme+"://"+parsed.Host != f.op.Server.URL || parsed.Path != "/logout" || parsed.Query().Get("id_token_hint") == "" || parsed.Query().Get("post_logout_redirect_uri") != fixtureOrigin+"/" {
		t.Fatal("untrusted RP logout target")
	}
	if cookieOf(t, w, auth.BrowserCookie).MaxAge != -1 {
		t.Fatal("local cookie retained")
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM browser_sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("local session survived logout")
	}
	f.db.QueryRow(`SELECT COUNT(*) FROM browser_credentials`).Scan(&n)
	if n != 0 {
		t.Fatal("encrypted provider token survived logout")
	}
}
