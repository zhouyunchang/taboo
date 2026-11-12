package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
)

func TestFederationCredentialDelegationRequiresLocalOwner(t *testing.T) {
	for _, role := range []string{"admin", "owner"} {
		t.Run(role, func(t *testing.T) {
			f := fixture(t, "")
			c := f.login("a", nil)
			f.exec(`UPDATE org_members SET role=? WHERE user_id<>'local'`, role)
			for _, x := range []struct{ path, body string }{
				{"/api/v1/orgs/a/identities", `{"name":"fixture","scopes":[{"project_id":"project-a","permission":"write"}]}`},
				{"/api/v1/orgs/a/sync/targets", `{"name":"fixture","platform":"github","config":{"token":"fixture","owner":"fixture","repo":"fixture"}}`},
				{"/api/v1/projects/project-a/dynamic-engines", `{"name":"fixture","connection_string":"postgres://fixture@127.0.0.1:1/db"}`},
				{"/api/v1/orgs/a/webhooks", `{"url":"https://fixture.example/hook","events":["*"]}`},
			} {
				want := 403
				if role == "owner" {
					want = 201
				}
				checkStatus(t, f.browserRequest("POST", x.path, x.body, c), want)
			}
			checkStatus(t, f.request("GET", "/api/v1/orgs/a/projects", c, false), 200)
			checkStatus(t, f.request("GET", "/api/v1/projects/project-a/secrets", c, false), 403)
		})
	}
}
func TestFederationLastExternalOwnerConnectionProtected(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	f.exec(`UPDATE org_members SET role='owner' WHERE user_id<>'local'`)
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"EXTERNAL_REQUIRED"}`, c), 200)
	checkStatus(t, f.browserRequest("PATCH", "/api/v1/orgs/a/oidc/provider-a", `{"enabled":false}`, c), 409)
	checkStatus(t, f.browserRequest("DELETE", "/api/v1/orgs/a/oidc/provider-a", "", c), 409)
	checkStatus(t, f.browserRequest("PATCH", "/api/v1/orgs/a/oidc/provider-a", `{"client_id":"changed"}`, c), 409)
	checkStatus(t, f.request("GET", "/api/v1/me", c, false), 200)
}
func TestLocalFactorAuditAndConcurrentConsumption(t *testing.T) {
	t.Run("setup_rollback", func(t *testing.T) {
		f := fixture(t, "")
		testdb.FailAudit(t, f.db)
		checkStatus(t, f.localRequest("POST", "/api/v1/auth/totp/setup", "", ""), 503)
		var pending string
		f.db.QueryRow(`SELECT COALESCE(totp_pending_enc,'') FROM users WHERE id='local'`).Scan(&pending)
		if pending != "" {
			t.Fatal("unrecorded factor setup")
		}
	})
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent_login", true: "login_audit_failure"}[fail], func(t *testing.T) {
			f := fixture(t, "")
			secret, _ := totp.GenerateSecret()
			enc, _ := tc.Seal(f.master, secret, tc.Context{Purpose: "user-totp", Object: "local", Version: 1})
			f.exec(`UPDATE users SET totp_secret_enc=?,totp_enabled=1 WHERE id='local'`, enc)
			challenge, _ := tc.SignJWTClaims(map[string]any{"sub": "local", "typ": "totp"}, "fixture-jwt", time.Minute)
			code, _ := totp.Code(secret, totp.NowStep())
			body, _ := json.Marshal(map[string]string{"challenge": challenge, "code": code})
			if fail {
				testdb.FailAudit(t, f.db)
			}
			var wg sync.WaitGroup
			statuses := make(chan int, 8)
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r := httptest.NewRequest("POST", "/api/v1/auth/totp/login", strings.NewReader(string(body)))
					w := httptest.NewRecorder()
					f.h.ServeHTTP(w, r)
					statuses <- w.Code
				}()
			}
			wg.Wait()
			close(statuses)
			successes := 0
			for status := range statuses {
				if status == 200 {
					successes++
				} else if status != 401 && !(fail && status == 503) {
					t.Fatal("unexpected login result", status)
				}
			}
			if fail && successes != 0 || !fail && successes != 1 {
				t.Fatal("factor consumption not atomic", successes)
			}
			var n int
			f.db.QueryRow(`SELECT COUNT(*) FROM refresh_tokens WHERE user_id='local'`).Scan(&n)
			if n != successes {
				t.Fatal("unrecorded login token persisted")
			}
		})
	}
}
