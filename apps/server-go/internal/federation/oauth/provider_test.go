package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

type fixture struct {
	server *httptest.Server
	mu     sync.Mutex
	key    *rsa.PrivateKey
	kid    string
	claims map[string]any
	status int
	calls  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{key: key, kid: "first", status: 200}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		issuer := f.server.URL + "/tenant/path"
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server/tenant/path", "/tenant/path/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/keys", "introspection_endpoint": f.server.URL + "/introspect", "introspection_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: f.kid, Algorithm: "RS256", Use: "sig"}}})
		case "/introspect":
			f.calls++
			r.ParseForm()
			id, secret, ok := r.BasicAuth()
			if !ok || id != url.QueryEscape("reader:app") || secret != url.QueryEscape("s+ p") || r.Form.Get("token") != "opaque-unparsed" || r.Form.Get("token_type_hint") != "access_token" {
				w.WriteHeader(401)
				return
			}
			w.WriteHeader(f.status)
			json.NewEncoder(w).Encode(f.claims)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.server.Close)
	f.claims = map[string]any{"iss": f.server.URL + "/tenant/path", "sub": "service-subject", "client_id": "service-client", "aud": "https://taboo.example/api", "iat": time.Now().Unix(), "exp": time.Now().Add(5 * time.Minute).Unix(), "jti": "unique-token", "scope": "taboo:read taboo:reveal", "token_type": "Bearer", "active": true}
	return f
}
func (f *fixture) sign(t *testing.T, typ string, overrides map[string]any) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	claims := map[string]any{}
	for k, v := range f.claims {
		claims[k] = v
	}
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: f.key, KeyID: f.kid}}, (&jose.SignerOptions{}).WithType(jose.ContentType(typ)))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestResourceJWTProfile(t *testing.T) {
	f := newFixture(t)
	p, err := Discover(context.Background(), f.server.URL+"/tenant/path", "oauth")
	if err != nil {
		t.Fatal(err)
	}
	verify := func(raw string) error {
		_, err := p.VerifyJWT(context.Background(), raw, "https://taboo.example/api", 15*time.Minute)
		return err
	}
	if err = verify(f.sign(t, "at+jwt", nil)); err != nil {
		t.Fatal(err)
	}
	if err = verify(f.sign(t, "application/at+jwt", nil)); err != nil {
		t.Fatal(err)
	}
	if err = verify(f.sign(t, "JWT", nil)); err == nil {
		t.Fatal("OIDC ID token accepted as resource token")
	}
	for _, claim := range []string{"iss", "sub", "client_id", "aud", "iat", "exp", "jti", "scope"} {
		t.Run("missing_"+claim, func(t *testing.T) {
			if verify(f.sign(t, "at+jwt", map[string]any{claim: nil})) == nil {
				t.Fatal("required claim missing")
			}
		})
	}
	for name, change := range map[string]map[string]any{"issuer": {"iss": "https://other.example"}, "audience": {"aud": "other-resource"}, "expiry": {"exp": time.Now().Unix() - 1}, "future": {"nbf": time.Now().Add(time.Hour).Unix()}, "long_lived": {"exp": time.Now().Add(time.Hour).Unix()}, "scope_type": {"scope": []string{"taboo:read"}}, "scope_separator": {"scope": "taboo:read\ttaboo:reveal"}} {
		t.Run(name, func(t *testing.T) {
			if verify(f.sign(t, "at+jwt", change)) == nil {
				t.Fatal("invalid resource token accepted")
			}
		})
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.key = key
	f.kid = "rotated"
	f.mu.Unlock()
	if err = verify(f.sign(t, "at+jwt", nil)); err != nil {
		t.Fatalf("JWK rotation failed: %v", err)
	}
}
func TestOpaqueIntrospectionAndAvailability(t *testing.T) {
	f := newFixture(t)
	p, err := Discover(context.Background(), f.server.URL+"/tenant/path", "oidc")
	if err != nil {
		t.Fatal(err)
	}
	verify := func() (ResourceToken, error) {
		return p.Introspect(context.Background(), "opaque-unparsed", "https://taboo.example/api", "reader:app", "s+ p")
	}
	token, err := verify()
	if err != nil || token.Subject != "service-subject" || !token.HasScope("taboo:reveal") {
		t.Fatalf("introspection %+v %v", token, err)
	}
	f.mu.Lock()
	f.claims["active"] = false
	f.mu.Unlock()
	if _, err = verify(); err != ErrInvalid {
		t.Fatalf("inactive token accepted: %v", err)
	}
	f.mu.Lock()
	f.claims["active"] = true
	delete(f.claims, "aud")
	f.mu.Unlock()
	if _, err = verify(); err != ErrInvalid {
		t.Fatalf("active without audience accepted: %v", err)
	}
	f.mu.Lock()
	f.status = 503
	f.mu.Unlock()
	if _, err = verify(); err != ErrUnavailable {
		t.Fatalf("provider failure not fail closed: %v", err)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 4 {
		t.Fatalf("introspection cached: %d requests", calls)
	}
}
