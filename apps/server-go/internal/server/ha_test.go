package server

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/cluster"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// Two connection pools are two replicas. They share PostgreSQL and nothing else.
func TestPostgresTwoReplicas(t *testing.T) {
	dsn, cleanup := startPostgres(t)
	defer cleanup()
	a, err := store.OpenPostgres(context.Background(), dsn, "taboo_runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := store.OpenPostgres(context.Background(), dsn, "taboo_runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if _, err = a.Exec(`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('o','o','o','x')`); err != nil {
		t.Fatal(err)
	}
	ok, err := cluster.Acquire(a, "replica-sweep", time.Minute)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	// The lease is per process, so a second pool in this process refreshes it.
	// A foreign holder must keep the other pool out.
	if _, err = b.Exec(`UPDATE cluster_leases SET holder='other-replica', lease_until=? WHERE name='replica-sweep'`, time.Now().Unix()+60); err != nil {
		t.Fatal(err)
	}
	ok, err = cluster.Acquire(a, "replica-sweep", time.Minute)
	if err != nil || ok {
		t.Fatalf("second replica took a live lease: %v %v", ok, err)
	}

	allowed, err := hit(a, "login:/api/v1/auth/login:203.0.113.5", time.Minute, 2)
	if err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	allowed, err = hit(b, "login:/api/v1/auth/login:203.0.113.5", time.Minute, 2)
	if err != nil || !allowed {
		t.Fatal(allowed, err)
	}
	allowed, err = hit(a, "login:/api/v1/auth/login:203.0.113.5", time.Minute, 2)
	if err != nil || allowed {
		t.Fatalf("rate limit did not cross replicas: %v %v", allowed, err)
	}

	busA, busB := events.NewBus(), events.NewBus()
	for _, bus := range []*events.Bus{busA, busB} {
		bus.Subscribe(func(tx *sql.Tx, e events.SecretEvent) error {
			_, err := tx.Exec(`INSERT INTO audit_logs(id,org_id,actor_id,action,resource) VALUES(?,?,?,?,?)`, tc.NewID(), e.OrgID, "system", "replica.fanout", e.EventID)
			return err
		})
	}
	if err = outbox.Insert(a, events.SecretEvent{OrgID: "o", EventID: tc.NewID(), Action: "created", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); (&outbox.Service{DB: a, Bus: busA}).Drain() }()
	go func() { defer wg.Done(); (&outbox.Service{DB: b, Bus: busB}).Drain() }()
	wg.Wait()
	var fans, done int
	if err = a.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='replica.fanout'`).Scan(&fans); err != nil || fans != 1 {
		t.Fatalf("fanout=%d %v", fans, err)
	}
	if err = b.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE status='done'`).Scan(&done); err != nil || done != 1 {
		t.Fatalf("done=%d %v", done, err)
	}
}

func hit(db *sql.DB, bucket string, window time.Duration, limit int) (bool, error) {
	now := time.Now().Unix()
	var n int
	err := db.QueryRow(`INSERT INTO rate_counters(bucket, window_start, hits) VALUES(?,?,1)
		ON CONFLICT(bucket, window_start) DO UPDATE SET hits=rate_counters.hits+1 RETURNING hits`, bucket, now/60*60).Scan(&n)
	return n <= limit, err
}

func startPostgres(t *testing.T) (string, func()) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required for the multi-replica test")
	}
	name := fmt.Sprintf("taboo-pg-ha-%d", time.Now().UnixNano())
	image := "postgres@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
	out, err := exec.Command("docker", "run", "-d", "--name", name,
		"-e", "POSTGRES_PASSWORD=fixture-admin", "-e", "POSTGRES_DB=taboo",
		"-p", "127.0.0.1::5432", image).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v %s", err, out)
	}
	cleanup := func() { _ = exec.Command("docker", "rm", "-f", name).Run() }
	deadline := time.Now().Add(90 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if exec.Command("docker", "exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "taboo").Run() == nil {
			ready = true
			break
		}
		time.Sleep(time.Second)
	}
	if !ready {
		logs, _ := exec.Command("docker", "logs", "--tail", "40", name).CombinedOutput()
		cleanup()
		t.Fatalf("postgres did not become ready\n%s", logs)
	}
	setup := `CREATE ROLE taboo_runtime NOLOGIN NOSUPERUSER NOBYPASSRLS; CREATE ROLE taboo_control LOGIN PASSWORD 'fixture-control' NOSUPERUSER NOBYPASSRLS; GRANT taboo_runtime TO taboo_control; ALTER DATABASE taboo OWNER TO taboo_control; GRANT CREATE ON SCHEMA public TO taboo_control;`
	if out, err = exec.Command("docker", "exec", name, "psql", "-U", "postgres", "-d", "taboo", "-v", "ON_ERROR_STOP=1", "-c", setup).CombinedOutput(); err != nil {
		cleanup()
		t.Fatalf("roles: %v %s", err, out)
	}
	portOut, err := exec.Command("docker", "port", name, "5432").CombinedOutput()
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	port := strings.TrimSpace(string(portOut))
	if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i+1:]
	}
	return fmt.Sprintf("postgres://taboo_control:fixture-control@127.0.0.1:%s/taboo?sslmode=disable", port), cleanup
}
