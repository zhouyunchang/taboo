package sync

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

type orderedAdapter struct {
	mu     sync.Mutex
	values []string
	fail   bool
}

func (a *orderedAdapter) Ping(context.Context, map[string]string) error { return nil }
func (a *orderedAdapter) Push(_ context.Context, _ map[string]string, _ string, value string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.values = append(a.values, value)
	if a.fail {
		return fmt.Errorf("downstream returned sensitive content: %s", value)
	}
	return nil
}
func orderedFixture(t *testing.T) (*Service, string, []string, *orderedAdapter) {
	db := testdb.Open(t)
	t.Cleanup(func() { db.Close() })
	s := &Service{DB: db, MasterKey: make([]byte, 32), DEKs: tc.NewDEKCache()}
	dek := make([]byte, 32)
	wrapped, _ := tc.Seal(s.MasterKey, hex.EncodeToString(dek), tc.DEKContext("a"))
	db.Exec(`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a',?)`, wrapped)
	db.Exec(`INSERT INTO projects(id,org_id,name,slug) VALUES('p','a','p','p')`)
	db.Exec(`INSERT INTO environments(id,project_id,name,slug) VALUES('e','p','dev','dev')`)
	enc, _ := tc.Seal(s.MasterKey, `{"token":"fixture"}`, tc.Context{Purpose: "sync-target", Org: "a", Object: "target", Version: 1})
	db.Exec(`INSERT INTO sync_targets(id,org_id,name,platform,config_enc) VALUES('target','a','target','github',?)`, enc)
	ids := []string{}
	for version := 1; version <= 2; version++ {
		cipher, _ := tc.Seal(dek, fmt.Sprint("fixture-v", version), tc.SecretContext("a", "p", "e", "secret", version))
		e := events.SecretEvent{Sequence: int64(version), EventID: fmt.Sprint(version), OrgID: "a", ProjectID: "p", EnvironmentID: "e", EnvSlug: "dev", SecretID: "secret", Folder: "/", Key: "K", Version: version, Ciphertext: cipher, Action: "updated"}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err = enqueue(tx, "target", e); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var id string
		db.QueryRow(`SELECT id FROM sync_runs WHERE event_id=?`, e.EventID).Scan(&id)
		ids = append(ids, id)
	}
	adapter := &orderedAdapter{}
	old := Adapters["github"]
	Adapters["github"] = adapter
	t.Cleanup(func() { Adapters["github"] = old })
	return s, enc, ids, adapter
}
func TestOrderedSyncRetryCannotOverwriteNewerValue(t *testing.T) {
	s, enc, ids, a := orderedFixture(t)
	execute := func(i int) { s.execute(ids[i], "a", "github", enc, "p", "dev", "K", 0) }
	execute(1)
	if len(a.values) != 0 {
		t.Fatal("later version skipped earlier pending event")
	}
	a.fail = true
	execute(0)
	execute(1)
	if len(a.values) != 1 {
		t.Fatal("later version skipped backoff")
	}
	var log string
	s.DB.QueryRow(`SELECT error FROM sync_runs WHERE id=?`, ids[0]).Scan(&log)
	if log != "sync attempt failed" {
		t.Fatal("downstream response leaked into stored error")
	}
	a.fail = false
	s.DB.Exec(`UPDATE sync_runs SET next_attempt_at=0 WHERE id=?`, ids[0])
	execute(0)
	execute(1)
	if fmt.Sprint(a.values) != "[fixture-v1 fixture-v1 fixture-v2]" {
		t.Fatal("wrong delivery order", a.values)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM sync_runs WHERE status='success'`).Scan(&n)
	if n != 2 {
		t.Fatal("missing success")
	}
}
func TestOrderedSyncWaitsForEarlierOutboxAndConcurrentClaim(t *testing.T) {
	s, _, ids, _ := orderedFixture(t)
	s.DB.Exec(`DELETE FROM sync_runs WHERE id=?`, ids[0])
	s.DB.Exec(`INSERT INTO outbox_events(id,org_id,event_id,kind,payload,status,event_sequence) VALUES('earlier','a','earlier','secret','{}','pending',1)`)
	if token, err := s.claimOrdered(ids[1]); err != nil || token != "" {
		t.Fatal("unmaterialized earlier event bypassed", err)
	}
	s.DB.Exec(`UPDATE outbox_events SET status='done' WHERE id='earlier'`)
	var wg sync.WaitGroup
	tokens := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := s.claimOrdered(ids[1])
			if err != nil {
				t.Error(err)
			}
			if token != "" {
				tokens <- token
			}
		}()
	}
	wg.Wait()
	close(tokens)
	if len(tokens) != 1 {
		t.Fatal("multiple ordered stream owners", len(tokens))
	}
}
func TestOrderedSyncAuditFailureRetainsOldJob(t *testing.T) {
	s, enc, ids, a := orderedFixture(t)
	testdb.FailAudit(t, s.DB)
	s.execute(ids[0], "a", "github", enc, "p", "dev", "K", 0)
	var status string
	s.DB.QueryRow(`SELECT status FROM sync_runs WHERE id=?`, ids[0]).Scan(&status)
	if status != "pending" {
		t.Fatal("unaudited success")
	}
	s.execute(ids[1], "a", "github", enc, "p", "dev", "K", 0)
	if len(a.values) != 1 {
		t.Fatal("later version bypassed unconfirmed completion")
	}
}
