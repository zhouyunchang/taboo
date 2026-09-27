package server

import (
	"context"
	"encoding/json"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/federation/oauth"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
)

// Enabled by scripts/test-federation.sh, mandatory in its dedicated public CI job.
func TestKeycloakInterop(t *testing.T) {
	issuer := os.Getenv("TABOO_TEST_KEYCLOAK_ISSUER")
	if issuer == "" {
		if os.Getenv("TABOO_REQUIRE_KEYCLOAK") == "1" {
			t.Fatal("Keycloak issuer required")
		}
		t.Skip("run scripts/test-federation.sh for real Keycloak interop")
	}
	f := fixture(t, issuer)
	w := f.request("GET", "/api/v1/auth/oidc/a/login", nil, false)
	checkStatus(t, w, 302)
	flow := cookieOf(t, w, "taboo.oidc.flow")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if strings.HasPrefix(r.URL.String(), fixtureOrigin+"/") {
			return http.ErrUseLastResponse
		}
		if len(via) > 5 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	res, e := client.Get(w.Header().Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if e != nil || res.StatusCode != 200 {
		t.Fatalf("Keycloak authorization failed: %d %v", res.StatusCode, e)
	}
	match := regexp.MustCompile(`<form[^>]+action="([^"]+)"`).FindSubmatch(body)
	if len(match) != 2 {
		t.Fatal("Keycloak login form missing")
	}
	action := html.UnescapeString(string(match[1]))
	res, e = client.PostForm(action, url.Values{"username": {"fixture-user"}, "password": {"fixture-password"}, "credentialId": {""}})
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 302 || !strings.HasPrefix(res.Header.Get("Location"), fixtureOrigin+"/api/v1/auth/oidc/a/callback?") {
		t.Fatalf("Keycloak did not redirect to registered callback: %d", res.StatusCode)
	}
	callback, e := url.Parse(res.Header.Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	w = f.request("GET", callback.RequestURI(), flow, false)
	checkStatus(t, w, 303)
	session := cookieOf(t, w, auth.BrowserCookie)
	checkStatus(t, f.request("GET", "/api/v1/me", session, false), 200)
	checkStatus(t, f.request("GET", "/api/v1/orgs/b/projects", session, false), 403)
	checkStatus(t, f.request("GET", callback.RequestURI(), flow, false), 400)
	checkStatus(t, f.request("POST", "/api/v1/auth/logout", session, true), 204)
	checkStatus(t, f.request("GET", "/api/v1/me", session, false), 401)
}

// Keycloak is tested through introspection rather than relabeling its ordinary
// JWT format as RFC 9068. No vendor-specific subject or role claim is consumed.
func TestKeycloakMachineInterop(t *testing.T) {
	issuer := os.Getenv("TABOO_TEST_KEYCLOAK_ISSUER")
	if issuer == "" {
		if os.Getenv("TABOO_REQUIRE_KEYCLOAK") == "1" {
			t.Fatal("Keycloak issuer required")
		}
		t.Skip("run scripts/test-federation.sh")
	}
	f := fixture(t, issuer)
	p, err := oauth.Discover(context.Background(), issuer, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"taboo.read taboo.write taboo.reveal"}}
	req, _ := http.NewRequest("POST", p.TokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("taboo-machine", "fixture-machine-secret")
	res, err := oauth.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var response struct {
		Access string `json:"access_token"`
	}
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&response) != nil || response.Access == "" {
		t.Fatalf("Keycloak machine grant failed: %d", res.StatusCode)
	}
	verified, err := p.Introspect(context.Background(), response.Access, "https://taboo.example/api", "taboo-resource", "fixture-resource-secret")
	if err != nil {
		t.Fatalf("Keycloak introspection: %v", err)
	}
	if verified.ClientID != "taboo-machine" || verified.Subject == "" || !verified.HasScope("taboo.read") {
		t.Fatal("standard machine claims missing")
	}
	body, _ := json.Marshal(map[string]any{"name": "Keycloak OAuth", "issuer": issuer, "audience": "https://taboo.example/api", "metadata_mode": "oidc", "token_profile": "INTROSPECTION_RFC7662", "client_id": "taboo-resource", "client_secret": "fixture-resource-secret"})
	w := f.localRequest("POST", "/api/v1/orgs/a/oauth-trusts", string(body), "")
	checkStatus(t, w, 201)
	var id struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &id)
	trust := id.ID
	body, _ = json.Marshal(map[string]any{"name": "Keycloak machine", "trust_id": trust, "subject": verified.Subject, "client_id": verified.ClientID, "grants": []map[string]string{{"project_id": "project-a", "environment_id": "env-a", "action": "read", "required_scope": "taboo.read"}}})
	w = f.localRequest("POST", "/api/v1/orgs/a/workloads", string(body), "")
	checkStatus(t, w, 201)
	json.Unmarshal(w.Body.Bytes(), &id)
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/projects/project-a/secrets?env=dev", nil)
		r.Header.Set("Authorization", "Bearer "+response.Access)
		r.Header.Set("X-Taboo-Trust", trust)
		r.Header.Set("X-Taboo-Organization", "a")
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		return w
	}
	checkStatus(t, call(), 200)
	checkStatus(t, f.localRequest("POST", "/api/v1/orgs/a/workloads/"+id.ID+"/revoke", "", ""), 200)
	checkStatus(t, call(), 401)
}
