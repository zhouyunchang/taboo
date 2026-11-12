package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
)

func TestRotationRedesignPolicyFixedRunGenerate(t *testing.T) {
	f := phase4Ready(t)
	base := "/api/v1/projects/project-a/secrets"
	checkStatus(t, f.localRequest("POST", base, `{"key":"ROT","value":"old-rot","expectedVersion":0}`, "rot-create"), 200)

	// fixed is explicit, has no schedule, and cannot be run.
	fixed := f.localRequest("PUT", base+"/ROT/rotation", `{"status":"fixed","fixed_reason":"vendor managed"}`, "rot-fixed")
	checkStatus(t, fixed, 200)
	if !strings.Contains(fixed.Body.String(), `"status":"fixed"`) || !strings.Contains(fixed.Body.String(), `"next_run_at":0`) {
		t.Fatalf("fixed policy: %s", fixed.Body.String())
	}
	checkStatus(t, f.localRequest("POST", base+"/ROT/rotation/run", "", "rot-fixed-run"), 409)

	// active manual policy: worker creates a run, then a new version completes it.
	checkStatus(t, f.localRequest("PUT", base+"/ROT/rotation", `{"status":"active","mode":"manual","interval_seconds":3600,"grace_seconds":3600}`, "rot-active"), 200)
	f.exec(`UPDATE rotation_policies SET next_run_at=1 WHERE secret_id=(SELECT id FROM secrets WHERE key='ROT')`)
	(&rotation.Service{DB: f.db}).Sweep(context.Background())
	var runID, runStatus string
	if err := f.db.QueryRow(`SELECT id, status FROM rotation_runs WHERE secret_key='ROT' ORDER BY created_at DESC LIMIT 1`).Scan(&runID, &runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "awaiting_manual" {
		t.Fatalf("run status=%s", runStatus)
	}
	checkStatus(t, f.localRequest("POST", base, `{"key":"ROT","value":"new-rot","expectedVersion":1,"reason":"manual rotation"}`, "rot-new"), 200)
	completed := f.localRequest("POST", base+"/ROT/rotation/runs/"+runID+"/complete", `{"new_version":2,"note":"verified","verified":true}`, "rot-complete")
	checkStatus(t, completed, 200)
	if !strings.Contains(completed.Body.String(), `"status":"succeeded"`) || !strings.Contains(completed.Body.String(), `"new_version":2`) {
		t.Fatalf("complete: %s", completed.Body.String())
	}

	// fixed manual update requires a reason and records it.
	checkStatus(t, f.localRequest("PUT", base+"/ROT/rotation", `{"status":"fixed","fixed_reason":"vendor managed"}`, "rot-fixed-2"), 200)
	checkStatus(t, f.localRequest("POST", base, `{"key":"ROT","value":"no-reason","expectedVersion":2}`, "rot-no-reason"), 400)
	checkStatus(t, f.localRequest("POST", base, `{"key":"ROT","value":"with-reason","expectedVersion":2,"reason":"DBA changed upstream"}`, "rot-reason"), 200)
	var meta string
	if err := f.db.QueryRow(`SELECT metadata FROM audit_logs WHERE action='secrets.update' AND resource LIKE '%ROT' ORDER BY rowid DESC LIMIT 1`).Scan(&meta); err != nil || !strings.Contains(meta, "DBA changed upstream") {
		t.Fatalf("fixed reason audit: %s %v", meta, err)
	}

	// generate: new random value, distribution confirmation, no plaintext audit.
	checkStatus(t, f.localRequest("POST", base, `{"key":"GEN","value":"old-gen","expectedVersion":0}`, "gen-create"), 200)
	genPolicy := f.localRequest("PUT", base+"/GEN/rotation", `{"status":"active","mode":"generate","interval_seconds":3600,"generation":{"profile":"alnum32","length":32}}`, "gen-policy")
	checkStatus(t, genPolicy, 200)
	if !strings.Contains(genPolicy.Body.String(), `"profile":"alnum32"`) || !strings.Contains(genPolicy.Body.String(), `"min_entropy_bits":187`) {
		t.Fatalf("generation policy: %s", genPolicy.Body.String())
	}
	f.exec(`UPDATE rotation_policies SET next_run_at=1 WHERE secret_id=(SELECT id FROM secrets WHERE key='GEN')`)
	(&rotation.Service{DB: f.db}).Sweep(context.Background())
	if err := f.db.QueryRow(`SELECT id FROM rotation_runs WHERE secret_key='GEN' AND status='awaiting_generation' ORDER BY created_at DESC LIMIT 1`).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	generated := f.localRequest("POST", base+"/GEN/rotation/runs/"+runID+"/generate", "", "gen-run")
	checkStatus(t, generated, 200)
	var genBody struct {
		Value   string `json:"value"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(generated.Body.Bytes(), &genBody); err != nil {
		t.Fatal(err)
	}
	if len(genBody.Value) != 32 || genBody.Version != 2 {
		t.Fatalf("generated value shape: %q v%d", genBody.Value, genBody.Version)
	}
	distributed := f.localRequest("POST", base+"/GEN/rotation/runs/"+runID+"/distribute", `{"note":"apps reloaded","verified":true}`, "gen-dist")
	checkStatus(t, distributed, 200)
	if !strings.Contains(distributed.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("distribute: %s", distributed.Body.String())
	}
	revealed := f.localRequest("GET", base+"/GEN", "", "")
	checkStatus(t, revealed, 200)
	if !strings.Contains(revealed.Body.String(), genBody.Value) {
		t.Fatalf("reveal generated value: %s", revealed.Body.String())
	}
	var audit string
	if err := f.db.QueryRow(`SELECT metadata FROM audit_logs WHERE action='rotation.run.generated' ORDER BY rowid DESC LIMIT 1`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, genBody.Value) {
		t.Fatalf("audit leaked generated value: %s", audit)
	}

	list := f.localRequest("GET", "/api/v1/projects/project-a/rotations", "", "")
	checkStatus(t, list, 200)
	if !strings.Contains(list.Body.String(), `"status":"fixed"`) || !strings.Contains(list.Body.String(), `"status":"active"`) || !strings.Contains(list.Body.String(), `"unset":`) {
		t.Fatalf("rotation list: %s", list.Body.String())
	}
	health := f.localRequest("GET", "/api/v1/orgs/a/rotation/health", "", "")
	checkStatus(t, health, 200)
	if !strings.Contains(health.Body.String(), `"online":false`) || !strings.Contains(health.Body.String(), `"status":"offline"`) {
		t.Fatalf("health: %s", health.Body.String())
	}
}
