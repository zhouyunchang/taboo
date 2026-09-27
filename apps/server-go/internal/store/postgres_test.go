package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestPostgresTenantIsolation(t *testing.T) {
	db := testdb.Open(t)
	if !store.IsPostgres(db) {
		t.Skip("PostgreSQL RLS integration runs in required database CI job")
	}
	for _, id := range []string{"a", "b"} {
		for _, q := range []string{
			`INSERT INTO users(id,email,password_hash) VALUES('%[1]s','%[1]s@test','!')`,
			`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('%[1]s','%[1]s','%[1]s','fixture')`,
			`INSERT INTO org_members(org_id,user_id,role) VALUES('%[1]s','%[1]s','owner')`,
			`INSERT INTO projects(id,org_id,name,slug) VALUES('%[1]s','%[1]s','%[1]s','%[1]s')`,
			`INSERT INTO environments(id,project_id,name,slug) VALUES('%[1]s','%[1]s','dev','dev')`,
			`INSERT INTO folders(id,env_id,name,path) VALUES('%[1]s','%[1]s','/','/')`,
			`INSERT INTO secrets(id,env_id,folder_id,key) VALUES('%[1]s','%[1]s','%[1]s','key')`,
		} {
			if _, err := db.Exec(fmt.Sprintf(q, id)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 12; i++ {
		org := []string{"a", "b"}[i%2]
		scope, closeScope, err := store.Scoped(context.Background(), db, org, org)
		if err != nil {
			t.Fatal(err)
		}
		var id, role string
		if err = scope.QueryRow(`SELECT id FROM secrets`).Scan(&id); err != nil || id != org {
			t.Fatalf("scope %s: %s %v", org, id, err)
		}
		if err = scope.QueryRow(`SELECT current_user`).Scan(&role); err != nil || role != "taboo_runtime" {
			t.Fatalf("runtime role: %s %v", role, err)
		}
		if _, err = scope.Exec(`INSERT INTO secrets(id,env_id,folder_id,key) VALUES('bad',? ,?,'bad')`, []string{"b", "a"}[i%2], []string{"b", "a"}[i%2]); err == nil {
			t.Fatal("cross tenant insert succeeded")
		}
		res, err := scope.Exec(`DELETE FROM secrets WHERE id<>?`, org)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := res.RowsAffected()
		if n != 0 {
			t.Fatal("cross tenant delete")
		}
		closeScope()
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled, closeCanceled, err := store.Scoped(ctx, db, "a", "a")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = canceled.Exec(`SELECT pg_sleep(30)`); err == nil {
		t.Fatal("canceled request reached storage")
	}
	closeCanceled()
	// Missing request scope cannot see tenant data; control pool scope never leaks.
	empty, closeEmpty, err := store.Scoped(context.Background(), db, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeEmpty()
	var n int
	if err = empty.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unscoped runtime: %d %v", n, err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM secrets`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("control pool changed: %d %v", n, err)
	}
	// Composite constraints also protect trusted jobs and import tools.
	if _, err = db.Exec(`INSERT INTO secrets(id,env_id,folder_id,key) VALUES('bad','a','b','bad')`); err == nil {
		t.Fatal("cross environment folder accepted")
	}
	if _, err = db.Exec(`INSERT INTO project_grants(org_id,project_id,user_id,role) VALUES('a','b','a','viewer')`); err == nil {
		t.Fatal("cross organization grant accepted")
	}
	// Reopening/migrating the same database does not weaken FORCE RLS.
	if err = db.QueryRow(`SELECT COUNT(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND relname IN ('secrets','secret_versions','projects') AND relrowsecurity AND relforcerowsecurity`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("FORCE RLS missing: %d %v", n, err)
	}
}

func TestConcurrentAuditChain(t *testing.T) {
	db := testdb.Open(t)
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := audit.Record(db, audit.Rec{OrgID: "a", ActorID: "fixture", Action: "fixture", Resource: "fixture"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	result, err := audit.Verify(db, "a", make([]byte, 32))
	if err != nil || !result.OK || result.Checked != 24 {
		t.Fatalf("concurrent audit %+v %v", result, err)
	}
}
