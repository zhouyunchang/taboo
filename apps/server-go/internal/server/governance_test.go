package server

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func (f *federationFixture) browserRequest(method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.AddCookie(c)
	r.Header.Set("Origin", fixtureOrigin)
	r.Header.Set("X-CSRF-Token", auth.BrowserCSRF(c.Value))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func TestFederationExplicitActionsAndRequiredLogin(t *testing.T) {
	f := fixture(t, "")
	c := f.login("a", nil)
	var uid string
	if err := f.db.QueryRow(`SELECT user_id FROM external_identities WHERE subject='subject-a'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	dek, err := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
	checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/members/"+uid, `{"role":"developer"}`, ""), 200)
	path := "/api/v1/projects/project-a/secrets"
	checkStatus(t, f.browserRequest("POST", path, `{"key":"K","value":"private"}`, c), 403)
	grants := []map[string]string{}
	grant := func(action string) {
		t.Helper()
		grants = append(grants, map[string]string{"project_id": "project-a", "action": action})
		b, _ := json.Marshal(map[string]any{"grants": grants})
		checkStatus(t, f.localRequest("PUT", "/api/v1/orgs/a/members/"+uid+"/actions", string(b), ""), 200)
	}
	grant("read")
	grant("write")
	checkStatus(t, f.browserRequest("POST", path, `{"key":"K","value":"private"}`, c), 200)
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/secrets/K", "", c), 403)
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/export", "", c), 403)
	checkStatus(t, f.browserRequest("POST", "/api/v1/projects/project-a/secrets/K/rollback", `{"version":1}`, c), 403)
	checkStatus(t, f.browserRequest("DELETE", "/api/v1/projects/project-a/secrets/K", "", c), 403)
	grant("reveal")
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/secrets/K", "", c), 200)
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/export", "", c), 403)
	grant("export")
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/export", "", c), 200)
	checkStatus(t, f.localRequest("PUT", "/api/v1/orgs/a/members/"+uid+"/actions", `{"grants":[]}`, ""), 200)
	checkStatus(t, f.browserRequest("GET", "/api/v1/projects/project-a/secrets/K", "", c), 403)
	checkStatus(t, f.localRequest("PUT", "/api/v1/orgs/a/members/"+uid+"/actions", `{"grants":[{"project_id":"project-b","action":"reveal"}]}`, ""), 400)
	// Enabling external-only access requires proof from an existing external owner.
	checkStatus(t, f.localRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"EXTERNAL_REQUIRED"}`, ""), 409)
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"EXTERNAL_REQUIRED"}`, c), 403)
	checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/members/"+uid, `{"role":"owner"}`, ""), 200)
	checkStatus(t, f.browserRequest("PUT", "/api/v1/orgs/a/security", `{"login_policy":"EXTERNAL_REQUIRED"}`, c), 200)
	for _, p := range []string{"/api/v1/orgs/a/projects", "/api/v1/orgs/a/members", "/api/v1/orgs/a/security", path} {
		checkStatus(t, f.localRequest("GET", p, "", ""), 403)
	}
	me := f.localRequest("GET", "/api/v1/me", "", "")
	checkStatus(t, me, 200)
	var info struct {
		Orgs []any `json:"orgs"`
	}
	json.Unmarshal(me.Body.Bytes(), &info)
	if len(info.Orgs) != 0 {
		t.Fatal("external-only organization exposed in local session")
	}
	checkStatus(t, f.browserRequest("GET", "/api/v1/orgs/a/projects", "", c), 200)
	checkStatus(t, f.browserRequest("PATCH", "/api/v1/orgs/a/members/"+uid, `{"role":"developer"}`, c), 409)
	checkStatus(t, f.browserRequest("DELETE", "/api/v1/orgs/a/members/"+uid, "", c), 409)
	// Upstream configuration disable cannot reactivate the local fallback.
	f.exec(`UPDATE oidc_providers SET enabled=0 WHERE id='provider-a'`)
	checkStatus(t, f.browserRequest("GET", "/api/v1/orgs/a/projects", "", c), 401)
	checkStatus(t, f.localRequest("GET", "/api/v1/orgs/a/projects", "", ""), 403)
}

func TestMemberMutationsRollbackWithAudit(t *testing.T) {
	for _, operation := range []string{"add", "remove", "actions"} {
		t.Run(operation, func(t *testing.T) {
			f := fixture(t, "")
			f.exec(`INSERT INTO users(id,email,password_hash) VALUES('other','other@test.example','!')`)
			if operation != "add" {
				f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','other','viewer')`)
			}
			testdb.FailAudit(t, f.db)
			var w *httptest.ResponseRecorder
			switch operation {
			case "add":
				w = f.localRequest("POST", "/api/v1/orgs/a/members", `{"email":"other@test.example","role":"developer"}`, "")
			case "remove":
				w = f.localRequest("DELETE", "/api/v1/orgs/a/members/other", "", "")
			case "actions":
				w = f.localRequest("PUT", "/api/v1/orgs/a/members/other/actions", `{"grants":[{"project_id":"project-a","action":"write"}]}`, "")
			}
			if w.Code < 500 {
				t.Fatalf("mutation should fail closed: %d", w.Code)
			}
			var n int
			f.db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id='a' AND user_id='other'`).Scan(&n)
			want := 1
			if operation == "add" {
				want = 0
			}
			if n != want {
				t.Fatal("unaudited membership persisted")
			}
			f.db.QueryRow(`SELECT COUNT(*) FROM member_action_grants`).Scan(&n)
			if n != 0 {
				t.Fatal("unaudited grant persisted")
			}
		})
	}
}

func TestConcurrentOwnerDemotionRetainsOwner(t *testing.T) {
	f := fixture(t, "")
	f.exec(`INSERT INTO users(id,email,password_hash) VALUES('other','other@test.example','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','other','owner')`)
	var wg sync.WaitGroup
	for _, id := range []string{"local", "other"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			f.localRequest("PATCH", "/api/v1/orgs/a/members/"+id, `{"role":"viewer"}`, "")
		}(id)
	}
	wg.Wait()
	var n int
	if f.db.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id='a' AND role='owner'`).Scan(&n) != nil || n < 1 {
		t.Fatal("last owner removed by concurrent mutations")
	}
}
