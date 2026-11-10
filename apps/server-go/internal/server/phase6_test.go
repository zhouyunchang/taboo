package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/govern"
)

func TestPhase6ApprovalAccessAndSCIM(t *testing.T) {
	f := phase4Ready(t)
	checkStatus(t, f.localRequest("PATCH", "/api/v1/projects/project-a", `{"approval_required":true}`, ""), 200)
	queued := f.localRequest("POST", "/api/v1/projects/project-a/secrets?env=dev", `{"key":"LOCKED","value":"phase6-secret","reason":"rotate"}`, "p6-queue")
	checkStatus(t, queued, 202)
	if strings.Contains(queued.Body.String(), "phase6-secret") || !strings.Contains(queued.Body.String(), `"status":"pending"`) {
		t.Fatalf("approval response: %s", queued.Body.String())
	}
	var created struct {
		ID string `json:"approval_id"`
	}
	if json.Unmarshal(queued.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatal(queued.Body.String())
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE key='LOCKED'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("secret written before approval: %d %v", n, err)
	}
	var meta string
	if err := f.db.QueryRow(`SELECT metadata FROM audit_logs WHERE action='approval.create'`).Scan(&meta); err != nil || strings.Contains(meta, "phase6-secret") {
		t.Fatalf("audit leaked plaintext: %s %v", meta, err)
	}
	checkStatus(t, f.localRequest("POST", "/api/v1/orgs/a/approvals/"+created.ID+"/approve", `{}`, ""), 403)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/import?env=dev", `{"content":"LOCKED=x\n"}`, ""), 409)

	f.exec(`INSERT INTO users(id,email,name,password_hash) VALUES('admin2','admin2@example.test','Admin','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','admin2','admin')`)
	admin := mustToken(t, "admin2", "admin2@example.test")
	checkStatus(t, f.bearer("POST", "/api/v1/orgs/a/approvals/"+created.ID+"/approve", `{}`, admin), 200)
	got := f.localRequest("GET", "/api/v1/projects/project-a/secrets/LOCKED?env=dev", "", "")
	checkStatus(t, got, 200)
	if !strings.Contains(got.Body.String(), `"value":"phase6-secret"`) {
		t.Fatalf("approved value: %s", got.Body.String())
	}

	f.exec(`INSERT INTO users(id,email,name,password_hash) VALUES('viewer2','viewer2@example.test','Viewer','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','viewer2','viewer')`)
	viewer := mustToken(t, "viewer2", "viewer2@example.test")
	checkStatus(t, f.bearer("GET", "/api/v1/projects/project-a/secrets/LOCKED?env=dev", "", viewer), 403)
	req := f.bearer("POST", "/api/v1/orgs/a/access-requests", `{"project_id":"project-a","env":"dev","action":"reveal","ttl_seconds":120,"reason":"incident"}`, viewer)
	checkStatus(t, req, 201)
	var access struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(req.Body.Bytes(), &access) != nil || access.ID == "" {
		t.Fatal(req.Body.String())
	}
	checkStatus(t, f.bearer("POST", "/api/v1/orgs/a/access-requests/"+access.ID+"/approve", `{}`, viewer), 403)
	checkStatus(t, f.bearer("POST", "/api/v1/orgs/a/access-requests/"+access.ID+"/approve", `{}`, admin), 200)
	checkStatus(t, f.bearer("GET", "/api/v1/projects/project-a/secrets/LOCKED?env=dev", "", viewer), 200)
	if _, err := f.db.Exec(`UPDATE access_requests SET expires_at=1 WHERE id=?`, access.ID); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.bearer("GET", "/api/v1/projects/project-a/secrets/LOCKED?env=dev", "", viewer), 403)
	govern.Sweep(f.db)
	var status string
	if err := f.db.QueryRow(`SELECT status FROM access_requests WHERE id=?`, access.ID).Scan(&status); err != nil || status != "expired" {
		t.Fatalf("sweep status %s %v", status, err)
	}

	tokenRes := f.localRequest("POST", "/api/v1/orgs/a/scim/tokens", `{"name":"idp","ttl_seconds":3600}`, "")
	checkStatus(t, tokenRes, 201)
	var scimToken struct {
		Token string `json:"token"`
	}
	listed := f.localRequest("GET", "/api/v1/orgs/a/scim/tokens", "", "")
	if json.Unmarshal(tokenRes.Body.Bytes(), &scimToken) != nil || !strings.HasPrefix(scimToken.Token, "taboo_scim_") || strings.Contains(listed.Body.String(), scimToken.Token) {
		t.Fatalf("token handling: %s", tokenRes.Body.String())
	}
	user := f.raw(http.MethodPost, "/api/v1/scim/v2/Users", `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"ada@example.test","externalId":"ada","name":{"formatted":"Ada"}}`, scimToken.Token)
	checkStatus(t, user, 201)
	if strings.Contains(user.Body.String(), "password") {
		t.Fatal("scim user echoed a password")
	}
	clash := f.raw(http.MethodPost, "/api/v1/scim/v2/Users", `{"userName":"collision@example.test","externalId":"takeover"}`, scimToken.Token)
	checkStatus(t, clash, 201)
	var source string
	if err := f.db.QueryRow(`SELECT auth_source FROM users WHERE email='collision@example.test'`).Scan(&source); err != nil || source != "local" {
		t.Fatalf("email takeover: %s %v", source, err)
	}
	bad := f.raw(http.MethodGet, `/api/v1/scim/v2/Users?filter=userName%20eq%20%22ada%22%20or%201=1`, "", scimToken.Token)
	checkStatus(t, bad, 400)
	var createdUser struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(user.Body.Bytes(), &createdUser) != nil || createdUser.ID == "" {
		t.Fatal(user.Body.String())
	}
	group := f.raw(http.MethodPost, "/api/v1/scim/v2/Groups", `{"displayName":"devs","urn:ietf:params:scim:schemas:extension:taboo:2.0:Group":{"role":"developer"},"members":[{"value":"`+createdUser.ID+`"}]}`, scimToken.Token)
	checkStatus(t, group, 201)
	var role string
	if err := f.db.QueryRow(`SELECT m.role FROM org_members m JOIN scim_users u ON u.user_id=m.user_id AND u.org_id=m.org_id WHERE u.user_name='ada@example.test'`).Scan(&role); err != nil || role != "developer" {
		t.Fatalf("group role %s %v", role, err)
	}
}

