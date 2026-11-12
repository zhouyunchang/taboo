package store

import (
	"database/sql"
	"io/fs"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsAreVersioned(t *testing.T) {
	for _, tc := range []struct {
		dialect  goose.Dialect
		root     string
		expected int
	}{
		{goose.DialectPostgres, "pg", SchemaVersionPostgres},
		{goose.DialectSQLite3, "sqlite", SchemaVersionSQLite},
	} {
		files := schemaSource(tc.dialect, tc.root, "owner", "runtime")
		names, readErr := readMigrationNames(files.(fs.ReadDirFS))
		if readErr != nil {
			t.Fatalf("%s: %v", tc.root, readErr)
		}
		if len(names) != tc.expected {
			t.Fatalf("%s: %d scripts, want %d", tc.root, len(names), tc.expected)
		}
	}
}

func TestMigrationSourceReplacesDeploymentNames(t *testing.T) {
	files := schemaSource(goose.DialectPostgres, "pg", "ctrl", "runtime")
	body, err := readFile(files, "00013_runtime_grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	if contains(body, "{role}") || !contains(body, "runtime") {
		t.Fatalf("runtime role not substituted: %s", body)
	}
	rls, err := readFile(files, "00012_rls.sql")
	if err != nil {
		t.Fatal(err)
	}
	if contains(rls, "{owner}") {
		t.Fatal("{owner} left in RLS policies")
	}
}

func TestExpectedVersionPerDialect(t *testing.T) {
	db := openTestSQLite(t)
	if got := ExpectedVersion(db); got != SchemaVersionSQLite {
		t.Fatalf("sqlite expected version = %d", got)
	}
}

func TestAutoMigrateSwitch(t *testing.T) {
	t.Setenv("TABOO_AUTO_MIGRATE", "0")
	if autoMigrateEnabled() {
		t.Fatal("TABOO_AUTO_MIGRATE=0 must disable startup migrations")
	}
	t.Setenv("TABOO_AUTO_MIGRATE", "")
	if !autoMigrateEnabled() {
		t.Fatal("migrations must run by default")
	}
}

// readMigrationNames 列出内存文件系统里的迁移脚本名。
func readMigrationNames(fsys interface {
	ReadDir(string) ([]fs.DirEntry, error)
}) ([]string, error) {
	entries, err := fsys.ReadDir(".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func readFile(fsys fs.FS, name string) (string, error) {
	b, err := fs.ReadFile(fsys, name)
	return string(b), err
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// openTestSQLite 建一个空的 SQLite 连接，不入测试库 tmp 目录。
func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
