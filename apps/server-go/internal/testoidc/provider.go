// Package testoidc supplies an independent, minimal public-protocol test OP.
// It deliberately emits no tenant, group, email, or vendor identity claims.
package testoidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type Grant struct {
	Nonce, Challenge, Redirect string
	Claims                     map[string]any
}
type Provider struct {
	Server                *httptest.Server
	Key                   *rsa.PrivateKey
	mu                    sync.Mutex
	grants                map[string]Grant
	tokens                map[string]map[string]any
	refreshes             map[string]map[string]any
	disabled, unavailable bool
	refreshClaims         map[string]any
	refreshCount          int
	keyID                 string
}

func New(t *testing.T) *Provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{Key: key, grants: map[string]Grant{}, keyID: "first", tokens: map[string]map[string]any{}, refreshes: map[string]map[string]any{}}
	p.Server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.Server.Close)
	return p
}
func (p *Provider) Rotate(t *testing.T) {
	t.Helper()
	k, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Key = k
	p.keyID = "second"
}
func (p *Provider) Code(q url.Values, overrides map[string]any) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b [24]byte
	rand.Read(b[:])
	code := base64.RawURLEncoding.EncodeToString(b[:])
	p.grants[code] = Grant{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), overrides}
	return code
}
func (p *Provider) Disable() { p.mu.Lock(); defer p.mu.Unlock(); p.disabled = true }
func (p *Provider) Outage()  { p.mu.Lock(); defer p.mu.Unlock(); p.unavailable = true }
func (p *Provider) RefreshOverrides(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refreshClaims = claims
}
func (p *Provider) RefreshCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.refreshCount }
func randomToken() string {
	var b [24]byte
	rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func cloneClaims(claims map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range claims {
		out[k] = v
	}
	return out
}
func (p *Provider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if p.unavailable {
		w.WriteHeader(503)
		return
	}
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]any{"claims_supported": []string{"sub", "auth_time", "acr", "amr"}, "acr_values_supported": []string{"urn:fixture:loa1", "urn:fixture:loa2"}, "introspection_endpoint": p.Server.URL + "/introspect", "introspection_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "end_session_endpoint": p.Server.URL + "/logout", "issuer": p.Server.URL, "authorization_endpoint": p.Server.URL + "/authorize", "token_endpoint": p.Server.URL + "/token", "jwks_uri": p.Server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
	case "/keys":
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.Key.PublicKey, KeyID: p.keyID, Algorithm: "RS256", Use: "sig"}}})
	case "/token":
		r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if r.Form.Get("grant_type") == "refresh_token" {
			p.refreshCount++
			previous, exists := p.refreshes[r.Form.Get("refresh_token")]
			delete(p.refreshes, r.Form.Get("refresh_token"))
			if !ok || id != "taboo-test" || secret != "fixture-secret" || !exists || p.disabled {
				w.WriteHeader(400)
				return
			}
			claims := cloneClaims(previous)
			claims["iat"] = time.Now().Unix()
			claims["exp"] = time.Now().Add(10 * time.Minute).Unix()
			delete(claims, "nonce")
			for k, v := range p.refreshClaims {
				if v == nil {
					delete(claims, k)
				} else {
					claims[k] = v
				}
			}
			p.issue(w, claims)
			return
		}
		grant, exists := p.grants[r.Form.Get("code")]
		delete(p.grants, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if p.disabled || !ok || id != "taboo-test" || secret != "fixture-secret" || !exists || grant.Redirect != r.Form.Get("redirect_uri") || grant.Challenge != base64.RawURLEncoding.EncodeToString(sum[:]) || r.Form.Get("grant_type") != "authorization_code" {
			w.WriteHeader(400)
			return
		}
		claims := map[string]any{"iss": p.Server.URL, "sub": "subject-a", "aud": "taboo-test", "iat": time.Now().Unix(), "auth_time": time.Now().Unix(), "exp": time.Now().Add(10 * time.Minute).Unix(), "nonce": grant.Nonce}
		for k, v := range grant.Claims {
			if v == nil {
				delete(claims, k)
			} else {
				claims[k] = v
			}
		}
		p.issue(w, claims)
	case "/introspect":
		r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok || id != "taboo-test" || secret != "fixture-secret" {
			w.WriteHeader(401)
			return
		}
		claims, ok := p.tokens[r.Form.Get("token")]
		if !ok || p.disabled {
			json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
		json.NewEncoder(w).Encode(claims)
	case "/logout":
		w.WriteHeader(204)

	default:
		w.WriteHeader(404)
	}
}

func (p *Provider) issue(w http.ResponseWriter, claims map[string]any) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.Key, KeyID: p.keyID}}, nil)
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
	raw, err := signed.CompactSerialize()
	if err != nil {
		w.WriteHeader(500)
		return
	}
	access, refresh := randomToken(), randomToken()
	p.tokens[access] = map[string]any{"active": true, "iss": p.Server.URL, "sub": claims["sub"], "client_id": "taboo-test", "aud": "taboo-test", "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "token_type": "Bearer", "scope": "openid"}
	p.refreshes[refresh] = cloneClaims(claims)
	json.NewEncoder(w).Encode(map[string]any{"id_token": raw, "access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": 300})
}
