package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testoauth"
)

func TestConsumerProcesses(t *testing.T) {
	if os.Getenv("TABOO_TEST_CONSUMERS") != "1" {
		t.Skip("run scripts/test-consumers.sh (required public CI job)")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "taboo-mcp")
	build := exec.Command("go", "build", "-race", "-o", bin, ".")
	build.Dir = filepath.Join(root, "apps/mcp")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("MCP build: %v\n%s", err, out)
	}
	for _, profile := range []string{"JWT_RFC9068", "INTROSPECTION_RFC7662", "local"} {
		t.Run(profile, func(t *testing.T) {
			f := fixture(t, "")
			op := testoauth.New(t)
			other := testoauth.New(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.h = New(&Deps{DB: f.db, MasterKey: f.master, JWTSecret: "fixture-jwt", DEKs: tc.NewDEKCache(), IssuerAllowlist: []string{op.Server.URL}, WorkerCtx: ctx})
			dek, err := tc.Seal(f.master, hex.EncodeToString(make([]byte, 32)), tc.DEKContext("a"))
			if err != nil {
				t.Fatal(err)
			}
			f.exec(`UPDATE orgs SET dek_encrypted=? WHERE id='a'`, dek)
			machine, secret, trust, revoke, mode := "machine-jwt", "fixture-machine-secret", "", "", "oauth"
			if profile == "INTROSPECTION_RFC7662" {
				machine = "machine-opaque"
			}
			create := func(path string, body any) map[string]string {
				t.Helper()
				b, _ := json.Marshal(body)
				w := f.localRequest("POST", path, string(b), "")
				checkStatus(t, w, 201)
				var out map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				result := map[string]string{}
				for _, key := range []string{"id", "client_id", "client_secret"} {
					var value string
					_ = json.Unmarshal(out[key], &value)
					result[key] = value
				}
				return result
			}
			if profile == "local" {
				mode = "local"
				created := create("/api/v1/orgs/a/identities", map[string]any{"name": "local compatibility", "scopes": []map[string]string{{"project_id": "project-a", "env": "dev", "permission": "write"}}})
				machine, secret = created["client_id"], created["client_secret"]
				revoke = "/api/v1/orgs/a/identities/" + created["id"] + "/revoke"
			} else {
				created := create("/api/v1/orgs/a/oauth-trusts", map[string]any{"name": "public AS", "issuer": op.Server.URL, "audience": "https://taboo.example/api", "metadata_mode": "oauth", "token_profile": profile, "client_id": "resource-server", "client_secret": "fixture-resource-secret"})
				trust = created["id"]
				grants := []map[string]string{}
				for _, action := range []string{"read", "write", "reveal", "export"} {
					grants = append(grants, map[string]string{"project_id": "project-a", "environment_id": "env-a", "action": action, "required_scope": action})
				}
				created = create("/api/v1/orgs/a/workloads", map[string]any{"name": "public consumer", "trust_id": trust, "subject": "service:" + machine, "client_id": machine, "grants": grants})
				revoke = "/api/v1/orgs/a/workloads/" + created["id"] + "/revoke"
			}
			server := httptest.NewServer(f.h)
			defer server.Close()
			token, err := tc.SignJWT("local", "collision@example.test", "fixture-jwt", 15*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			vars := []string{}
			for _, v := range os.Environ() {
				if !strings.HasPrefix(v, "TABOO_") {
					vars = append(vars, v)
				}
			}
			vars = append(vars, "TABOO_TEST_CONSUMER=1", "TABOO_SERVER="+server.URL, "TABOO_AUTH_MODE="+mode, "TABOO_CLIENT_ID="+machine, "TABOO_CLIENT_SECRET="+secret, "TABOO_PROJECT_ID=project-a", "TABOO_ENV=dev", "TABOO_ORG=a", "TABOO_SESSION_FILE="+filepath.Join(t.TempDir(), "session.json"), "TABOO_TEST_MCP_BIN="+bin, "TABOO_TEST_OWNER_TOKEN="+token, "TABOO_TEST_REVOKE_PATH="+revoke)
			if mode == "oauth" {
				vars = append(vars, "TABOO_OAUTH_ISSUER="+op.Server.URL, "TABOO_OAUTH_METADATA_MODE=oauth", "TABOO_OAUTH_TRUST_ID="+trust, "TABOO_OAUTH_SCOPES=read write reveal export", "TABOO_OAUTH_RESOURCE=https://taboo.example/api", "TABOO_TEST_OTHER_ISSUER="+other.Server.URL)
			}
			run := func(dir, name string, args ...string) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, name, args...)
				cmd.Dir = dir
				cmd.Env = vars
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("consumer %s failed: %v\n%s", name, err, out)
				}
			}
			run(filepath.Join(root, "packages/sdk-go"), "go", "test", "-race", "-run", "^TestConsumerAPI$", "-count=1", ".")
			run(root, "node", "tests/federation/consumers.mjs")
		})
	}
}
