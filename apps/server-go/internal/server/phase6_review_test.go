package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A change that lands while an approval is pending must not be overwritten
// by the approval, and terminal approvals must drop the sealed payload.
func TestPhase6ApprovalPinsVersionAndDropsPayload(t *testing.T) {
	f := phase4Ready(t)
	checkStatus(t, f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"RACE","value":"v1"}`, ""), 200)
	checkStatus(t, f.localRequest("PATCH", "/api/v1/projects/project-a", `{"approval_required":true}`, ""), 200)
	f.exec(`INSERT INTO users(id,email,name,password_hash) VALUES('admin2','admin2@example.test','Admin','!')`)
	f.exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('a','admin2','admin')`)
	admin := mustToken(t, "admin2", "admin2@example.test")

	queued := f.localRequest("POST", "/api/v1/projects/project-a/secrets", `{"key":"RACE","value":"from-request"}`, "")
	checkStatus(t, queued, 202)
	var a struct {
		ID string `json:"approval_id"`
	}
	if json.Unmarshal(queued.Body.Bytes(), &a) != nil || a.ID == "" {
		t.Fatal(queued.Body.String())
	}
	// A concurrent change approved first moves the secret to v2.
	other := f.bearer("POST", "/api/v1/projects/project-a/secrets", `{"key":"RACE","value":"concurrent"}`, admin)
	checkStatus(t, other, 202)
	var b struct {
		ID string `json:"approval_id"`
	}
	_ = json.Unmarshal(other.Body.Bytes(), &b)
	checkStatus(t, f.localRequest("POST", "/api/v1/orgs/a/approvals/"+b.ID+"/approve", `{}`, ""), 200)

	stale := f.bearer("POST", "/api/v1/orgs/a/approvals/"+a.ID+"/approve", `{}`, admin)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale approval applied: %d %s", stale.Code, stale.Body.String())
	}
	got := f.localRequest("GET", "/api/v1/projects/project-a/secrets/RACE", "", "")
	if !strings.Contains(got.Body.String(), `"value":"concurrent"`) {
		t.Fatalf("concurrent change lost: %s", got.Body.String())
	}
	var enc string
	if err := f.db.QueryRow(`SELECT payload_enc FROM approval_requests WHERE id=?`, b.ID).Scan(&enc); err != nil || enc != "" {
		t.Fatalf("applied approval kept its payload: %v", err)
	}
	checkStatus(t, f.localRequest("POST", "/api/v1/orgs/a/approvals/"+a.ID+"/cancel", `{}`, ""), 200)
	if err := f.db.QueryRow(`SELECT payload_enc FROM approval_requests WHERE id=?`, a.ID).Scan(&enc); err != nil || enc != "" {
		t.Fatalf("cancelled approval kept its payload: %v", err)
	}
}

func TestPhase6SCIMSyntheticEmailAndThrottle(t *testing.T) {
	f := phase4Ready(t)
	res := f.localRequest("POST", "/api/v1/orgs/a/scim/tokens", `{"name":"idp","ttl_seconds":3600}`, "")
	checkStatus(t, res, 201)
	var tok struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &tok)
	user := f.raw(http.MethodPost, "/api/v1/scim/v2/Users", `{"userName":"fresh@example.test","externalId":"fresh"}`, tok.Token)
	checkStatus(t, user, 201)
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email='fresh@example.test'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("SCIM claimed a global email: %d %v", n, err)
	}
	if !strings.Contains(user.Body.String(), "fresh@example.test") {
		t.Fatalf("userName not kept: %s", user.Body.String())
	}
	for i := 0; i < 20; i++ {
		checkStatus(t, f.raw(http.MethodGet, "/api/v1/scim/v2/Users", "", "taboo_scim_wrong"), 401)
	}
	checkStatus(t, f.raw(http.MethodGet, "/api/v1/scim/v2/Users", "", tok.Token), 429)
}

func TestPhase6DeviceStartHasNoCompleteURI(t *testing.T) {
	f := phase4Ready(t)
	started := f.raw(http.MethodPost, "/api/v1/auth/device", `{"client_id":"taboo-cli"}`, "")
	checkStatus(t, started, 200)
	if strings.Contains(started.Body.String(), "verification_uri_complete") {
		t.Fatal(started.Body.String())
	}
}
