package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testoauth"
)

func TestExternalWorkloadProfilesAndLocalRevocation(t *testing.T) {
	for _, profile := range []string{"JWT_RFC9068", "INTROSPECTION_RFC7662"} {
		t.Run(profile, func(t *testing.T) {
			f := fixture(t, "")
			op := testoauth.New(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.h = New(&Deps{DB: f.db, MasterKey: f.master, JWTSecret: "fixture-jwt", DEKs: tc.NewDEKCache(), IssuerAllowlist: []string{op.Server.URL}, WorkerCtx: ctx})
			dek, _ := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
			f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
			b, _ := json.Marshal(map[string]any{"name": "standard AS", "issuer": op.Server.URL, "audience": "https://taboo.example/api", "metadata_mode": "oauth", "token_profile": profile, "client_id": "resource-server", "client_secret": "fixture-resource-secret"})
			w := f.localRequest("POST", "/api/v1/orgs/a/oauth-trusts", string(b), "")
			checkStatus(t, w, 201)
			var created struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(w.Body.Bytes(), &created) != nil {
				t.Fatal("invalid response")
			}
			trust := created.ID
			machine := "machine-jwt"
			if profile == "INTROSPECTION_RFC7662" {
				machine = "machine-opaque"
			}
			grants := []map[string]string{{"project_id": "project-a", "environment_id": "env-a", "action": "read", "required_scope": "read"}, {"project_id": "project-a", "environment_id": "env-a", "action": "write", "required_scope": "write"}}
			b, _ = json.Marshal(map[string]any{"name": "automation", "trust_id": trust, "subject": "service:" + machine, "client_id": machine, "grants": grants})
			w = f.localRequest("POST", "/api/v1/orgs/a/workloads", string(b), "")
			checkStatus(t, w, 201)
			json.Unmarshal(w.Body.Bytes(), &created)
			binding := created.ID
			getToken := func(scope string) string {
				t.Helper()
				form := url.Values{"grant_type": {"client_credentials"}, "scope": {scope}, "resource": {"https://taboo.example/api"}}
				req, _ := http.NewRequest("POST", op.Server.URL+"/token", strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.SetBasicAuth(machine, "fixture-machine-secret")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				if res.StatusCode != 200 {
					t.Fatal("machine grant rejected")
				}
				var token struct {
					Access string `json:"access_token"`
				}
				if json.NewDecoder(res.Body).Decode(&token) != nil || token.Access == "" {
					t.Fatal("machine token missing")
				}
				return token.Access
			}
			request := func(method, path, body, token, org string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("X-Taboo-Trust", trust)
				r.Header.Set("X-Taboo-Organization", org)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				f.h.ServeHTTP(w, r)
				return w
			}
			token := getToken("read write reveal export")
			checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "a"), 200)
			checkStatus(t, request("POST", "/api/v1/projects/project-a/secrets?env=dev", `{"key":"KEY","value":"fixture"}`, token, "a"), 200)
			// A write binding never implies reveal/export, even if the token has those scopes.
			checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets/KEY?env=dev", "", token, "a"), 403)
			checkStatus(t, request("GET", "/api/v1/projects/project-a/export?env=dev", "", token, "a"), 403)
			checkStatus(t, request("GET", "/api/v1/projects/project-b/secrets?env=dev", "", token, "a"), 403)
			checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "b"), 401)
			checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", getToken("write"), "a"), 403)
			grants = append(grants, map[string]string{"project_id": "project-a", "environment_id": "env-a", "action": "reveal", "required_scope": "reveal"})
			b, _ = json.Marshal(map[string]any{"grants": grants})
			checkStatus(t, f.localRequest("PUT", "/api/v1/orgs/a/workloads/"+binding+"/grants", string(b), ""), 200)
			checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets/KEY?env=dev", "", token, "a"), 200)
			// Provider changes cannot silently reinterpret bound subjects from a new issuer.
			checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/oauth-trusts/"+trust, `{"issuer":"https://different.example"}`, ""), 409)
			if profile == "INTROSPECTION_RFC7662" {
				op.Disable()
				checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "a"), 401)
				op.Outage()
				checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "a"), 503)
			} else {
				op.Disable()
				checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "a"), 200)
			}
			// Local binding revocation rejects already-issued offline JWTs immediately.
			checkStatus(t, f.localRequest("POST", "/api/v1/orgs/a/workloads/"+binding+"/revoke", "", ""), 200)
			if profile == "JWT_RFC9068" {
				checkStatus(t, request("GET", "/api/v1/projects/project-a/secrets?env=dev", "", token, "a"), 401)
			}
			testdb.FailAudit(t, f.db)
			checkStatus(t, f.localRequest("PATCH", "/api/v1/orgs/a/oauth-trusts/"+trust, `{"enabled":false}`, ""), 503)
			var enabled int
			if err := f.db.QueryRow(`SELECT enabled FROM oauth_trusts WHERE id=?`, trust).Scan(&enabled); err != nil || enabled != 1 {
				t.Fatal("unaudited trust mutation persisted")
			}
		})
	}
}
