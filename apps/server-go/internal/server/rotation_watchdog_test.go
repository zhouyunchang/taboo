package server

import (
	"context"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
)

func TestRotationWatchdogMarksOverdue(t *testing.T) {
	f := phase4Ready(t)
	base := "/api/v1/projects/project-a/secrets"
	checkStatus(t, f.localRequest("POST", base, `{"key":"LATE","value":"late","expectedVersion":0}`, "late-create"), 200)
	checkStatus(t, f.localRequest("PUT", base+"/LATE/rotation", `{"status":"active","mode":"manual","interval_seconds":3600,"grace_seconds":3600}`, "late-policy"), 200)
	f.exec(`UPDATE rotation_policies SET next_run_at=1 WHERE secret_id=(SELECT id FROM secrets WHERE key='LATE')`)
	(&rotation.Service{DB: f.db}).Sweep(context.Background())
	f.exec(`UPDATE rotation_runs SET grace_until=1 WHERE secret_key='LATE'`)
	(&rotation.Service{DB: f.db}).Watchdog(context.Background())
	var status string
	if err := f.db.QueryRow(`SELECT status FROM rotation_runs WHERE secret_key='LATE' ORDER BY created_at DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "overdue" {
		t.Fatalf("status=%s", status)
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='rotation.run.overdue'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("overdue audit %d %v", n, err)
	}
}
