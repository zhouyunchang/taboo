package dynamic

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

type recoveryEngine struct {
	mu      sync.Mutex
	revokes int
	failure bool
	barrier chan struct{}
	release chan struct{}
}

func (e *recoveryEngine) Type() string                                               { return "fixture" }
func (e *recoveryEngine) Grant(context.Context, string, string, time.Duration) error { return nil }
func (e *recoveryEngine) Revoke(ctx context.Context, _ string) error {
	e.mu.Lock()
	e.revokes++
	failure := e.failure
	e.mu.Unlock()
	if e.barrier != nil {
		e.barrier <- struct{}{}
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if failure {
		return errors.New("fixture failure")
	}
	return nil
}
func dynamicFixture(t *testing.T) (*Service, *recoveryEngine) {
	db := testdb.Open(t)
	t.Cleanup(func() { db.Close() })
	s := New(db, make([]byte, 32))
	engine := &recoveryEngine{}
	enc, _ := tc.Seal(s.MasterKey, "public-fixture", tc.Context{Purpose: "dynamic-engine", Org: "a", Project: "p", Object: "e", Version: 1})
	for _, q := range []string{`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a','fixture')`, `INSERT INTO projects(id,org_id,name,slug) VALUES('p','a','p','p')`, `INSERT INTO machine_identities(id,org_id,name,client_id,secret_hash) VALUES('m','a','m','m','!')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO dynamic_engines(id,org_id,project_id,name,conn_encrypted) VALUES('e','a','p','fixture',?)`, enc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO dynamic_leases(id,engine_id,identity_id,username,password_encrypted,status,expires_at,lease_token,lease_until) VALUES('l','e','m','fixture_user','fixture','revoking',?,'crashed',?)`, time.Now().Unix()+100, time.Now().Unix()+60); err != nil {
		t.Fatal(err)
	}
	s.cache.engines[tc.SHA256("a\x00p\x00e\x00"+enc)] = engine
	return s, engine
}
func TestDynamicCrashRecoveryAndSingleWorker(t *testing.T) {
	s, e := dynamicFixture(t)
	s.sweep(context.Background())
	if e.revokes != 0 {
		t.Fatal("live claim stolen")
	}
	s.DB.Exec(`UPDATE dynamic_leases SET lease_until=0`)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.sweep(context.Background()) }()
	}
	wg.Wait()
	var state string
	s.DB.QueryRow(`SELECT status FROM dynamic_leases WHERE id='l'`).Scan(&state)
	if state != "revoked" || e.revokes != 1 {
		t.Fatal("crash recovery failed", state, e.revokes)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='dynamic.lease.revoke'`).Scan(&n)
	if n != 1 {
		t.Fatal("missing or duplicate completion audit", n)
	}
}
func TestDynamicReclaimAuditFailureAndDeadLetter(t *testing.T) {
	t.Run("audit", func(t *testing.T) {
		s, e := dynamicFixture(t)
		s.DB.Exec(`UPDATE dynamic_leases SET lease_until=0`)
		testdb.FailAudit(t, s.DB)
		s.sweep(context.Background())
		var state string
		s.DB.QueryRow(`SELECT status FROM dynamic_leases WHERE id='l'`).Scan(&state)
		if state != "revoking" || e.revokes != 1 {
			t.Fatal("unaudited completion accepted", state, e.revokes)
		}
	})
	t.Run("dead_letter", func(t *testing.T) {
		s, e := dynamicFixture(t)
		e.failure = true
		for i := 0; i < 5; i++ {
			s.DB.Exec(`UPDATE dynamic_leases SET lease_until=0`)
			s.sweep(context.Background())
		}
		var state string
		s.DB.QueryRow(`SELECT status FROM dynamic_leases WHERE id='l'`).Scan(&state)
		if state != "failed" {
			t.Fatal("failed reclaim not surfaced", state)
		}
		s.sweep(context.Background())
		if e.revokes != 5 {
			t.Fatal("dead letter retried without operator")
		}
	})
}
func TestDynamicLateWorkerFenced(t *testing.T) {
	s, e := dynamicFixture(t)
	s.DB.Exec(`UPDATE dynamic_leases SET lease_until=0`)
	e.barrier = make(chan struct{}, 1)
	e.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.revokeNow(context.Background(), "e", "l", "fixture_user") }()
	<-e.barrier
	s.DB.Exec(`UPDATE dynamic_leases SET lease_token='successor',lease_until=? WHERE id='l'`, time.Now().Unix()+60)
	close(e.release)
	if err := <-done; err == nil {
		t.Fatal("late worker accepted")
	}
	var state, claim string
	s.DB.QueryRow(`SELECT status,lease_token FROM dynamic_leases WHERE id='l'`).Scan(&state, &claim)
	if state != "revoking" || claim != "successor" {
		t.Fatal("late worker overwrote successor")
	}
}
func TestDynamicActivationAuditFailureKeepsRecovery(t *testing.T) {
	s, _ := dynamicFixture(t)
	testdb.FailAudit(t, s.DB)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/fixture", nil)
	if s.activateLease(w, r, "a", &auth.Actor{ID: "m", Kind: auth.KindIdentity}, "l", "crashed", time.Now().Unix()+100, "dynamic.lease.create") {
		t.Fatal("unaudited activation")
	}
	var state string
	s.DB.QueryRow(`SELECT status FROM dynamic_leases WHERE id='l'`).Scan(&state)
	if state != "revoking" {
		t.Fatal("lost recovery row")
	}
}
func TestDynamicRenewedLeaseNotReclaimedFromStaleScan(t *testing.T) {
	s, _ := dynamicFixture(t)
	s.DB.Exec(`UPDATE dynamic_leases SET status='active',lease_until=0,lease_token=''`)
	if s.revokeNow(context.Background(), "e", "l", "fixture_user") == nil {
		t.Fatal("unexpired renewed lease reclaimed")
	}
}

func TestDynamicRealPostgresGrantAndReclaim(t *testing.T) {
	dsn := os.Getenv("TABOO_TEST_DYNAMIC_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("TABOO_REQUIRE_POSTGRES") == "1" {
			t.Fatal("dynamic PostgreSQL fixture DSN required")
		}
		t.Skip("run scripts/test-postgres.sh")
	}
	engine, err := PostgresFactory(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.(*Postgres).Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user := "taboo_test_" + strings.ReplaceAll(tc.NewID(), "-", "")[:12]
	ctx := context.Background()
	t.Cleanup(func() { _ = engine.Revoke(ctx, user) })
	for i := 0; i < 2; i++ {
		if err = engine.Grant(ctx, user, "public-fixture-password", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	var valid bool
	if err = db.QueryRow(`SELECT rolvaliduntil>now() FROM pg_roles WHERE rolname=$1`, user).Scan(&valid); err != nil || !valid {
		t.Fatal("actual role missing or expired", err)
	}
	if err = engine.Revoke(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err = engine.Revoke(ctx, user); err != nil {
		t.Fatal("reclamation not idempotent", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM pg_roles WHERE rolname=$1`, user).Scan(&n)
	if n != 0 {
		t.Fatal("actual role survived revoke")
	}
}
