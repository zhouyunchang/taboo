// Package testoauth is an independent authorization server fixture using only
// RFC 6749, 8414, 9068 and 7662 messages. It issues no vendor/tenant role claims.
package testoauth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type Provider struct {
	Server      *httptest.Server
	mu          sync.Mutex
	key         *rsa.PrivateKey
	tokens      map[string]map[string]any
	disabled    bool
	unavailable bool
}

func New(t *testing.T) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{key: key, tokens: map[string]map[string]any{}}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.Server.Close)
	return p
}
func (p *Provider) Disable() { p.mu.Lock(); defer p.mu.Unlock(); p.disabled = true }
func (p *Provider) Outage()  { p.mu.Lock(); defer p.mu.Unlock(); p.unavailable = true }
func (p *Provider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if p.unavailable {
		w.WriteHeader(503)
		return
	}
	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		json.NewEncoder(w).Encode(map[string]any{"issuer": p.Server.URL, "token_endpoint": p.Server.URL + "/token", "jwks_uri": p.Server.URL + "/keys", "introspection_endpoint": p.Server.URL + "/introspect", "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "introspection_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
	case "/keys":
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "fixture-key", Algorithm: "RS256", Use: "sig"}}})
	case "/token":
		r.ParseForm()
		client, secret, ok := r.BasicAuth()
		if !ok || secret != "fixture-machine-secret" || (client != "machine-jwt" && client != "machine-opaque") || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("resource") != "https://taboo.example/api" || p.disabled {
			w.WriteHeader(401)
			return
		}
		claims := map[string]any{"iss": p.Server.URL, "sub": "service:" + client, "client_id": client, "aud": "https://taboo.example/api", "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "jti": tc.NewID(), "scope": r.Form.Get("scope"), "active": true, "token_type": "Bearer"}
		token := tc.NewID()
		if client == "machine-jwt" {
			signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "fixture-key"}}, (&jose.SignerOptions{}).WithType("at+jwt"))
			if err != nil {
				w.WriteHeader(500)
				return
			}
			b, _ := json.Marshal(claims)
			signed, err := signer.Sign(b)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			token, err = signed.CompactSerialize()
			if err != nil {
				w.WriteHeader(500)
				return
			}
		}
		p.tokens[token] = claims
		json.NewEncoder(w).Encode(map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": 300, "scope": r.Form.Get("scope")})
	case "/introspect":
		r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok || id != "resource-server" || secret != "fixture-resource-secret" {
			w.WriteHeader(401)
			return
		}
		claims, ok := p.tokens[r.Form.Get("token")]
		if !ok || p.disabled {
			json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		json.NewEncoder(w).Encode(claims)
	default:
		w.WriteHeader(404)
	}
}
