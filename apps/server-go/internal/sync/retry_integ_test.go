package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

type failAdapter struct{}

func (failAdapter) Push(ctx context.Context, cfg map[string]string, key, value string) error {
	return errors.New("HTTP 503")
}
func (failAdapter) Ping(ctx context.Context, cfg map[string]string) error { return errors.New("503") }

func TestFirstFailKeepsPendingAndRetries(t *testing.T) {
	old := Adapters["github"]
	Adapters["github"] = failAdapter{}
	defer func() { Adapters["github"] = old }()

	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{DB: db, MasterKey: make([]byte, 32), DEKs: tc.NewDEKCache()}
	org, tid, run := tc.NewID(), tc.NewID(), tc.NewID()
	_, _ = db.Exec(`INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, 'o', 'o', 'x')`, org)
	enc, _ := tc.Encrypt(s.MasterKey, `{"token":"t"}`)
	if _, err := db.Exec(`INSERT INTO sync_targets (id, org_id, name, platform, env_slug, config_enc, enabled)
		VALUES (?, ?, 't', 'github', '*', ?, 1)`, tid, org, enc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_runs (id, target_id, org_id, secret_key, action, status, next_attempt_at)
		VALUES (?, ?, ?, 'K', 'updated', 'pending', 0)`, run, tid, org); err != nil {
		t.Fatal(err)
	}

	s.execute(run, org, "github", enc, "", "*", "K", 0)
	var status string
	var attempts int
	var next int64
	if err := db.QueryRow(`SELECT status, attempts, next_attempt_at FROM sync_runs WHERE id = ?`, run).
		Scan(&status, &attempts, &next); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 || next <= time.Now().Unix() {
		t.Fatalf("want pending+backoff, got status=%s attempts=%d next=%d", status, attempts, next)
	}
}
