package auth

import (
	"os"
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func setupRBAC(t *testing.T) (cleanup func(), can func(who, action, env string) bool) {
	t.Helper()
	dir := t.TempDir()
	dbConn, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	org, proj, envDev, envProd := tc.NewID(), tc.NewID(), tc.NewID(), tc.NewID()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	mustExec := func(q string, args ...any) { _, err := dbConn.Exec(q, args...); must(err) }
	mustExec(`INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, 'o', 'o', 'x')`, org)
	mustExec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'p', 'p')`, proj, org)
	mustExec(`INSERT INTO environments (id, project_id, name, slug, sort_order, protected) VALUES (?, ?, 'dev', 'dev', 0, 0)`, envDev, proj)
	mustExec(`INSERT INTO environments (id, project_id, name, slug, sort_order, protected) VALUES (?, ?, 'prod', 'prod', 1, 1)`, envProd, proj)

	mkUser := func(role string) *Actor {
		id := tc.NewID()
		mustExec(`INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, 'x')`, id, id+"@t.test", role)
		mustExec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)`, org, id, role)
		return &Actor{ID: id, Kind: KindUser, Name: role}
	}
	viewer, dev, admin, owner := mkUser("viewer"), mkUser("developer"), mkUser("admin"), mkUser("owner")

	ident := tc.NewID()
	mustExec(`INSERT INTO machine_identities (id, org_id, name, auth_type, client_id, secret_hash, token_ttl)
		VALUES (?, ?, 'ci', 'client_credentials', 'mi_x', 'h', 900)`, ident, org)
	mustExec(`INSERT INTO identity_scopes (identity_id, project_id, env_id, permission) VALUES (?, ?, ?, 'read')`, ident, proj, envDev)
	mustExec(`INSERT INTO identity_scopes (identity_id, project_id, env_id, permission) VALUES (?, ?, ?, 'reveal')`, ident, proj, envProd)
	idActor := &Actor{ID: ident, Kind: KindIdentity, Name: "ci"}

	return func() { _ = dbConn.Close(); _ = os.RemoveAll(dir) }, func(who, action, env string) bool {
		var a *Actor
		switch who {
		case "viewer":
			a = viewer
		case "developer":
			a = dev
		case "admin":
			a = admin
		case "owner":
			a = owner
		case "identity":
			a = idActor
		}
		return Can(dbConn, a, org, proj, action, env)
	}
}

func TestRoleEnvMatrix(t *testing.T) {
	cleanup, can := setupRBAC(t)
	defer cleanup()
	cases := []struct {
		who, action, env string
		want             bool
	}{
		{"viewer", "read", "prod", true},
		{"viewer", "reveal", "dev", false},
		{"viewer", "write", "dev", false},
		{"viewer", "audit.read", "", false},
		{"developer", "reveal", "dev", true},
		{"developer", "write", "dev", true},
		{"developer", "reveal", "prod", false},
		{"developer", "write", "prod", false},
		{"developer", "admin", "", false},
		{"admin", "reveal", "prod", true},
		{"admin", "write", "prod", true},
		{"admin", "admin", "", true},
		{"admin", "members", "", false},
		{"admin", "audit.read", "", true},
		{"owner", "members", "", true},
		{"identity", "read", "dev", true},
		{"identity", "reveal", "dev", false},
		{"identity", "reveal", "prod", true},
		{"identity", "write", "prod", false},
		{"identity", "admin", "", false},
	}
	for _, c := range cases {
		if got := can(c.who, c.action, c.env); got != c.want {
			t.Fatalf("%s %s env=%s: got %v want %v", c.who, c.action, c.env, got, c.want)
		}
	}
}

func TestRestrictedGrant(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	org, p1, p2 := tc.NewID(), tc.NewID(), tc.NewID()
	e1 := tc.NewID()
	_, _ = db.Exec(`INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, 'o', 'o', 'x')`, org)
	_, _ = db.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'a', 'a')`, p1, org)
	_, _ = db.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'b', 'b')`, p2, org)
	_, _ = db.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order, protected) VALUES (?, ?, 'dev', 'dev', 0, 0)`, e1, p1)
	uid := tc.NewID()
	_, _ = db.Exec(`INSERT INTO users (id, email, name, password_hash) VALUES (?, 'r@t.test', 'r', 'x')`, uid)
	_, _ = db.Exec(`INSERT INTO org_members (org_id, user_id, role, restricted) VALUES (?, ?, 'developer', 1)`, org, uid)
	a := &Actor{ID: uid, Kind: KindUser}
	if Can(db, a, org, p1, "read", "dev") {
		t.Fatal("restricted without grant must deny")
	}
	_, _ = db.Exec(`INSERT INTO project_grants (org_id, project_id, user_id, role) VALUES (?, ?, ?, 'viewer')`, org, p1, uid)
	if !Can(db, a, org, p1, "read", "dev") {
		t.Fatal("grant should allow read")
	}
	if Can(db, a, org, p1, "write", "dev") {
		t.Fatal("viewer grant cannot write")
	}
}
