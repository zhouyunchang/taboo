package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestURLProfile(t *testing.T) {
	for _, s := range []string{"http://idp.example.org", "https://user:secret@idp.example.org", "https://idp.example.org?x=1", "https://idp.example.org#fragment", "file:///etc/passwd"} {
		if _, e := URL(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{"https://idp.example.org/tenant", "http://127.0.0.1:18081/realms/test", "http://[::1]:8888"} {
		if _, e := URL(s); e != nil {
			t.Fatalf("rejected %q", s)
		}
	}
}
func TestDiscoveryTrustBoundaries(t *testing.T) {
	for _, variant := range []string{"issuer", "endpoint", "algorithm", "authentication", "redirect"} {
		t.Run(variant, func(t *testing.T) {
			var s *httptest.Server
			s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := Provider{Issuer: s.URL, AuthorizationEndpoint: s.URL + "/auth", TokenEndpoint: s.URL + "/token", JWKSURI: s.URL + "/keys", SigningAlgs: []string{"RS256"}, AuthMethods: []string{"client_secret_basic"}}
				switch variant {
				case "issuer":
					p.Issuer += "/"
				case "endpoint":
					p.JWKSURI = "https://another.example.org/keys"
				case "algorithm":
					p.SigningAlgs = []string{"HS256"}
				case "authentication":
					p.AuthMethods = []string{"none"}
				case "redirect":
					http.Redirect(w, r, "https://another.example.org", 302)
					return
				}
				json.NewEncoder(w).Encode(p)
			}))
			defer s.Close()
			if _, e := Discover(context.Background(), s.URL); e == nil {
				t.Fatal("accepted incompatible metadata")
			}
		})
	}
}
