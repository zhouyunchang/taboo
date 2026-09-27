package taboo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuthAcquisitionCacheScopeAndNoFallback(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "token_endpoint": server.URL + "/token", "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
		case "/token":
			calls.Add(1)
			r.ParseForm()
			id, secret, ok := r.BasicAuth()
			if !ok || id != url.QueryEscape("service:client") || secret != url.QueryEscape("test+ secret") || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("resource") != "https://kms.example/api" || r.Form.Get("scope") != "read reveal" {
				t.Error("incorrect client_credentials request")
				w.WriteHeader(400)
				return
			}
			if fail.Load() {
				w.WriteHeader(503)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "opaque-fixture", "token_type": "Bearer", "expires_in": 300, "scope": "read reveal"})
		case "/api/v1/projects/project/secrets":
			if r.Header.Get("X-Taboo-Trust") != "trust" || r.Header.Get("X-Taboo-Organization") != "org" || r.Header.Get("Authorization") != "Bearer opaque-fixture" {
				t.Error("missing resource selection")
			}
			json.NewEncoder(w).Encode(map[string]any{"secrets": []any{}})
		default:
			t.Error("unexpected endpoint or local fallback:", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c, err := New(server.URL).WithIdentity("old", "old").WithOAuthClientCredentials(OAuthCredentials{Issuer: server.URL, ClientID: "service:client", ClientSecret: "test+ secret", Scopes: []string{"read", "reveal"}, Resource: "https://kms.example/api", Organization: "org", TrustID: "trust"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListSecrets(context.Background(), "project", "dev", ""); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("parallel refresh was not coalesced: %d", calls.Load())
	}
	c.mu.Lock()
	c.oauth.expires = time.Now()
	c.mu.Unlock()
	fail.Store(true)
	if _, err = c.ListSecrets(context.Background(), "project", "dev", ""); err == nil {
		t.Fatal("used stale token after exchange failure")
	}
	fail.Store(false)
	if _, err = c.ListSecrets(context.Background(), "project", "dev", ""); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected exchanges %d", calls.Load())
	}
}
func TestOAuthRejectsUntrustedMetadataAndRedirect(t *testing.T) {
	var stolen atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { stolen.Add(1); w.WriteHeader(200) }))
	defer evil.Close()
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": source.URL, "token_endpoint": evil.URL + "/token"})
	}))
	defer source.Close()
	c, err := New(source.URL).WithOAuthClientCredentials(OAuthCredentials{Issuer: source.URL, ClientID: "fixture", ClientSecret: "fixture", Scopes: []string{"read"}, Organization: "a", TrustID: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ListSecrets(context.Background(), "p", "dev", ""); err == nil {
		t.Fatal("cross-origin endpoint trusted")
	}
	if stolen.Load() != 0 {
		t.Fatal("credentials sent to untrusted endpoint")
	}
}
