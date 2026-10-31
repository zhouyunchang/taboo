// Package github implements instance-level "Sign in with GitHub" using a GitHub
// OAuth App (OAuth 2.0 authorization code + state + PKCE S256). GitHub is not an
// OpenID Provider, so this is deliberately separate from the org-scoped OIDC
// federation in internal/oidc: GitHub only proves "who owns GitHub account N";
// Taboo issues its own local session tokens afterwards.
package github

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config is loaded from TABOO_GITHUB_* environment variables by the operator.
// GitHub login is disabled unless both client id and client secret are set.
type Config struct {
	ClientID     string
	ClientSecret string
	// Access policy: a GitHub account may sign in if it matches ANY of the
	// lists below, or AllowAny is explicitly set. All comparisons are
	// case-insensitive. Teams are "org/team-slug".
	AllowedUsers []string
	AllowedOrgs  []string
	AllowedTeams []string
	AllowAny     bool
	// AutoCreate provisions a new Taboo account (with a personal org) for an
	// admitted GitHub account that is not linked yet. Default false: unlinked
	// accounts are rejected and must be linked from an existing account first.
	AutoCreate bool
	// JoinOrg (slug) / JoinRole optionally add auto-created users to a shared org.
	JoinOrg  string
	JoinRole string
	// Endpoint overrides (GitHub Enterprise Server or tests).
	OAuthURL string // default https://github.com
	APIURL   string // default https://api.github.com
}

func (c Config) Enabled() bool { return c.ClientID != "" && c.ClientSecret != "" }

// Issuer namespaces external_identities rows; the subject is the immutable
// numeric GitHub user id (never the login or email, which can change).
func (c Config) Issuer() string { return strings.TrimRight(c.OAuthURL, "/") }

func (c Config) needsOrgScope() bool { return len(c.AllowedOrgs) > 0 || len(c.AllowedTeams) > 0 }

func list(v string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ConfigFromEnv reads TABOO_GITHUB_*; get is usually os.Getenv. An error means
// GitHub login was requested but is misconfigured (fail closed at startup).
func ConfigFromEnv(get func(string) string) (Config, error) {
	c := Config{
		ClientID:     strings.TrimSpace(get("TABOO_GITHUB_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(get("TABOO_GITHUB_CLIENT_SECRET")),
		AllowedUsers: list(get("TABOO_GITHUB_ALLOWED_USERS")),
		AllowedOrgs:  list(get("TABOO_GITHUB_ALLOWED_ORGS")),
		AllowedTeams: list(get("TABOO_GITHUB_ALLOWED_TEAMS")),
		AllowAny:     truthy(get("TABOO_GITHUB_ALLOW_ANY")),
		AutoCreate:   truthy(get("TABOO_GITHUB_AUTOCREATE")),
		JoinOrg:      strings.TrimSpace(get("TABOO_GITHUB_JOIN_ORG")),
		JoinRole:     strings.ToLower(strings.TrimSpace(get("TABOO_GITHUB_JOIN_ROLE"))),
		OAuthURL:     strings.TrimRight(strings.TrimSpace(get("TABOO_GITHUB_OAUTH_URL")), "/"),
		APIURL:       strings.TrimRight(strings.TrimSpace(get("TABOO_GITHUB_API_URL")), "/"),
	}
	if f := strings.TrimSpace(get("TABOO_GITHUB_CLIENT_SECRET_FILE")); f != "" && c.ClientSecret == "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return Config{}, fmt.Errorf("TABOO_GITHUB_CLIENT_SECRET_FILE unreadable: %w", err)
		}
		c.ClientSecret = strings.TrimSpace(string(b))
	}
	if c.OAuthURL == "" {
		c.OAuthURL = "https://github.com"
	}
	if c.APIURL == "" {
		c.APIURL = "https://api.github.com"
	}
	if c.JoinRole == "" {
		c.JoinRole = "viewer"
	}
	if (c.ClientID == "") != (c.ClientSecret == "") {
		return Config{}, errors.New("GitHub login needs both TABOO_GITHUB_CLIENT_ID and TABOO_GITHUB_CLIENT_SECRET (or leave both empty to disable)")
	}
	if !c.Enabled() {
		return c, nil
	}
	for _, t := range c.AllowedTeams {
		if p := strings.Split(t, "/"); len(p) != 2 || p[0] == "" || p[1] == "" {
			return Config{}, fmt.Errorf("TABOO_GITHUB_ALLOWED_TEAMS entry %q must be org/team-slug", t)
		}
	}
	if !c.AllowAny && len(c.AllowedUsers)+len(c.AllowedOrgs)+len(c.AllowedTeams) == 0 {
		return Config{}, errors.New("GitHub login enabled without an access policy: set TABOO_GITHUB_ALLOWED_USERS / _ORGS / _TEAMS, or TABOO_GITHUB_ALLOW_ANY=true to admit every GitHub account")
	}
	if c.JoinRole != "viewer" && c.JoinRole != "developer" {
		return Config{}, errors.New("TABOO_GITHUB_JOIN_ROLE must be viewer or developer")
	}
	return c, nil
}
