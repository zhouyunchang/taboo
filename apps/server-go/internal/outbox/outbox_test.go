package outbox_test

import (
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

func TestDurableFanoutRollbackReplayAndLease(t *testing.T) {
	db := testdb.Open(t)
	for _, q := range []string{`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a','fixture')`, `INSERT INTO sync_targets(id,org_id,name,platform,config_enc) VALUES('target','a','target','github','fixture')`, `INSERT INTO webhooks(id,org_id,url,secret_enc) VALUES('hook','a','https://example.test','fixture')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	e := events.SecretEvent{EventID: "event", OrgID: "a", ProjectID: "project", Folder: "/a/", Key: "KEY", Version: 1, Action: "created", SecretID: "secret", Ciphertext: "encrypted-fixture"}
	if err := outbox.Insert(db, e); err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus()
	(&syncsvc.Service{DB: db}).Subscribe(bus)
	(&webhooks.Service{DB: db}).Subscribe(bus)
	broken := true
	bus.Subscribe(func(*sql.Tx, events.SecretEvent) error {
		if broken {
			return errors.New("consumer unavailable")
		}
		return nil
	})
	s := &outbox.Service{DB: db, Bus: bus}
	s.Drain()
	assertCount := func(table, where string, want int) {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE ` + where).Scan(&n); err != nil || n != want {
			t.Fatalf("%s count=%d want=%d: %v", table, n, want, err)
		}
	}
	assertCount("outbox_events", "status='pending'", 1)
	assertCount("sync_runs", "1=1", 0)
	assertCount("webhook_deliveries", "1=1", 0)
	broken = false
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.Drain() }()
	}
	wg.Wait()
	assertCount("outbox_events", "status='done'", 1)
	assertCount("sync_runs", "1=1", 1)
	assertCount("webhook_deliveries", "1=1", 1)
	// Simulate replay after completion: no duplicate delivery or sync job.
	if _, err := db.Exec(`UPDATE outbox_events SET status='pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE webhook_deliveries SET status='success'`); err != nil {
		t.Fatal(err)
	}
	s.Drain()
	assertCount("webhook_deliveries", "1=1", 1)
	assertCount("sync_runs", "1=1", 1)
	var id string
	if err := db.QueryRow(`SELECT id FROM sync_runs`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := outbox.Claim(db, "sync_runs", id)
			if err != nil {
				t.Error(err)
			}
			if token != "" {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("concurrent owners=%d", claims.Load())
	}
	if _, err := db.Exec(`UPDATE sync_runs SET lease_until=0`); err != nil {
		t.Fatal(err)
	}
	if token, err := outbox.Claim(db, "sync_runs", id); err != nil || token == "" {
		t.Fatalf("expired lease not recovered: %v", err)
	}
}
