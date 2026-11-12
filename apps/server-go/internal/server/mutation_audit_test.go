package server

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestGovernanceMutationsRollbackWithAudit(t *testing.T) {
	for _, c := range []struct{ name, method, path, body, table string }{
		{"project", "POST", "/api/v1/orgs/a/projects", `{"name":"atomic"}`, "projects"},
		{"folder", "POST", "/api/v1/projects/project-a/folders", `{"path":"/parent/child/"}`, "folders"},
		{"identity", "POST", "/api/v1/orgs/a/identities", `{"name":"atomic","scopes":[{"project_id":"project-a","permission":"read"}]}`, "machine_identities"},
		{"sync", "POST", "/api/v1/orgs/a/sync/targets", `{"name":"atomic","platform":"github","config":{"token":"public-fixture","owner":"fixture","repo":"fixture"}}`, "sync_targets"},
		{"webhook", "POST", "/api/v1/orgs/a/webhooks", `{"url":"https://fixture.example/hook","events":["*"]}`, "webhooks"},
		{"dynamic", "POST", "/api/v1/projects/project-a/dynamic-engines", `{"name":"atomic","connection_string":"postgres://fixture@127.0.0.1:1/fixture"}`, "dynamic_engines"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := fixture(t, "")
			var before int
			f.db.QueryRow(`SELECT COUNT(*) FROM ` + c.table).Scan(&before)
			testdb.FailAudit(t, f.db)
			w := f.localRequest(c.method, c.path, c.body, "")
			checkStatus(t, w, 503)
			var after int
			f.db.QueryRow(`SELECT COUNT(*) FROM ` + c.table).Scan(&after)
			if before != after {
				t.Fatal("mutation survived failed audit")
			}
			if c.name == "folder" {
				f.db.QueryRow(`SELECT COUNT(*) FROM folders`).Scan(&after)
				if after != 0 {
					t.Fatal("parent folder leaked")
				}
			}
		})
	}
}
func TestRegistrationAuditFailureLeavesNoAccount(t *testing.T) {
	f := fixture(t, "")
	testdb.FailAudit(t, f.db)
	r := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(`{"email":"atomic@example.test","password":"fixture-password","name":"Fixture"}`))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	checkStatus(t, w, 503)
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email='atomic@example.test'`).Scan(&n)
	if n != 0 {
		t.Fatal("account survived failed audit")
	}
	f.db.QueryRow(`SELECT COUNT(*) FROM orgs WHERE slug='atomic'`).Scan(&n)
	if n != 0 {
		t.Fatal("organization survived failed audit")
	}
}
func TestGovernanceDeletionAndRevocationRollback(t *testing.T) {
	for _, kind := range []string{"identity", "sync", "webhook", "folder"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "")
			create, body, remove, table := "", "", "", ""
			switch kind {
			case "identity":
				create = "/api/v1/orgs/a/identities"
				body = `{"name":"fixture","scopes":[{"project_id":"project-a","permission":"read"}]}`
				remove = "/api/v1/orgs/a/identities/%s/revoke"
				table = "machine_identities"
			case "sync":
				create = "/api/v1/orgs/a/sync/targets"
				body = `{"name":"fixture","platform":"github","config":{"token":"public-fixture","owner":"fixture","repo":"fixture"}}`
				remove = "/api/v1/orgs/a/sync/targets/%s"
				table = "sync_targets"
			case "webhook":
				create = "/api/v1/orgs/a/webhooks"
				body = `{"url":"https://fixture.example/hook","events":["*"]}`
				remove = "/api/v1/orgs/a/webhooks/%s"
				table = "webhooks"
			case "folder":
				create = "/api/v1/projects/project-a/folders"
				body = `{"path":"/empty/"}`
				remove = "/api/v1/projects/project-a/folders?path=/empty/"
				table = "folders"
			}
			w := f.localRequest("POST", create, body, "")
			checkStatus(t, w, 201)
			var created struct {
				ID string `json:"id"`
			}
			json.Unmarshal(w.Body.Bytes(), &created)
			testdb.FailAudit(t, f.db)
			method := "DELETE"
			if kind == "identity" {
				method = "POST"
			}
			if kind != "folder" {
				remove = fmt.Sprintf(remove, created.ID)
			}
			checkStatus(t, f.localRequest(method, remove, "", ""), 503)
			var count int
			f.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id=?`, created.ID).Scan(&count)
			if count != 1 {
				t.Fatal("row deleted without audit")
			}
			if kind == "identity" {
				var status string
				f.db.QueryRow(`SELECT status FROM machine_identities WHERE id=?`, created.ID).Scan(&status)
				if status != "active" {
					t.Fatal("status changed without audit")
				}
			}
		})
	}
}
func TestFolderMoveUsesLiteralUnicodePrefixAndAudit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			f := fixture(t, "")
			dek, _ := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
			f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
			for _, path := range []string{"/汉_%/nested/", "/汉X/"} {
				raw, _ := json.Marshal(map[string]string{"path": path})
				checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/folders", string(raw), ""), 201)
			}
			if fail {
				testdb.FailAudit(t, f.db)
			}
			expected := 200
			if fail {
				expected = 503
			}
			checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/folders/move", `{"from":"/汉_%/","to":"/新/目标/"}`, ""), expected)
			var n int
			f.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE path='/汉X/'`).Scan(&n)
			if n != 1 {
				t.Fatal("wildcard matched unrelated path")
			}
			want := "/新/目标/nested/"
			if fail {
				want = "/汉_%/nested/"
			}
			f.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE path=?`, want).Scan(&n)
			if n != 1 {
				t.Fatal("literal Unicode prefix not preserved")
			}
			if fail {
				f.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE path='/新/'`).Scan(&n)
				if n != 0 {
					t.Fatal("new parent survived rollback")
				}
			}
		})
	}
}
