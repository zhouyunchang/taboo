package cluster

import (
	"testing"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func TestLeaseSingleHolder(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ok, err := Acquire(db, "sweep", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first acquire %v %v", ok, err)
	}
	ok, err = Acquire(db, "sweep", time.Minute)
	if err != nil || !ok {
		t.Fatalf("refresh %v %v", ok, err)
	}
	if _, err = db.Exec(`UPDATE cluster_leases SET holder='other', lease_until=? WHERE name='sweep'`, time.Now().Unix()+60); err != nil {
		t.Fatal(err)
	}
	ok, err = Acquire(db, "sweep", time.Minute)
	if err != nil || ok {
		t.Fatalf("foreign lease taken: %v %v", ok, err)
	}
	if _, err = db.Exec(`UPDATE cluster_leases SET lease_until=0 WHERE name='sweep'`); err != nil {
		t.Fatal(err)
	}
	ok, err = Acquire(db, "sweep", time.Minute)
	if err != nil || !ok {
		t.Fatalf("expired lease %v %v", ok, err)
	}
}
