package org_test

import (
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestBackfillPersonalSpaceLeavesSharedKeys(t *testing.T) {
	db := testdb.Open(t)
	key := make([]byte, 32)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,email,password_hash,name) VALUES('owner','owner@example.test','!','Owner'),('peer','peer@example.test','!','Peer')`)
	exec(`INSERT INTO orgs(id,name,slug,dek_encrypted,created_at) VALUES('team','Team','team','fixture','2020-01-01 00:00:00')`)
	exec(`INSERT INTO org_members(org_id,user_id,role) VALUES('team','owner','owner'),('team','peer','developer')`)
	exec(`INSERT INTO projects(id,org_id,name,slug) VALUES('proj','team','api','api')`)
	if err := store.ClassifyTenants(db); err != nil {
		t.Fatal(err)
	}
	var teamKind string
	if err := db.QueryRow(`SELECT kind FROM orgs WHERE id='team'`).Scan(&teamKind); err != nil || teamKind != org.KindOrganization {
		t.Fatalf("shared org kind %s %v", teamKind, err)
	}
	if err := org.BackfillPersonalSpaces(db, key); err != nil {
		t.Fatal(err)
	}
	if err := org.BackfillPersonalSpaces(db, key); err != nil {
		t.Fatal(err)
	}
	var personal, projects, teamProject int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orgs WHERE kind='personal' AND owner_user_id='owner'`).Scan(&personal); err != nil || personal != 1 {
		t.Fatalf("owner personal spaces %d %v", personal, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM orgs WHERE kind='personal' AND owner_user_id='peer'`).Scan(&personal); err != nil || personal != 1 {
		t.Fatalf("peer personal spaces %d %v", personal, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects p JOIN orgs o ON o.id=p.org_id WHERE o.owner_user_id='owner' AND o.kind='personal'`).Scan(&projects); err != nil || projects != 1 {
		t.Fatalf("personal projects %d %v", projects, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id='proj' AND org_id='team'`).Scan(&teamProject); err != nil || teamProject != 1 {
		t.Fatal("shared project moved")
	}
}
