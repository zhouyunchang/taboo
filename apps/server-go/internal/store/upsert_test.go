package store

import "testing"

func TestPgUpsertAddsSetColumnForAuditHeads(t *testing.T) {
	sqlite := `INSERT INTO audit_heads(org_id,revision) VALUES($1,1) ON CONFLICT(org_id) DO UPDATE SET revision=audit_heads.revision+1`
	want := `INSERT INTO audit_heads(org_id,revision) VALUES($1,1) ON CONFLICT(org_id) DO UPDATE SET revision = audit_heads.revision+1`
	if got := pgUpsert(sqlite); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestPgUpsertLeavesOtherSQLAlone(t *testing.T) {
	q := `UPDATE audit_heads SET revision=revision+1 WHERE org_id=$1`
	if got := pgUpsert(q); got != q {
		t.Fatalf("rewrote an unrelated statement: %s", got)
	}
}
