package redact

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONRedactsSecretFields(t *testing.T) {
	in := []byte(`{"password":"hunter2","nested":{"client_secret":"sek","token_ttl":900},"match":"wJalr","content":"A=1","ok":"visible","items":[{"value":"hide-me","file":"a.env"}]}`)
	out := JSON(in)
	for _, leaked := range []string{"hunter2", "sek", "wJalr", "A=1", "hide-me"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("leaked %s in %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"ok":"visible"`) || !strings.Contains(out, `"token_ttl":900`) || !strings.Contains(out, `"file":"a.env"`) || !strings.Contains(out, marker) {
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
