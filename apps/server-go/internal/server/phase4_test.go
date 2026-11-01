package server

import (
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/scanreport"
)

func (f *federationFixture) bearer(method, path, body, token string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func phase4Ready(t *testing.T) *federationFixture {
	t.Helper()
	f := fixture(t, "")
	dek, err := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
	return f
}

func TestSecretReferencesImportAndVersions(t *testing.T) {
	f := phase4Ready(t)
	put := func(path, body string) {
		t.Helper()
		checkStatus(t, f.localRequest("POST", path, body, ""), 200)
	}
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"DEV_BASE","value":"base-value"}`)
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"DEV_REF","value":"${dev:DEV_BASE}"}`)
	raw := f.localRequest("GET", "/api/v1/projects/project-a/secrets/DEV_REF?env=dev", "", "")
	checkStatus(t, raw, 200)
	if !strings.Contains(raw.Body.String(), `"value":"${dev:DEV_BASE}"`) || strings.Contains(raw.Body.String(), "base-value") {
		t.Fatalf("unresolved reveal: %s", raw.Body.String())
	}
	got := f.localRequest("GET", "/api/v1/projects/project-a/secrets/DEV_REF?env=dev&resolve=true", "", "")
	checkStatus(t, got, 200)
	if !strings.Contains(got.Body.String(), `"value":"base-value"`) || !strings.Contains(got.Body.String(), `"resolved":true`) {
		t.Fatalf("resolved reveal: %s", got.Body.String())
	}
	exported := f.localRequest("GET", "/api/v1/projects/project-a/export?env=dev&resolve=true", "", "")
	checkStatus(t, exported, 200)
	if !strings.Contains(exported.Body.String(), "DEV_REF=base-value\n") || strings.Contains(exported.Body.String(), "${dev:DEV_BASE}") {
		t.Fatalf("resolved export: %s", exported.Body.String())
	}
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"CYCLE_A","value":"${dev:CYCLE_B}"}`)
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"CYCLE_B","value":"${dev:CYCLE_A}"}`)
	cycle := f.localRequest("GET", "/api/v1/projects/project-a/secrets/CYCLE_A?env=dev&resolve=true", "", "")
	checkStatus(t, cycle, 400)
	if !strings.Contains(cycle.Body.String(), "REFERENCE_CYCLE") {
		t.Fatalf("cycle: %s", cycle.Body.String())
	}
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"MISS","value":"${dev:NO_SUCH}"}`)
	missing := f.localRequest("GET", "/api/v1/projects/project-a/secrets/MISS?env=dev&resolve=true", "", "")
	checkStatus(t, missing, 404)
	if !strings.Contains(missing.Body.String(), "REFERENCE_NOT_FOUND") || !strings.Contains(missing.Body.String(), "${dev:NO_SUCH}") {
		t.Fatalf("missing: %s", missing.Body.String())
	}
	created := f.localRequest("POST", "/api/v1/orgs/a/projects", `{"name":"Other"}`, "")
	checkStatus(t, created, 201)
	var project struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &project); err != nil || project.Slug != "other" {
		t.Fatalf("project: %s %v", created.Body.String(), err)
	}
	const hidden = "hidden-cross-project-value"
	put("/api/v1/projects/"+project.ID+"/secrets?env=dev", `{"key":"SECRET","value":"`+hidden+`"}`)
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"XREF","value":"${other:dev:SECRET}"}`)
	owner := f.localRequest("GET", "/api/v1/projects/project-a/secrets/XREF?env=dev&resolve=true", "", "")
	checkStatus(t, owner, 200)
	if !strings.Contains(owner.Body.String(), hidden) {
		t.Fatalf("owner should resolve cross project: %s", owner.Body.String())
	}
	ident := f.localRequest("POST", "/api/v1/orgs/a/identities", `{"name":"limited","token_ttl":900,"scopes":[{"project_id":"project-a","env":"dev","permission":"write"}]}`, "")
	checkStatus(t, ident, 201)
	var creds struct {
		ID           string `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(ident.Body.Bytes(), &creds); err != nil {
		t.Fatal(err)
	}
	tokenRes := f.localRequest("POST", "/api/v1/identities/token", `{"client_id":"`+creds.ClientID+`","client_secret":"`+creds.ClientSecret+`"}`, "")
	checkStatus(t, tokenRes, 200)
	var tok struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenRes.Body.Bytes(), &tok); err != nil || tok.Access == "" {
		t.Fatalf("token: %s", tokenRes.Body.String())
	}
	denied := f.bearer("GET", "/api/v1/projects/project-a/secrets/XREF?env=dev&resolve=true", "", tok.Access)
	checkStatus(t, denied, 403)
	if !strings.Contains(denied.Body.String(), "REFERENCE_FORBIDDEN") || strings.Contains(denied.Body.String(), hidden) {
		t.Fatalf("identity leaked or wrong code: %s", denied.Body.String())
	}
	var leaked int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE metadata LIKE ?`, "%"+hidden+"%").Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("hidden value stored in audit: %d %v", leaked, err)
	}
	f.exec(`INSERT INTO users(id,email,name,password_hash,status,auth_source) VALUES('viewer1','viewer@example.test','Viewer','!','active','local')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','viewer1','viewer')`)
	viewer, err := tc.SignJWT("viewer1", "viewer@example.test", "fixture-jwt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	put("/api/v1/projects/project-a/secrets?env=dev", `{"key":"DEV_BASE","value":"base-value-v2","expectedVersion":1}`)
	checkStatus(t, f.bearer("GET", "/api/v1/projects/project-a/secrets/DEV_BASE/versions?env=dev", "", viewer), 200)
	secretBody := f.bearer("GET", "/api/v1/projects/project-a/secrets/DEV_BASE/versions/1?env=dev", "", viewer)
	checkStatus(t, secretBody, 403)
	if strings.Contains(secretBody.Body.String(), "base-value") {
		t.Fatalf("viewer saw version plaintext: %s", secretBody.Body.String())
	}
	v1 := f.localRequest("GET", "/api/v1/projects/project-a/secrets/DEV_BASE/versions/1?env=dev", "", "")
	checkStatus(t, v1, 200)
	if !strings.Contains(v1.Body.String(), `"value":"base-value"`) {
		t.Fatalf("version 1: %s", v1.Body.String())
	}

	preview := f.localRequest("POST", "/api/v1/projects/project-a/import?env=dev", `{"content":"NEW_KEY=one\nDEV_BASE=clash\n","preview":true}`, "")
	checkStatus(t, preview, 200)
	if !strings.Contains(preview.Body.String(), `"action":"conflict"`) || !strings.Contains(preview.Body.String(), "NEW_KEY") {
		t.Fatalf("preview: %s", preview.Body.String())
	}
	var n int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE key='NEW_KEY'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("preview wrote: %d %v", n, err)
	}
	clash := f.localRequest("POST", "/api/v1/projects/project-a/import?env=dev", `{"content":"NEW_KEY=one\nDEV_BASE=clash\n"}`, "")
	checkStatus(t, clash, 409)
	if !strings.Contains(clash.Body.String(), "IMPORT_CONFLICT") || !strings.Contains(clash.Body.String(), "DEV_BASE") {
		t.Fatalf("conflict: %s", clash.Body.String())
	}
	skipped := f.localRequest("POST", "/api/v1/projects/project-a/import?env=dev", `{"content":"NEW_KEY=one\nDEV_BASE=clash\n","strategy":"skip"}`, "")
	checkStatus(t, skipped, 200)
	if !strings.Contains(skipped.Body.String(), `"created":1`) || !strings.Contains(skipped.Body.String(), `"skipped":1`) {
		t.Fatalf("skip: %s", skipped.Body.String())
	}
	kept := f.localRequest("GET", "/api/v1/projects/project-a/secrets/DEV_BASE?env=dev", "", "")
	if !strings.Contains(kept.Body.String(), "base-value-v2") {
		t.Fatalf("skip overwrote: %s", kept.Body.String())
	}
	over := f.localRequest("POST", "/api/v1/projects/project-a/import?env=dev", `{"content":"DEV_BASE=imported\n","strategy":"overwrite"}`, "")
	checkStatus(t, over, 200)
	after := f.localRequest("GET", "/api/v1/projects/project-a/secrets/DEV_BASE?env=dev", "", "")
	if !strings.Contains(after.Body.String(), `"value":"imported"`) {
		t.Fatalf("overwrite: %s", after.Body.String())
	}
	var audits int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='secrets.import'`).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("import audits: %d %v", audits, err)
	}
}

func TestScanReportStoresNoSecretText(t *testing.T) {
	f := phase4Ready(t)
	const leaked = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	rejected := f.localRequest("POST", "/api/v1/orgs/a/scan-reports", `{"source":"cli","findings":[{"rule_id":"aws-secret-access-key","level":"critical","file":"src/app.env","line":3,"fingerprint":"`+strings.Repeat("ab", 32)+`"}]}`, "")
	checkStatus(t, rejected, 400)
	fp := scanreport.Fingerprint(f.master, "a", "aws-secret-access-key", "src/app.env", 3, leaked)
	ok := f.localRequest("POST", "/api/v1/orgs/a/scan-reports", `{"source":"cli","findings":[{"rule_id":"aws-secret-access-key","level":"critical","file":"src/app.env","line":3,"match":"`+leaked+`"}]}`, "")
	checkStatus(t, ok, 201)
	var stored string
	if err := f.db.QueryRow(`SELECT findings_json FROM scan_reports`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, leaked) || strings.Contains(stored, "match") || !strings.Contains(stored, fp) || !strings.Contains(stored, `"alg":"hmac-sha256"`) {
		t.Fatalf("stored report: %s", stored)
	}
	list := f.localRequest("GET", "/api/v1/orgs/a/scan-reports", "", "")
	checkStatus(t, list, 200)
	if strings.Contains(list.Body.String(), leaked) || strings.Contains(list.Body.String(), fp) || !strings.Contains(list.Body.String(), `"can_delete":true`) {
		t.Fatalf("list: %s", list.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatal(ok.Body.String())
	}
	detail := f.localRequest("GET", "/api/v1/orgs/a/scan-reports/"+created.ID, "", "")
	checkStatus(t, detail, 200)
	if strings.Contains(detail.Body.String(), leaked) || !strings.Contains(detail.Body.String(), `"file":"src/app.env"`) || !strings.Contains(detail.Body.String(), `"alg":"hmac-sha256"`) {
		t.Fatalf("detail: %s", detail.Body.String())
	}
	checkStatus(t, f.localRequest("GET", "/api/v1/orgs/b/scan-reports", "", ""), 403)
	f.exec(`INSERT INTO scan_reports (id, org_id, created_by, actor_name, source, finding_count, critical_count, warning_count, findings_json) VALUES ('legacy','a','local','Local','cli',1,0,1,?)`,
		`[{"rule_id":"generic-api-key","level":"warning","file":"old.env","line":1,"fingerprint":"`+strings.Repeat("ab", 32)+`"}]`)
	legacy := f.localRequest("GET", "/api/v1/orgs/a/scan-reports/legacy", "", "")
	checkStatus(t, legacy, 200)
	if !strings.Contains(legacy.Body.String(), `"alg":"sha256"`) || strings.Contains(legacy.Body.String(), "hmac-sha256") {
		t.Fatalf("legacy report: %s", legacy.Body.String())
	}
}
