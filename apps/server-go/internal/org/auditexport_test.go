package org

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func exportFixture(t *testing.T) *Service {
	db := testdb.Open(t)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{`INSERT INTO users(id,email,password_hash) VALUES('u','u@example.test','!')`, `INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a','fixture')`, `INSERT INTO org_members(org_id,user_id,role) VALUES('a','u','owner')`, `INSERT INTO audit_exports(id,org_id,actor_id,format,filters) VALUES('job','a','u','jsonl','{}')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := audit.Record(db, audit.Rec{OrgID: "a", ActorID: "u", ActorType: "user", Action: "fixture", Resource: "metadata"}); err != nil {
		t.Fatal(err)
	}
	return &Service{DB: db, DataDir: t.TempDir()}
}
func TestExportConcurrentClaimAndCrashRecovery(t *testing.T) {
	s := exportFixture(t)
	if _, err := s.DB.Exec(`UPDATE audit_exports SET lease_token='crashed',lease_until=? WHERE id='job'`, time.Now().Unix()+60); err != nil {
		t.Fatal(err)
	}
	s.runExportJobs()
	var state, path string
	s.DB.QueryRow(`SELECT status,file_path FROM audit_exports WHERE id='job'`).Scan(&state, &path)
	if state != "pending" || path != "" {
		t.Fatal("live claim was stolen")
	}
	s.DB.Exec(`UPDATE audit_exports SET lease_until=0 WHERE id='job'`)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.runExportJobs() }()
	}
	wg.Wait()
	s.DB.QueryRow(`SELECT status,file_path FROM audit_exports WHERE id='job'`).Scan(&state, &path)
	if state != "done" {
		t.Fatal("crashed export not recovered", state)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatal("completed export missing", err)
	}
	var count int
	s.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='audit.export'`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate completion audit", count)
	}
	files, _ := filepath.Glob(filepath.Join(s.DataDir, "exports", "a", "*"))
	if len(files) != 1 {
		t.Fatal("concurrent worker left extra files", len(files))
	}
}
func TestExportAuditFailureDoesNotPublishFile(t *testing.T) {
	s := exportFixture(t)
	testdb.FailAudit(t, s.DB)
	s.runExportJobs()
	var state, path string
	s.DB.QueryRow(`SELECT status,file_path FROM audit_exports WHERE id='job'`).Scan(&state, &path)
	if state != "pending" || path != "" {
		t.Fatal("unaudited file published")
	}
	files, _ := filepath.Glob(filepath.Join(s.DataDir, "exports", "a", "*"))
	if len(files) != 0 {
		t.Fatal("failed worker file leaked")
	}
}
func TestExportExpiredWorkerCannotPublish(t *testing.T) {
	s := exportFixture(t)
	// Simulate loss of ownership between reading rows and committing completion.
	// writeExport cancellation is also checked before any success file is recorded.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f, err := os.CreateTemp(t.TempDir(), "cancelled")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = s.writeExport(ctx, f, "a", "jsonl", auditFilter{}, 100); err == nil {
		t.Fatal("cancelled export reported success")
	}
}
