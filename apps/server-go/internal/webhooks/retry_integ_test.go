package webhooks

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func TestDeliver5xxKeepsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	key := make([]byte, 32)
	s := &Service{DB: db, MasterKey: key}
	org, wid, did := tc.NewID(), tc.NewID(), tc.NewID()
	enc, _ := tc.Encrypt(key, "whsec")
	_, _ = db.Exec(`INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, 'o', 'o', 'x')`, org)
	_, _ = db.Exec(`INSERT INTO webhooks (id, org_id, url, secret_enc, events, active) VALUES (?, ?, ?, ?, '[]', 1)`,
		wid, org, srv.URL, enc)
	_, _ = db.Exec(`INSERT INTO webhook_deliveries (id, webhook_id, org_id, event, payload, status, next_attempt_at)
		VALUES (?, ?, ?, 'secret.created', '{}', 'pending', 0)`, did, wid, org)

	s.deliver(did, org, "secret.created", `{"ok":true}`, 0, srv.URL, enc)
	var status string
	var attempts int
	if err := db.QueryRow(`SELECT status, attempts FROM webhook_deliveries WHERE id = ?`, did).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("want pending after 5xx, got %s attempts=%d", status, attempts)
	}
	_ = time.Second
}
