package server

import (
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func (f *federationFixture) localRequest(method, path, body, idempotency string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	token, err := tc.SignJWT("local", "collision@example.test", "fixture-jwt", time.Minute)
	if err != nil {
		f.t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	if idempotency != "" {
		r.Header.Set("Idempotency-Key", idempotency)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func TestSecretEnvelopeMutationAndAuditAtomicity(t *testing.T) {
	f := fixture(t, "")
	dek, _ := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
	f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
	path := "/api/v1/projects/project-a/secrets?env=dev"
	one := `{"key":"K","value":"one","expectedVersion":0}`
	checkStatus(t, f.localRequest("POST", path, one, "write-1"), 200)
	w := f.localRequest("POST", path, one, "write-1")
	checkStatus(t, w, 200)
	if w.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("receipt not replayed")
	}
	checkStatus(t, f.localRequest("POST", path, `{"key":"K","value":"different","expectedVersion":0}`, "write-1"), 409)
	checkStatus(t, f.localRequest("POST", path, one, "stale-write"), 409)
	checkStatus(t, f.localRequest("POST", path, `{"key":"K","value":"two","expectedVersion":1}`, "write-2"), 200)
	rollback := "/api/v1/projects/project-a/secrets/K/rollback?env=dev"
	checkStatus(t, f.localRequest("POST", rollback, `{"version":1,"expectedVersion":2}`, "rollback"), 200)
	checkStatus(t, f.localRequest("POST", rollback, `{"version":1,"expectedVersion":2}`, "rollback"), 200)
	w = f.localRequest("GET", "/api/v1/projects/project-a/secrets/K?env=dev", "", "")
	checkStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"value":"one"`) {
		t.Fatal("rollback did not re-encrypt original plaintext")
	}
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secret_versions`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("duplicate mutation: %d %v", n, err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("duplicate outbox: %d %v", n, err)
	}
	// Context-bound versions cannot be substituted, even for the same object.
	f.exec(`UPDATE secret_versions SET ciphertext=(SELECT ciphertext FROM secret_versions WHERE version=1) WHERE version=3`)
	checkStatus(t, f.localRequest("GET", "/api/v1/projects/project-a/secrets/K?env=dev", "", ""), 500)
	checkStatus(t, f.localRequest("GET", "/api/v1/projects/project-a/export?env=dev", "", ""), 500)
	checkStatus(t, f.localRequest("DELETE", "/api/v1/projects/project-a/secrets/K?env=dev&expectedVersion=2", "", "stale-delete"), 409)
	checkStatus(t, f.localRequest("DELETE", "/api/v1/projects/project-a/secrets/K?env=dev&expectedVersion=3", "", "delete"), 200)
	checkStatus(t, f.localRequest("DELETE", "/api/v1/projects/project-a/secrets/K?env=dev&expectedVersion=3", "", "delete"), 200)
	testdb.FailAudit(t, f.db)
	checkStatus(t, f.localRequest("POST", path, `{"key":"NEW","value":"hidden"}`, "failed-write"), 500)
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unaudited secret persisted: %d %v", n, err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM mutation_receipts WHERE request_key='failed-write'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed receipt persisted: %d %v", n, err)
	}
}
