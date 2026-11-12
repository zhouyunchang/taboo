package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
)

func TestRotationWorkerHeartbeat(t *testing.T) {
	f := phase4Ready(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	(&rotation.Service{DB: f.db, WorkerID: "test-worker", Role: "rotation"}).StartWorker(ctx, 10*time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var n int
		_ = f.db.QueryRow(`SELECT COUNT(*) FROM worker_heartbeats WHERE worker_id='test-worker'`).Scan(&n)
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker heartbeat was not written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	health := f.localRequest("GET", "/api/v1/orgs/a/rotation/health", "", "")
	checkStatus(t, health, 200)
	if !strings.Contains(health.Body.String(), `"online":true`) || !strings.Contains(health.Body.String(), `"status":"online"`) {
		t.Fatalf("health: %s", health.Body.String())
	}
}
