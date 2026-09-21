package audit

import (
	"testing"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func TestHashChainAndTamper(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	org := "org-1"
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	if err := Record(db, Rec{OrgID: org, ActorID: "u", Action: "a", Resource: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(db, Rec{OrgID: org, ActorID: "u", Action: "b", Resource: "r2"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteCheckpoint(db, key, org); err != nil {
		t.Fatal(err)
	}
	v, err := Verify(db, org, key)
	if err != nil || !v.OK {
		rows, _ := db.Query(`SELECT id, prev_hash, entry_hash, created_at, action FROM audit_logs WHERE org_id = ? ORDER BY rowid`, org)
		for rows.Next() {
			var id, prev, entry, created, action string
			_ = rows.Scan(&id, &prev, &entry, &created, &action)
			t.Logf("row %s action=%s created=%q prev=%s entry=%s", id, action, created, prev[:8], entry[:8])
		}
		rows.Close()
		t.Fatalf("verify: %+v %v", v, err)
	}
	_, _ = db.Exec(`UPDATE audit_logs SET action = 'tampered' WHERE action = 'b'`)
	v, err = Verify(db, org, key)
	if err != nil {
		t.Fatal(err)
	}
	if v.OK {
		t.Fatal("tamper must fail verify")
	}
	_ = tc.NewID()
}
