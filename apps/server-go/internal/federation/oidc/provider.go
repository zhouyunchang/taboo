// Package oidc implements the public OIDC relying-party boundary. It knows
// nothing about local organizations, role names, or vendor-specific claims.
package oidc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "github.com/coreos/go-oidc/v3/oidc"
)

var ErrProvider = errors.New("oidc provider unavailable or incompatible")
var ErrToken = errors.New("oidc token invalid")

// URL permits HTTPS or an explicit loopback HTTP development endpoint. The
// issuer is compared exactly; trailing slashes are not silently normalized.
func URL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, ErrProvider
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "[::1]" || u.Hostname() == "::1" || u.Hostname() == "localhost")) {
		return nil, ErrProvider
	}
	return u, nil
}

type Provider struct {
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	Issuer                string   `json:"issuer"`
	EndSessionEndpoint    string   `json:"end_session_endpoint"`
	IntrospectionEndpoint string   `json:"introspection_endpoint"`
	IntrospectionMethods  []string `json:"introspection_endpoint_auth_methods_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
	ACRValues             []string `json:"acr_values_supported"`
	SigningAlgs           []string `json:"id_token_signing_alg_values_supported"`
	AuthMethods           []string `json:"token_endpoint_auth_methods_supported"`
	keySet                core.KeySet
	client                *http.Client
}

// Discover uses a bounded, non-redirecting client. This initial transport
// profile requires same-origin endpoints, rather than guessing endpoint trust.
func Discover(ctx context.Context, issuer string) (*Provider, error) {
	u, err := URL(issuer)
	if err != nil {
		return nil, err
	}
	c := &http.Client{Transport: boundedTransport{http.DefaultTransport}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, ErrProvider
	}
	res, err := c.Do(r)
	if err != nil {
		return nil, ErrProvider
	}
	defer res.Body.Close()
	var p Provider
	if res.StatusCode != http.StatusOK || decode(res.Body, &p) != nil || p.Issuer != issuer {
		return nil, ErrProvider
	}
	for _, endpoint := range []string{p.AuthorizationEndpoint, p.TokenEndpoint, p.JWKSURI} {
		e, err := URL(endpoint)
		if err != nil || e.Scheme != u.Scheme || e.Host != u.Host {
			return nil, ErrProvider
		}
	}
	for _, endpoint := range []string{p.EndSessionEndpoint, p.IntrospectionEndpoint} {
		if endpoint == "" {
			continue
		}
		e, err := URL(endpoint)
		if err != nil || e.Scheme != u.Scheme || e.Host != u.Host {
			return nil, ErrProvider
		}
	}

	if len(p.AuthMethods) > 0 && !contains(p.AuthMethods, "client_secret_basic") {
		return nil, ErrProvider
	}
	if !contains(p.SigningAlgs, "RS256") {
		return nil, ErrProvider
	}
	p.client = c
	// The cached key set must not inherit a login request's cancellation.
	p.keySet = core.NewRemoteKeySet(core.ClientContext(context.Background(), c), p.JWKSURI)
	return &p, nil
}

// Limit every provider response, including JWKS reads performed by the verifier.
type boundedTransport struct{ http.RoundTripper }

func (t boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.RoundTripper.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	res.Body.Close()
	if err != nil || len(raw) > 1<<20 {
		return nil, ErrProvider
	}
	res.Body = io.NopCloser(bytes.NewReader(raw))
	return res, nil
}

func contains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func decode(r io.Reader, v any) error {
	b, err := io.ReadAll(io.LimitReader(r, 1<<20+1))
	if err != nil || len(b) > 1<<20 {
		return ErrProvider
	}
	return json.Unmarshal(b, v)
}

type Identity struct {
	Subject         string
	ExpiresAt       time.Time
	Claims          map[string]any
	RawIDToken      string
	AccessToken     string
	RefreshToken    string
	AccessExpiresAt time.Time
}

func (p *Provider) Verify(ctx context.Context, raw, clientID, nonce string) (Identity, error) {
	return p.verify(ctx, raw, clientID, nonce, true)
}
func (p *Provider) verify(ctx context.Context, raw, clientID, nonce string, requireNonce bool) (Identity, error) {
	v := core.NewVerifier(p.Issuer, p.keySet, &core.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}})
	t, err := v.Verify(ctx, raw)
	if err != nil || t.Subject == "" || nonce == "" || ((requireNonce || t.Nonce != "") && subtle.ConstantTimeCompare([]byte(t.Nonce), []byte(nonce)) != 1) || t.IssuedAt.IsZero() || t.IssuedAt.After(time.Now().Add(30*time.Second)) {
		return Identity{}, ErrToken
	}
	var claims map[string]any
	if t.Claims(&claims) != nil {
		return Identity{}, ErrToken
	}
	azp, _ := claims["azp"].(string)
	if (len(t.Audience) > 1 && azp != clientID) || (azp != "" && azp != clientID) {
		return Identity{}, ErrToken
	}
	return Identity{Subject: t.Subject, ExpiresAt: t.Expiry, Claims: claims, RawIDToken: raw}, nil
}

func (p *Provider) Exchange(ctx context.Context, clientID, secret, code, verifier, redirect, nonce string) (Identity, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	return p.token(ctx, clientID, secret, form, nonce, "", true)
}

// Refresh validates any new ID Token against the original issuer, audience,
// subject and nonce (when present). A refresh cannot reset the authentication time.
func (p *Provider) Refresh(ctx context.Context, clientID, secret, refresh, nonce, subject string) (Identity, error) {
	return p.token(ctx, clientID, secret, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}, nonce, subject, false)
}
func (p *Provider) token(ctx context.Context, clientID, secret string, form url.Values, nonce, subject string, initial bool) (Identity, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, ErrProvider
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	res, err := p.client.Do(r)
	if err != nil {
		return Identity{}, ErrProvider
	}
	defer res.Body.Close()
	var token struct {
		IDToken string `json:"id_token"`
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Type    string `json:"token_type"`
		Expires int64  `json:"expires_in"`
	}
	if res.StatusCode != 200 || decode(res.Body, &token) != nil || token.Access == "" || len(token.Access) > 16384 || len(token.Refresh) > 16384 || !strings.EqualFold(token.Type, "Bearer") || token.Expires < 0 || token.Expires > 86400 {
		return Identity{}, ErrProvider
	}
	var identity Identity
	if token.IDToken != "" {
		identity, err = p.verify(ctx, token.IDToken, clientID, nonce, initial)
		if err != nil {
			return Identity{}, err
		}
	} else if initial {
		return Identity{}, ErrToken
	} else {
		identity.Subject = subject
	}
	if hash, ok := identity.Claims["at_hash"]; ok {
		expected, ok := hash.(string)
		sum := sha256.Sum256([]byte(token.Access))
		if !ok || subtle.ConstantTimeCompare([]byte(expected), []byte(base64.RawURLEncoding.EncodeToString(sum[:16]))) != 1 {
			return Identity{}, ErrToken
		}
	}
	if !initial && identity.Subject != subject {
		return Identity{}, ErrToken
	}
	identity.AccessToken, identity.RefreshToken = token.Access, token.Refresh
	if token.Expires > 0 {
		identity.AccessExpiresAt = time.Now().Add(time.Duration(token.Expires) * time.Second)
	} else {
		identity.AccessExpiresAt = identity.ExpiresAt
	}
	if identity.AccessExpiresAt.IsZero() {
		return Identity{}, ErrToken
	}
	return identity, nil
}

func (p *Provider) SupportsOnline() bool {
	return p.IntrospectionEndpoint != "" && (len(p.IntrospectionMethods) == 0 || contains(p.IntrospectionMethods, "client_secret_basic"))
}

// CheckAccess requires a live, standard introspection response bound to the
// original browser subject/client and the operator's explicitly selected audience.
func (p *Provider) CheckAccess(ctx context.Context, raw, audience, clientID, secret, subject string) error {
	if !p.SupportsOnline() || raw == "" || audience == "" || subject == "" {
		return ErrToken
	}
	form := url.Values{"token": {raw}, "token_type_hint": {"access_token"}}
	r, err := http.NewRequestWithContext(ctx, "POST", p.IntrospectionEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ErrProvider
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	res, err := p.client.Do(r)
	if err != nil {
		return ErrProvider
	}
	defer res.Body.Close()
	var token struct {
		Active    bool            `json:"active"`
		Issuer    string          `json:"iss"`
		Subject   string          `json:"sub"`
		ClientID  string          `json:"client_id"`
		Audience  json.RawMessage `json:"aud"`
		Expires   int64           `json:"exp"`
		Issued    int64           `json:"iat"`
		NotBefore int64           `json:"nbf"`
		Type      string          `json:"token_type"`
	}
	if res.StatusCode != 200 || decode(res.Body, &token) != nil {
		return ErrProvider
	}
	var aud []string
	var single string
	if json.Unmarshal(token.Audience, &single) == nil {
		aud = []string{single}
	} else {
		_ = json.Unmarshal(token.Audience, &aud)
	}
	now := time.Now().Unix()
	if !token.Active || token.Issuer != p.Issuer || token.Subject != subject || token.ClientID != clientID || !contains(aud, audience) || !strings.EqualFold(token.Type, "Bearer") || token.Expires <= now || token.Issued <= 0 || token.Issued > now+30 || token.Expires <= token.Issued || token.NotBefore > now+30 {
		return ErrToken
	}
	return nil
}
