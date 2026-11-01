package redact

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONRedactsSecretFields(t *testing.T) {
	in := []byte(`{"password":"hunter2","nested":{"client_secret":"sek","token_ttl":900},"match":"wJalr","content":"A=1","name":"api","ok":"not-on-allowlist","connection":"mysql://u:pw@h/db","new_password":"np","secrets":{"API_KEY":"bulk-val"},"items":[{"value":"hide-me","file":"a.env"}]}`)
	out := JSON(in)
	for _, leaked := range []string{"hunter2", "sek", "wJalr", "A=1", "hide-me", "not-on-allowlist", "pw@h", "np", "bulk-val"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("leaked %s in %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"name":"api"`) || !strings.Contains(out, `"token_ttl":900`) || !strings.Contains(out, `"file":"a.env"`) || !strings.Contains(out, marker) {
		t.Fatalf("redaction damaged safe fields: %s", out)
	}
	if JSON([]byte("not json")) != "[non-json omitted]" {
		t.Fatal("non-json body was logged")
	}
}

func TestAccessLogOmitsSecret(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	h := AccessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/identities/token?token=raw-query", strings.NewReader(`{"client_secret":"super-secret","name":"ci"}`))
	req.Header.Set("Authorization", "Bearer raw-token")
	h.ServeHTTP(httptest.NewRecorder(), req)
	logged := buf.String()
	if strings.Contains(logged, "super-secret") || strings.Contains(logged, "raw-token") || strings.Contains(logged, "raw-query") {
		t.Fatalf("access log leaked: %s", logged)
	}
	if !strings.Contains(logged, marker) || !strings.Contains(logged, `name`) || !strings.Contains(logged, `ci`) {
		t.Fatalf("access log: %s", logged)
	}
}

func TestAccessLogKeepsLargeBodyIntact(t *testing.T) {
	big := strings.Repeat("x", 3<<20)
	var got int
	h := AccessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = len(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(big)))
	if got != len(big) {
		t.Fatalf("handler saw %d bytes, want %d", got, len(big))
	}
}
