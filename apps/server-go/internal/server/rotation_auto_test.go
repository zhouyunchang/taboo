package server

import (
	"context"
	"strings"
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

func TestRotationAutoGenerateWorker(t *testing.T) {
	f := phase4Ready(t)
	base := "/api/v1/projects/project-a/secrets"
	checkStatus(t, f.localRequest("POST", base, `{"key":"AUTO","value":"old-auto","expectedVersion":0}`, "auto-create"), 200)
	checkStatus(t, f.localRequest("PUT", base+"/AUTO/rotation", `{"status":"active","mode":"generate","interval_seconds":3600,"auto_generate":true,"generation":{"profile":"alnum32","length":32}}`, "auto-policy"), 200)
	f.exec(`UPDATE rotation_policies SET next_run_at=1 WHERE secret_id=(SELECT id FROM secrets WHERE key='AUTO')`)

	secrets := &secretsvc.Service{DB: f.db, MasterKey: f.master, DEKs: tc.NewDEKCache()}
	(&rotation.Service{DB: f.db, Secrets: secrets}).Sweep(context.Background())
	var status string
	var newVersion int
	if err := f.db.QueryRow(`SELECT status, new_version FROM rotation_runs WHERE secret_key='AUTO' ORDER BY created_at DESC LIMIT 1`).Scan(&status, &newVersion); err != nil {
		t.Fatal(err)
	}
	if status != "awaiting_distribution" || newVersion != 2 {
		t.Fatalf("auto run status=%s version=%d", status, newVersion)
	}
	revealed := f.localRequest("GET", base+"/AUTO", "", "")
	checkStatus(t, revealed, 200)
	if strings.Contains(revealed.Body.String(), "old-auto") {
		t.Fatalf("auto generate did not replace value: %s", revealed.Body.String())
	}
}
