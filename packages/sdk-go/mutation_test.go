package taboo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMutationOptionsPreserveReceiptAndVersion(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Idempotency-Key") != "retained-receipt" {
			t.Error("receipt lost")
		}
		if r.Method == "DELETE" {
			if r.URL.Query().Get("expectedVersion") != "7" {
				t.Error("delete fence lost")
			}
		} else {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["expectedVersion"] != float64(7) {
				t.Error("mutation fence lost")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":8}`))
	}))
	defer srv.Close()
	c := New(srv.URL).WithToken("fixture")
	expected := 7
	options := MutationOptions{IdempotencyKey: "retained-receipt", ExpectedVersion: &expected}
	for i := 0; i < 2; i++ {
		if _, err := c.UpsertSecret(context.Background(), "p", "dev", "/", "KEY", "value", "", nil, options); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.RollbackSecret(context.Background(), "p", "dev", "/", "KEY", 3, options); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteSecret(context.Background(), "p", "dev", "/", "KEY", options); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("unexpected implicit retry: %d", calls)
	}
}
