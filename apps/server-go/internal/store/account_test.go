package store_test

import (
	"path/filepath"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestClassifyTenantsSplitsSoleOwnersOnce(t *testing.T) {
	db := testdb.Open(t)
	if v, err := store.Version(db); err != nil || v != store.ExpectedVersion(db) {
		t.Fatalf("schema %d %v", v, err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,email,password_hash,name) VALUES('u1','u1@example.test','!','One'),('u2','u2@example.test','!','Two'),('u3','u3@example.test','!','Three')`)
	exec(`INSERT INTO orgs(id,name,slug,dek_encrypted,created_at) VALUES('old','Old','old','x','2020-01-01 00:00:00'),('newer','Newer','newer','x','2021-01-01 00:00:00'),('shared','Shared','shared','x','2019-01-01 00:00:00'),('viewer','Viewer','viewer','x','2018-01-01 00:00:00')`)
	exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('old','u1','owner'),('newer','u1','owner'),('shared','u1','owner'),('shared','u2','developer'),('viewer','u3','viewer')`)
	if err := store.ClassifyTenants(db); err != nil {
		t.Fatal(err)
	}
	kindOf := func(id string) (string, string) {
		t.Helper()
		var kind, owner string
		if err := db.QueryRow(`SELECT kind, COALESCE(owner_user_id,'') FROM orgs WHERE id=?`, id).Scan(&kind, &owner); err != nil {
			t.Fatal(err)
		}
		return kind, owner
	}
	if kind, owner := kindOf("old"); kind != "personal" || owner != "u1" {
		t.Fatalf("oldest sole org = %s %s", kind, owner)
	}
	if kind, owner := kindOf("newer"); kind != "organization" || owner != "" {
		t.Fatalf("later sole org = %s %s", kind, owner)
	}
	if kind, _ := kindOf("shared"); kind != "organization" {
		t.Fatalf("shared org = %s", kind)
	}
	if kind, _ := kindOf("viewer"); kind != "organization" {
		t.Fatalf("non-owner sole org = %s", kind)
	}
	exec(`INSERT INTO orgs(id,name,slug,dek_encrypted,created_at) VALUES('after','After','after','x','2022-01-01 00:00:00')`)
	exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('after','u1','owner')`)
	if err := store.ClassifyTenants(db); err != nil {
		t.Fatal(err)
	}
	if kind, _ := kindOf("after"); kind != "organization" {
		t.Fatal("classification converted an organization created after the user already had a personal space")
	}
	if kind, _ := kindOf("newer"); kind != "organization" {
		t.Fatal("second classification moved another sole-owner organization")
	}

	if store.IsPostgres(db) {
		return
	}
	exec(`INSERT INTO users(id,email,password_hash) VALUES('fresh','fresh@example.test','!')`)
	exec(`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('fresh-org','Fresh','fresh','x')`)
	exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('fresh-org','fresh','owner')`)
	var sequence int
	var name, path string
	if db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path) != nil {
		t.Fatal("missing fixture database")
	}
	reopened, err := store.Open(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var kind string
	if err = reopened.QueryRow(`SELECT kind FROM orgs WHERE id='fresh-org'`).Scan(&kind); err != nil || kind != "organization" {
		t.Fatalf("startup reclassified a new organization: %s %v", kind, err)
	}
}
