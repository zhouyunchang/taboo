package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestPhase5ImportRotationAndScanBoundaries(t *testing.T) {
	f := phase4Ready(t)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"KEEP","value":"keep-me","expectedVersion":0}`, "p5-keep"), 200)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/import", `{"content":"KEEP=new\nFRESH=1\n","format":"dotenv"}`, "p5-conflict"), 409)
	var fresh int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE key = 'FRESH'`).Scan(&fresh); err != nil || fresh != 0 {
		t.Fatalf("conflict wrote a secret: %d %v", fresh, err)
	}
	preview := f.localRequest("POST", "/api/v1/projects/project-a/import", `{"format":"doppler","preview":true,"content":"{\"API_TOKEN\":\"doppler-value\",\"DOPPLER_PROJECT\":\"demo\"}"}`, "")
	checkStatus(t, preview, 200)
	if !strings.Contains(preview.Body.String(), `"key":"API_TOKEN"`) || strings.Contains(preview.Body.String(), "doppler-value") {
		t.Fatalf("doppler preview: %s", preview.Body.String())
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE key='API_TOKEN'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("preview wrote %d %v", n, err)
	}
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/import", `{"format":"1password","strategy":"skip","content":"{\"title\":\"API\",\"fields\":[{\"label\":\"api key\",\"value\":\"op-value\"}]}"}`, "p5-op"), 200)
	got := f.localRequest("GET", "/api/v1/projects/project-a/secrets/API_KEY", "", "")
	checkStatus(t, got, 200)
	if !strings.Contains(got.Body.String(), `"value":"op-value"`) {
		t.Fatalf("1password import: %s", got.Body.String())
	}

	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"ROTATE_ME","value":"rotation-plaintext","expectedVersion":0}`, "p5-rot"), 200)
	checkStatus(t, f.localRequest("PUT", "/api/v1/projects/project-a/secrets/ROTATE_ME/rotation", `{"status":"active","mode":"manual","interval_seconds":3600}`, "p5-policy"), 200)
	if _, err := f.db.Exec(`UPDATE rotation_policies SET next_run_at = 1`); err != nil {
		t.Fatal(err)
	}
	(&rotation.Service{DB: f.db}).Sweep(context.Background())
	var payload string
	if err := f.db.QueryRow(`SELECT payload FROM outbox_events WHERE payload LIKE '%rotation_due%'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "rotation-plaintext") || !strings.Contains(payload, `"Action":"rotation_due"`) {
		t.Fatalf("rotation event: %s", payload)
	}
	var audits int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'rotation.run.created'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("rotation audits %d %v", audits, err)
	}

	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"REF_TARGET","value":"resolved-target","expectedVersion":0}`, "p5-target"), 200)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"REF_SRC","value":"${dev:REF_TARGET}","expectedVersion":0}`, "p5-src"), 200)
	ver := f.localRequest("GET", "/api/v1/projects/project-a/secrets/REF_SRC/versions/1?resolve=true", "", "")
	checkStatus(t, ver, 200)
	if !strings.Contains(ver.Body.String(), `"value":"resolved-target"`) || !strings.Contains(ver.Body.String(), `"resolved":true`) {
		t.Fatalf("version resolve: %s", ver.Body.String())
	}
	var meta string
	if err := f.db.QueryRow(`SELECT metadata FROM audit_logs WHERE action='secrets.reveal' AND resource LIKE '%REF_SRC%' ORDER BY rowid DESC LIMIT 1`).Scan(&meta); err != nil || !strings.Contains(meta, `"resolved":true`) {
		t.Fatalf("version reveal audit: %s %v", meta, err)
	}
	plain := f.localRequest("GET", "/api/v1/projects/project-a/secrets/REF_SRC/versions/1", "", "")
	checkStatus(t, plain, 200)
	if err := f.db.QueryRow(`SELECT metadata FROM audit_logs WHERE action='secrets.reveal' AND resource LIKE '%REF_SRC%' ORDER BY rowid DESC LIMIT 1`).Scan(&meta); err != nil || !strings.Contains(meta, `"resolved":false`) || strings.Contains(plain.Body.String(), "resolved-target") {
		t.Fatalf("unresolved version: body=%s meta=%s err=%v", plain.Body.String(), meta, err)
	}

	engine := f.localRequest("POST", "/api/v1/projects/project-a/dynamic-engines", `{"name":"mysql-main","type":"mysql","connection_string":"mysql://root:secret@127.0.0.1:1/leasedb","database":"leasedb"}`, "p5-mysql")
	checkStatus(t, engine, 201)
	if !strings.Contains(engine.Body.String(), `"type":"mysql"`) || strings.Contains(engine.Body.String(), "root:secret") {
		t.Fatalf("engine: %s", engine.Body.String())
	}
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/dynamic-engines", `{"name":"nope","type":"oracle","connection_string":"oracle://x","database":"x"}`, "p5-oracle"), 400)

	health := f.localRequest("GET", "/api/v1/healthz", "", "")
	if health.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control %q", health.Header().Get("Cache-Control"))
	}
	missing := httptest.NewRecorder()
	f.h.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil))
	if missing.Code != 404 || missing.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("404 cache %d %q", missing.Code, missing.Header().Get("Cache-Control"))
	}
}

func TestMachineTokenAllowlistAndScanVisibility(t *testing.T) {
	f := phase4Ready(t)
	created := f.localRequest("POST", "/api/v1/orgs/a/identities", `{"name":"ci","token_ttl":900,"allowed_cidrs":["10.0.0.0/8"],"scopes":[{"project_id":"project-a","permission":"read"}]}`, "p5-id")
	checkStatus(t, created, 201)
	var body struct {
		ID           string   `json:"id"`
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		AllowedCIDRs []string `json:"allowed_cidrs"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &body); err != nil || body.ClientSecret == "" || body.AllowedCIDRs[0] != "10.0.0.0/8" {
		t.Fatal(created.Body.String())
	}
	denied := tokenFrom(f, body.ClientID, body.ClientSecret, "192.0.2.1:1234")
	checkStatus(t, denied, 403)
	if strings.Contains(denied.Body.String(), body.ClientSecret) {
		t.Fatal("token error leaked the secret")
	}
	allowed := tokenFrom(f, body.ClientID, body.ClientSecret, "10.2.3.4:9")
	checkStatus(t, allowed, 200)
	var tok struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(allowed.Body.Bytes(), &tok); err != nil || tok.Access == "" {
		t.Fatal(allowed.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/a/scan-reports", nil)
	req.RemoteAddr = "192.0.2.9:9"
	req.Header.Set("Authorization", "Bearer "+tok.Access)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	checkStatus(t, w, 403)

	human := f.localRequest("POST", "/api/v1/orgs/a/scan-reports", `{"source":"human","findings":[{"rule_id":"generic-api-key","level":"warning","file":"human.env","line":2,"match":"human-secret-value"}]}`, "")
	checkStatus(t, human, 201)
	humanID := reportID(t, human)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/orgs/a/scan-reports", strings.NewReader(`{"source":"machine","findings":[{"rule_id":"generic-api-key","level":"warning","file":"machine.env","line":4,"match":"machine-secret-value"}]}`))
	req.RemoteAddr = "10.2.3.4:9"
	req.Header.Set("Authorization", "Bearer "+tok.Access)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	checkStatus(t, w, 201)
	machineID := reportID(t, w)
	hidden := callAs(f, http.MethodGet, "/api/v1/orgs/a/scan-reports/"+humanID, "", tok.Access, "10.2.3.4:9")
	checkStatus(t, hidden, 404)
	list := callAs(f, http.MethodGet, "/api/v1/orgs/a/scan-reports", "", tok.Access, "10.2.3.4:9")
	checkStatus(t, list, 200)
	if strings.Contains(list.Body.String(), `"source":"human"`) || !strings.Contains(list.Body.String(), `"source":"machine"`) || strings.Contains(list.Body.String(), "machine-secret-value") {
		t.Fatalf("machine list: %s", list.Body.String())
	}
	checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/identities/"+body.ID, `{"list_scan_reports":true}`, "p5-grant"), 200)
	list = callAs(f, http.MethodGet, "/api/v1/orgs/a/scan-reports", "", tok.Access, "10.2.3.4:9")
	checkStatus(t, list, 200)
	if !strings.Contains(list.Body.String(), `"source":"human"`) {
		t.Fatalf("granted list: %s", list.Body.String())
	}
	checkStatus(t, callAs(f, http.MethodDelete, "/api/v1/orgs/a/scan-reports/"+machineID, "", tok.Access, "10.2.3.4:9"), 403)
	f.exec(`INSERT INTO users(id,email,name,password_hash) VALUES('dev2','dev2@example.test','Dev','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','dev2','developer')`)
	devTok, err := tc.SignJWT("dev2", "dev2@example.test", "fixture-jwt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	checkStatus(t, f.bearer("DELETE", "/api/v1/orgs/a/scan-reports/"+machineID, "", devTok), 403)
	gone := f.localRequest("DELETE", "/api/v1/orgs/a/scan-reports/"+machineID, "", "")
	checkStatus(t, gone, 200)
	var deleted int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='scan_report.delete'`).Scan(&deleted); err != nil || deleted != 1 {
		t.Fatalf("delete audit %d %v", deleted, err)
	}
}

func reportID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.ID == "" {
		t.Fatal(w.Body.String())
	}
	return body.ID
}

func callAs(f *federationFixture, method, path, body, token, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	return w
}

func tokenFrom(f *federationFixture, id, secret, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/identities/token", strings.NewReader(`{"client_id":"`+id+`","client_secret":"`+secret+`"}`))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	return w
}

func TestImportFolderRollsBackWithAudit(t *testing.T) {
	f := phase4Ready(t)
	testdb.FailAudit(f.t, f.db)
	checkStatus(f.t, f.localRequest("POST", "/api/v1/projects/project-a/import?path=/rolled/", `{"content":"NEW_FOLDER_KEY=value\n","strategy":"skip"}`, "p5-rollback"), 500)
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM folders WHERE path='/rolled/'`).Scan(&n); err != nil || n != 0 {
		f.t.Fatalf("folder survived rollback: %d %v", n, err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secrets WHERE key='NEW_FOLDER_KEY'`).Scan(&n); err != nil || n != 0 {
		f.t.Fatalf("secret survived rollback: %d %v", n, err)
	}
}
