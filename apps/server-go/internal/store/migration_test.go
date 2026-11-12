package store_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/testdb"
)

func TestLegacyEnvelopeUpgradeBackupAndPostgresTransfer(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if err := os.WriteFile(filepath.Join(dir, "master.key"), []byte(hex.EncodeToString(key)), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dek, _ := tc.Encrypt(key, hex.EncodeToString(key))
	value, _ := tc.Encrypt(key, "fixture secret")
	queries := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO orgs(id,name,slug,dek_encrypted) VALUES('a','a','a',?)`, []any{dek}},
		{`INSERT INTO projects(id,org_id,name,slug) VALUES('p','a','p','p')`, nil},
		{`INSERT INTO folders(id,project_id,name,path) VALUES('f','p','/','/')`, nil},
		{`INSERT INTO secrets(id,project_id,folder_id,key,latest_version) VALUES('s','p','f','K',1)`, nil},
		{`INSERT INTO secret_versions(id,secret_id,version,ciphertext,created_by) VALUES('v','s',1,?,'fixture')`, []any{value}},
	}
	for _, q := range queries {
		if _, err = db.Exec(q.q, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = audit.Record(db, audit.Rec{OrgID: "a", Action: "fixture", ActorID: "fixture", Resource: "s"}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup")
	if err = store.Backup(dir, backup); err != nil {
		t.Fatal(err)
	}
	wrong := make([]byte, 32)
	wrong[0] = 1
	if _, err = store.UpgradeEnvelopes(db, wrong, true); err == nil {
		t.Fatal("wrong key migration succeeded")
	}
	if _, err = db.Exec(`UPDATE secret_versions SET ciphertext='damaged'`); err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpgradeEnvelopes(db, key, true); err == nil {
		t.Fatal("damaged ciphertext migration succeeded")
	}
	var unchanged string
	if err = db.QueryRow(`SELECT dek_encrypted FROM orgs`).Scan(&unchanged); err != nil || unchanged != dek {
		t.Fatal("partial migration committed")
	}
	if _, err = db.Exec(`UPDATE secret_versions SET ciphertext=?`, value); err != nil {
		t.Fatal(err)
	}
	if n, err := store.UpgradeEnvelopes(db, key, true); err != nil || n != 2 {
		t.Fatalf("upgrade %d %v", n, err)
	}
	if n, err := store.UpgradeEnvelopes(db, key, true); err != nil || n != 0 {
		t.Fatalf("upgrade not idempotent: %d %v", n, err)
	}
	if err = db.QueryRow(`SELECT ciphertext FROM secret_versions`).Scan(&unchanged); err != nil || !strings.HasPrefix(unchanged, "taboo:v1:") {
		t.Fatal("new envelope missing")
	}
	if plaintext, err := tc.Open(key, unchanged, tc.SecretContext("a", "p", "s", 1)); err != nil || plaintext != "fixture secret" {
		t.Fatal("migration changed plaintext")
	}
	// Recover a stopped SQLite instance from the pre-migration backup and key.
	recovered, err := store.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if n, err := store.UpgradeEnvelopes(recovered, key, false); err != nil || n != 2 {
		t.Fatalf("backup not recoverable: %d %v", n, err)
	}
	target := testdb.Open(t)
	if store.IsPostgres(target) {
		counts, err := store.ImportSQLite(db, target)
		if err != nil || counts["secret_versions"] != 1 {
			t.Fatalf("transfer %v %v", counts, err)
		}
		if _, err = store.UpgradeEnvelopes(target, key, false); err != nil {
			t.Fatal(err)
		}
		v, err := audit.Verify(target, "a", key)
		if err != nil || !v.OK || v.Checked != 1 {
			t.Fatalf("transferred audit chain %+v %v", v, err)
		}
		if _, err = store.ImportSQLite(db, target); err == nil {
			t.Fatal("nonempty target overwritten")
		}
	}
}

func TestSQLiteMigrationRefusesFutureSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟"库由更新的二进制迁移过"：goose 版本表里出现高于本服务的版本。
	if _, err = db.Exec(`INSERT INTO goose_db_version(version_id, is_applied) VALUES (999, 1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if migrated, err := store.Open(dir); err == nil {
		migrated.Close()
		t.Fatal("future database downgraded")
	}
}
