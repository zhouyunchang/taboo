package store_test

import (
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestCollapseKeepsProdValueAndDevOnlyKeys(t *testing.T) {
	db := testdb.Open(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('o','O','o','x')`)
	exec(`INSERT INTO projects(id,org_id,name,slug) VALUES('p','o','api','api')`)
	exec(`INSERT INTO environments(id,project_id,name,slug,approval_required) VALUES('dev','p','dev','dev',0),('prod','p','prod','prod',1)`)
	exec(`INSERT INTO folders(id,env_id,name,path) VALUES('fd','dev','/','/'),('fp','prod','/','/')`)
	exec(`INSERT INTO secrets(id,env_id,folder,folder_id,key,latest_version) VALUES('sd','dev','/','fd','SHARED',1),('sp','prod','/','fp','SHARED',4),('only','dev','/','fd','ONLY_DEV',1)`)
	exec(`INSERT INTO secret_versions(id,secret_id,version,ciphertext,created_by) VALUES('vd','sd',1,'dev-value','u'),('vp','sp',4,'prod-value','u'),('vo','only',1,'dev-only','u')`)
	if err := store.CollapseEnvironments(db); err != nil {
		t.Fatal(err)
	}
	var envs int
	var slug string
	if err := db.QueryRow(`SELECT COUNT(*), MIN(slug) FROM environments WHERE project_id='p'`).Scan(&envs, &slug); err != nil || envs != 1 || slug != "dev" {
		t.Fatalf("envs %d slug %s err %v", envs, slug, err)
	}
	var approval int
	if err := db.QueryRow(`SELECT approval_required FROM projects WHERE id='p'`).Scan(&approval); err != nil || approval != 1 {
		t.Fatalf("approval %d %v", approval, err)
	}
	value := func(key string) string {
		t.Helper()
		var ciphertext string
		if err := db.QueryRow(`SELECT v.ciphertext FROM secrets s JOIN secret_versions v ON v.secret_id=s.id AND v.version=s.latest_version JOIN environments e ON e.id=s.env_id WHERE e.project_id='p' AND s.key=?`, key).Scan(&ciphertext); err != nil {
			t.Fatal(err)
		}
		return ciphertext
	}
	if value("SHARED") != "prod-value" || value("ONLY_DEV") != "dev-only" {
		t.Fatal("merged secret values are wrong")
	}
}
