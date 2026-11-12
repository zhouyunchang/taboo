// Package oauth validates resource tokens using public OAuth metadata and token
// profiles. It has no knowledge of vendor claims or taboo's tenant/role model.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "github.com/coreos/go-oidc/v3/oidc"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oidc"
)

var ErrUnavailable = errors.New("authorization server unavailable or incompatible")
var ErrInvalid = errors.New("invalid resource token")

const JWT = "JWT_RFC9068"
const Introspection = "INTROSPECTION_RFC7662"

// Metadata source is explicit: RFC 8414 and OIDC use different well-known paths.
type Provider struct {
	Issuer                   string   `json:"issuer"`
	TokenEndpoint            string   `json:"token_endpoint"`
	JWKSURI                  string   `json:"jwks_uri"`
	IntrospectionEndpoint    string   `json:"introspection_endpoint"`
	TokenAuthMethods         []string `json:"token_endpoint_auth_methods_supported"`
	IntrospectionAuthMethods []string `json:"introspection_endpoint_auth_methods_supported"`
	client                   *http.Client
	keys                     core.KeySet
}

type limitedBody struct {
	io.Reader
	io.Closer
}
type boundedTransport struct{}

func (boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	res.Body = &limitedBody{Reader: io.LimitReader(res.Body, (1<<20)+1), Closer: res.Body}
	return res, nil
}
func Client() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: boundedTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func MetadataURL(issuer, mode string) (string, error) {
	u, err := oidc.URL(issuer)
	if err != nil {
		return "", ErrUnavailable
	}
	switch mode {
	case "oauth":
		return u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server" + strings.TrimSuffix(u.EscapedPath(), "/"), nil
	case "oidc":
		return strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration", nil
	default:
		return "", ErrUnavailable
	}
}
func sameOrigin(issuer, endpoint string) bool {
	i, err := oidc.URL(issuer)
	if err != nil {
		return false
	}
	e, err := oidc.URL(endpoint)
	return err == nil && i.Scheme == e.Scheme && i.Host == e.Host
}
func decode(r io.Reader, v any) error {
	b, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return ErrUnavailable
	}
	if json.Unmarshal(b, v) != nil {
		return ErrUnavailable
	}
	return nil
}
func contains(list []string, value string) bool {
	for _, x := range list {
		if x == value {
			return true
		}
	}
	return false
}

func Discover(ctx context.Context, issuer, mode string) (*Provider, error) {
	endpoint, err := MetadataURL(issuer, mode)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	c := Client()
	res, err := c.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	var p Provider
	if res.StatusCode != 200 || decode(res.Body, &p) != nil || p.Issuer != issuer {
		return nil, ErrUnavailable
	}
	if !sameOrigin(issuer, p.TokenEndpoint) {
		return nil, ErrUnavailable
	}
	for _, e := range []string{p.JWKSURI, p.IntrospectionEndpoint} {
		if e != "" && !sameOrigin(issuer, e) {
			return nil, ErrUnavailable
		}
	}
	p.client = c
	if p.JWKSURI != "" {
		p.keys = core.NewRemoteKeySet(core.ClientContext(context.Background(), c), p.JWKSURI)
	}
	return &p, nil
}

func (p *Provider) Supports(profile string) bool {
	switch profile {
	case JWT:
		return p.keys != nil
	case Introspection:
		return p.IntrospectionEndpoint != "" && (len(p.IntrospectionAuthMethods) == 0 || contains(p.IntrospectionAuthMethods, "client_secret_basic"))
	}
	return false
}

type ResourceToken struct {
	Issuer    string          `json:"iss"`
	Subject   string          `json:"sub"`
	ClientID  string          `json:"client_id"`
	Audience  json.RawMessage `json:"aud"`
	IssuedAt  int64           `json:"iat"`
	ExpiresAt int64           `json:"exp"`
	NotBefore int64           `json:"nbf"`
	ID        string          `json:"jti"`
	Scope     string          `json:"scope"`
	Active    bool            `json:"active"`
	TokenType string          `json:"token_type"`
}

func audienceHas(raw json.RawMessage, want string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == want
	}
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		return false
	}
	return contains(list, want)
}
func (t ResourceToken) HasScope(scope string) bool {
	return contains(strings.Split(t.Scope, " "), scope)
}
func validScopes(raw string) bool {
	if raw == "" {
		return false
	}
	for _, scope := range strings.Split(raw, " ") {
		if scope == "" {
			return false
		}
		for _, c := range scope {
			if c != 0x21 && (c < 0x23 || c > 0x5b) && (c < 0x5d || c > 0x7e) {
				return false
			}
		}
	}
	return true
}
func (t ResourceToken) validate(issuer, audience, profile string, maxLifetime time.Duration) error {
	now := time.Now().Unix()
	if issuer == "" || audience == "" || t.Issuer != issuer || t.Subject == "" || t.ClientID == "" || !audienceHas(t.Audience, audience) || t.ExpiresAt <= now || t.IssuedAt <= 0 || t.IssuedAt > now+30 || t.ExpiresAt <= t.IssuedAt || t.NotBefore > now+30 || !validScopes(t.Scope) {
		return ErrInvalid
	}
	if profile == JWT && (t.ID == "" || maxLifetime <= 0 || t.ExpiresAt-t.IssuedAt > int64(maxLifetime.Seconds())) {
		return ErrInvalid
	}
	if profile == Introspection && (!t.Active || !strings.EqualFold(t.TokenType, "Bearer")) {
		return ErrInvalid
	}
	return nil
}

func (p *Provider) VerifyJWT(ctx context.Context, raw, audience string, maxLifetime time.Duration) (ResourceToken, error) {
	var token ResourceToken
	if !p.Supports(JWT) || len(raw) > 16384 {
		return token, ErrInvalid
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return token, ErrInvalid
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return token, ErrInvalid
	}
	var h struct {
		Alg  string `json:"alg"`
		Type string `json:"typ"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || (h.Type != "at+jwt" && h.Type != "application/at+jwt") {
		return token, ErrInvalid
	}
	payload, err := p.keys.VerifySignature(ctx, raw)
	if err != nil {
		return token, ErrInvalid
	}
	if json.Unmarshal(payload, &token) != nil {
		return token, ErrInvalid
	}
	if err = token.validate(p.Issuer, audience, JWT, maxLifetime); err != nil {
		return ResourceToken{}, err
	}
	return token, nil
}

// Introspect does not inspect opaque tokens. It requires the provider's live
// standard response to supply every field in taboo's explicit resource profile.
// It is intentionally not cached, including on provider errors.
func (p *Provider) Introspect(ctx context.Context, raw, audience, clientID, secret string) (ResourceToken, error) {
	var token ResourceToken
	if !p.Supports(Introspection) || raw == "" || len(raw) > 16384 || clientID == "" || secret == "" {
		return token, ErrInvalid
	}
	form := url.Values{"token": {raw}, "token_type_hint": {"access_token"}}
	req, err := http.NewRequestWithContext(ctx, "POST", p.IntrospectionEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return token, ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	res, err := p.client.Do(req)
	if err != nil {
		return token, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || decode(res.Body, &token) != nil {
		return token, ErrUnavailable
	}
	if err = token.validate(p.Issuer, audience, Introspection, 0); err != nil {
		return ResourceToken{}, err
	}
	return token, nil
}
