package apperr

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInternalMessageIsGeneric(t *testing.T) {
	b, err := json.Marshal(New(500, "INTERNAL", `pq: relation "secrets" does not exist`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pq:") || !strings.Contains(string(b), `"internal error"`) {
		t.Fatalf("leaked: %s", b)
	}
	b, _ = json.Marshal(New(404, "NOT_FOUND", "env not found"))
	if !strings.Contains(string(b), "env not found") {
		t.Fatalf("non-500 changed: %s", b)
	}
}
