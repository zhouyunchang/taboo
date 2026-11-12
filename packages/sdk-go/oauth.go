package taboo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthCredentials selects an external authorization server explicitly. Resource
// is sent only when that server supports RFC 8707. Audience enforcement remains
// mandatory at the resource server regardless of this request parameter.
type OAuthCredentials struct {
	Issuer          string
	MetadataMode    string // oauth (RFC 8414) or oidc (OIDC Discovery)
	ClientID        string
	ClientSecret    string
	Scopes          []string
	Resource        string
	Organization    string // taboo organization slug
	TrustID         string
	endpoint        string
	metadataExpires time.Time
	expires         time.Time
}

func protocolURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil, fmt.Errorf("HTTPS or loopback HTTP URL required")
	}
	return u, nil
}
func (c *Client) WithOAuthClientCredentials(config OAuthCredentials) (*Client, error) {
	if _, err := protocolURL(config.Issuer); err != nil {
		return nil, err
	}
	if _, err := protocolURL(c.BaseURL); err != nil {
		return nil, err
	}
	if config.MetadataMode == "" {
		config.MetadataMode = "oauth"
	}
	if config.MetadataMode != "oauth" && config.MetadataMode != "oidc" {
		return nil, fmt.Errorf("metadata mode must be oauth or oidc")
	}
	if config.ClientID == "" || config.ClientSecret == "" || config.Organization == "" || config.TrustID == "" || len(config.Scopes) == 0 {
		return nil, fmt.Errorf("OAuth client credential, scopes, organization and trust ID are required")
	}
	for _, scope := range config.Scopes {
		if scope == "" || strings.ContainsAny(scope, " \t\r\n") {
			return nil, fmt.Errorf("each OAuth scope must be one exact value")
		}
	}
	if config.Resource != "" {
		u, err := url.Parse(config.Resource)
		if err != nil || !u.IsAbs() || u.Fragment != "" {
			return nil, fmt.Errorf("resource must be an absolute URI without a fragment")
		}
	}
	config.Scopes = append([]string(nil), config.Scopes...)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.oauth = &config
	c.identity = nil
	c.token = ""
	c.refresh = ""
	c.onRefresh = nil
	return c, nil
}
func limitedJSON(r io.Reader, out any) error {
	b, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return fmt.Errorf("OAuth response unavailable")
	}
	if json.Unmarshal(b, out) != nil {
		return fmt.Errorf("invalid OAuth response")
	}
	return nil
}
func (c *Client) protocolClient() *http.Client {
	client := *c.HTTPClient
	if client.Timeout == 0 || client.Timeout > 30*time.Second {
		client.Timeout = 30 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

// Called while holding the client's mutex: concurrent API requests share one
// token exchange. Provider failures never cause a local-credential fallback.
func (c *Client) oauthToken(ctx context.Context) (string, error) {
	cfg := c.oauth
	if c.token != "" && time.Until(cfg.expires) > 30*time.Second {
		return c.token, nil
	}
	client := c.protocolClient()
	if cfg.endpoint == "" || time.Now().After(cfg.metadataExpires) {
		issuer, _ := protocolURL(cfg.Issuer)
		metadata := strings.TrimSuffix(cfg.Issuer, "/") + "/.well-known/openid-configuration"
		if cfg.MetadataMode == "oauth" {
			metadata = issuer.Scheme + "://" + issuer.Host + "/.well-known/oauth-authorization-server" + strings.TrimSuffix(issuer.EscapedPath(), "/")
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", metadata, nil)
		res, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("OAuth metadata unavailable")
		}
		var d struct {
			Issuer        string   `json:"issuer"`
			TokenEndpoint string   `json:"token_endpoint"`
			AuthMethods   []string `json:"token_endpoint_auth_methods_supported"`
		}
		err = limitedJSON(res.Body, &d)
		res.Body.Close()
		if err != nil || res.StatusCode != 200 || d.Issuer != cfg.Issuer {
			return "", fmt.Errorf("OAuth metadata rejected")
		}
		endpoint, err := protocolURL(d.TokenEndpoint)
		if err != nil || issuer.Scheme != endpoint.Scheme || issuer.Host != endpoint.Host {
			return "", fmt.Errorf("untrusted OAuth token endpoint")
		}
		basic := len(d.AuthMethods) == 0
		for _, m := range d.AuthMethods {
			if m == "client_secret_basic" {
				basic = true
			}
		}
		if !basic {
			return "", fmt.Errorf("OAuth client_secret_basic is not supported")
		}
		cfg.endpoint = d.TokenEndpoint
		cfg.metadataExpires = time.Now().Add(5 * time.Minute)
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join(cfg.Scopes, " ")}}
	if cfg.Resource != "" {
		form.Set("resource", cfg.Resource)
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", cfg.endpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(cfg.ClientID), url.QueryEscape(cfg.ClientSecret))
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OAuth token exchange unavailable")
	}
	defer res.Body.Close()
	var token struct {
		Access  string  `json:"access_token"`
		Type    string  `json:"token_type"`
		Expires int64   `json:"expires_in"`
		Scope   *string `json:"scope"`
	}
	if limitedJSON(res.Body, &token) != nil || res.StatusCode != 200 || token.Access == "" || len(token.Access) > 16384 || !strings.EqualFold(token.Type, "Bearer") || token.Expires <= 0 || token.Expires > 86400 {
		return "", fmt.Errorf("OAuth token exchange rejected")
	}
	if token.Scope != nil {
		granted := strings.Fields(*token.Scope)
		for _, scope := range cfg.Scopes {
			ok := false
			for _, g := range granted {
				if g == scope {
					ok = true
				}
			}
			if !ok {
				return "", fmt.Errorf("OAuth server did not grant every requested scope")
			}
		}
	}
	c.token = token.Access
	cfg.expires = time.Now().Add(time.Duration(token.Expires) * time.Second)
	return c.token, nil
}
