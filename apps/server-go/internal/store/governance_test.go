package store_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestSchema12GovernanceUpgradePreservesWorkloads(t *testing.T) {
	db := testdb.Open(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO users(id,email,password_hash) VALUES('u','u@test.example','!')`,
		`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a','fixture')`,
		`INSERT INTO org_members(org_id,user_id,role) VALUES('a','u','owner')`,
		`INSERT INTO projects(id,org_id,name,slug) VALUES('p','a','p','p')`,
		`INSERT INTO environments(id,project_id,name,slug) VALUES('e','p','dev','dev')`,
		`INSERT INTO oauth_trusts(id,org_id,name,issuer,audience,metadata_mode,token_profile) VALUES('t','a','fixture','https://op.example','api','oauth','JWT_RFC9068')`,
		`INSERT INTO machine_identities(id,org_id,name,auth_type,client_id,secret_hash) VALUES('w','a','fixture','external_oauth','external:w','!')`,
		`INSERT INTO workload_bindings(id,org_id,trust_id,subject,client_id) VALUES('w','a','t','subject','client')`,
		`DROP TABLE member_action_grants`, `DROP TABLE org_security`, `DROP TABLE browser_credentials`, `DROP TABLE provider_security`,
	} {
		exec(q)
	}
	var reopen func() (*sql.DB, error)
	if store.IsPostgres(db) {
		exec(`ALTER TABLE oidc_providers DROP CONSTRAINT provider_id_org; ALTER TABLE browser_sessions DROP CONSTRAINT browser_token_org`)
		exec(`ALTER TABLE workload_grants DROP CONSTRAINT workload_grants_action_check; ALTER TABLE workload_grants ADD CONSTRAINT workload_grants_action_check CHECK(action IN ('read','reveal','write','export')); UPDATE schema_version SET version=12`)
		var schema string
		if db.QueryRow(`SELECT current_schema()`).Scan(&schema) != nil {
			t.Fatal("missing fixture schema")
		}
		u, err := url.Parse(os.Getenv("TABOO_TEST_POSTGRES_DSN"))
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		reopen = func() (*sql.DB, error) { return store.OpenPostgres(context.Background(), u.String(), "taboo_runtime") }
	} else {
		var ddl string
		if db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='workload_grants'`).Scan(&ddl) != nil {
			t.Fatal("missing workload schema")
		}
		exec(`DROP TABLE workload_grants`)
		exec(strings.ReplaceAll(ddl, ",'rollback','delete'", ""))
		exec(`PRAGMA user_version=12`)
		var sequence int
		var name, path string
		if db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path) != nil {
			t.Fatal("missing fixture database")
		}
		reopen = func() (*sql.DB, error) { return store.Open(filepath.Dir(path)) }
	}
	exec(`INSERT INTO workload_grants(binding_id,org_id,project_id,env_id,action,required_scope) VALUES('w','a','p','e','write','write')`)
	upgraded, err := reopen()
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if v, err := store.Version(upgraded); err != nil || v != store.SchemaVersion {
		t.Fatal("schema not upgraded", v, err)
	}
	var scope string
	if upgraded.QueryRow(`SELECT required_scope FROM workload_grants WHERE action='write'`).Scan(&scope) != nil || scope != "write" {
		t.Fatal("existing workload lost")
	}
	if _, err = upgraded.Exec(`INSERT INTO workload_grants(binding_id,org_id,project_id,env_id,action,required_scope) VALUES('w','a','p','e','rollback','rollback')`); err != nil {
		t.Fatal(err)
	}
	if _, err = upgraded.Exec(`INSERT INTO member_action_grants(org_id,user_id,project_id,env_id,action) VALUES('a','u','p','e','reveal')`); err != nil {
		t.Fatal(err)
	}
	second, err := reopen()
	if err != nil {
		t.Fatal("upgrade replay", err)
	}
	second.Close()
}
