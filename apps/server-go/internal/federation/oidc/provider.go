// Package oidc implements the public OIDC relying-party boundary. It knows
// nothing about local organizations, role names, or vendor-specific claims.
package oidc

import (
	"context"
	"crypto/subtle"
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
	c := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
	if p.EndSessionEndpoint != "" {
		e, err := URL(p.EndSessionEndpoint)
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
	Subject    string
	ExpiresAt  time.Time
	Claims     map[string]any
	RawIDToken string
}

func (p *Provider) Verify(ctx context.Context, raw, clientID, nonce string) (Identity, error) {
	v := core.NewVerifier(p.Issuer, p.keySet, &core.Config{ClientID: clientID, SupportedSigningAlgs: []string{"RS256"}})
	t, err := v.Verify(ctx, raw)
	if err != nil || t.Subject == "" || nonce == "" || subtle.ConstantTimeCompare([]byte(t.Nonce), []byte(nonce)) != 1 || t.IssuedAt.IsZero() || t.IssuedAt.After(time.Now().Add(30*time.Second)) {
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
	}
	if res.StatusCode != 200 || decode(res.Body, &token) != nil || token.IDToken == "" {
		return Identity{}, ErrProvider
	}
	return p.Verify(ctx, token.IDToken, clientID, nonce)
}