func TestPhase6DevicePKCEAndBackChannel(t *testing.T) {
	f := phase4Ready(t)
	started := f.raw(http.MethodPost, "/api/v1/auth/device", `{"client_id":"taboo-cli"}`, "")
	checkStatus(t, started, 200)
	var dev struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		Interval   int    `json:"interval"`
	}
	if json.Unmarshal(started.Body.Bytes(), &dev) != nil || dev.UserCode == "" || dev.Interval != 5 {
		t.Fatal(started.Body.String())
	}
	body := `{"client_id":"taboo-cli","device_code":"` + dev.DeviceCode + `","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`
	pending := f.raw(http.MethodPost, "/api/v1/auth/device/token", body, "")
	checkStatus(t, pending, 400)
	if !strings.Contains(pending.Body.String(), "authorization_pending") {
		t.Fatal(pending.Body.String())
	}
	slow := f.raw(http.MethodPost, "/api/v1/auth/device/token", body, "")
	checkStatus(t, slow, 400)
	if !strings.Contains(slow.Body.String(), "slow_down") {
		t.Fatal(slow.Body.String())
	}
	checkStatus(t, f.localRequest("POST", "/api/v1/auth/device/decision", `{"user_code":"`+dev.UserCode+`","org_slug":"a","approve":true}`, ""), 200)
	if _, err := f.db.Exec(`UPDATE device_authorizations SET last_polled_at=0`); err != nil {
		t.Fatal(err)
	}
	done := f.raw(http.MethodPost, "/api/v1/auth/device/token", body, "")
	checkStatus(t, done, 200)
	if !strings.Contains(done.Body.String(), `"access"`) {
		t.Fatal(done.Body.String())
	}

	verifier := "phase6-verifier-phase6-verifier-phase6-verifier-ok"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	redirect := "http://127.0.0.1:43111/callback"
	checkStatus(t, f.localRequest("POST", "/api/v1/auth/cli/authorize", `{"code_challenge":"`+challenge+`","redirect_uri":"https://evil.example/callback","org_slug":"a"}`, ""), 400)
	authz := f.localRequest("POST", "/api/v1/auth/cli/authorize", `{"code_challenge":"`+challenge+`","redirect_uri":"`+redirect+`","org_slug":"a"}`, "")
	checkStatus(t, authz, 200)
	var redir struct {
		Redirect string `json:"redirect"`
	}
	if json.Unmarshal(authz.Body.Bytes(), &redir) != nil || !strings.HasPrefix(redir.Redirect, redirect+"?code=") {
		t.Fatal(authz.Body.String())
	}
	u, err := url.Parse(redir.Redirect)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	exchanged := f.raw(http.MethodPost, "/api/v1/auth/cli/token", `{"client_id":"taboo-cli","code":"`+code+`","code_verifier":"`+verifier+`","redirect_uri":"`+redirect+`"}`, "")
	checkStatus(t, exchanged, 200)
	again := f.raw(http.MethodPost, "/api/v1/auth/cli/token", `{"client_id":"taboo-cli","code":"`+code+`","code_verifier":"`+verifier+`","redirect_uri":"`+redirect+`"}`, "")
	checkStatus(t, again, 400)

	sid, sub := "sid-phase6", "subject-phase6"
	f.exec(`INSERT INTO browser_sessions(token_hash,user_id,org_id,provider_id,config_hash,origin,expires_at,created_at,sid,subject) VALUES('hash-1','local','a','provider-a','rev',?,?,?,?,?)`, fixtureOrigin, time.Now().Add(time.Hour).Unix(), time.Now().Unix(), sid, sub)
	now := time.Now().Unix()
	raw, err := f.op.Sign(map[string]any{
		"iss": f.op.Server.URL, "aud": "taboo-test", "iat": now, "exp": now + 60,
		"sub": sub, "sid": sid, "jti": "logout-1",
		"events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.form("/api/v1/auth/oidc/a/backchannel-logout?pid=provider-a", url.Values{"logout_token": {raw}}), 200)
	var left int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM browser_sessions WHERE sid=?`, sid).Scan(&left); err != nil || left != 0 {
		t.Fatalf("session survived back-channel logout: %d %v", left, err)
	}
	checkStatus(t, f.form("/api/v1/auth/oidc/a/backchannel-logout?pid=provider-a", url.Values{"logout_token": {raw}}), 400)
	nonce, err := f.op.Sign(map[string]any{
		"iss": f.op.Server.URL, "aud": "taboo-test", "iat": now, "exp": now + 60,
		"sub": sub, "jti": "logout-2", "nonce": "nope",
		"events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.form("/api/v1/auth/oidc/a/backchannel-logout?pid=provider-a", url.Values{"logout_token": {nonce}}), 400)
}

func mustToken(t *testing.T, id, email string) string {
	t.Helper()
	token, err := tc.SignJWT(id, email, "fixture-jwt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (f *federationFixture) raw(method, path, body, bearer string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *federationFixture) form(path string, v url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}
