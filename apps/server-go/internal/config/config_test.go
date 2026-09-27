package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingRootNeverReplaced(t *testing.T) {
	dir := t.TempDir()
	key, err := loadOrCreateMasterKey(dir)
	if err != nil || len(key) != 64 {
		t.Fatalf("fresh: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "taboo.db"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	retained, err := loadOrCreateMasterKey(dir)
	if err != nil || retained != key {
		t.Fatal("existing root changed")
	}
	if err = os.Remove(filepath.Join(dir, "master.key")); err != nil {
		t.Fatal(err)
	}
	if _, err = loadOrCreateMasterKey(dir); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err = os.Stat(filepath.Join(dir, "master.key")); !os.IsNotExist(err) {
		t.Fatal("replacement root written")
	}
}
