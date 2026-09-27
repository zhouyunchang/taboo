package server

import (
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
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
