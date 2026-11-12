// Package testdb runs the same integration fixtures against portable SQLite and
// PostgreSQL. Every PostgreSQL fixture owns a fresh schema, removed on cleanup.
package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/lib/pq"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func Open(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TABOO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("TABOO_REQUIRE_POSTGRES") == "1" {
			t.Fatal("TABOO_TEST_POSTGRES_DSN is required")
		}
		db, err := store.Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "taboo_test_" + strings.ReplaceAll(tc.NewID(), "-", "")
	if _, err = bootstrap.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(name)); err != nil {
		bootstrap.Close()
		t.Fatal(err)
	}
	// The production migrations grant the runtime role USAGE on public. A test
	// fixture owns a private schema instead, so grant the same schema visibility
	// here; the per-table grants in 00012/00014 resolve through search_path.
	if _, err = bootstrap.Exec(`GRANT USAGE ON SCHEMA ` + pq.QuoteIdentifier(name) + ` TO taboo_runtime`); err != nil {
		bootstrap.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { bootstrap.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(name) + ` CASCADE`); bootstrap.Close() })
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	db, err := store.OpenPostgres(context.Background(), u.String(), "taboo_runtime")
	if err != nil {
		t.Fatal(err)
	}
	// 00013 grants every table in public; a fresh test schema needs the same
	// table/sequence grants after migrations have created them.
	for _, grant := range []string{
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + pq.QuoteIdentifier(name) + ` TO taboo_runtime`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA ` + pq.QuoteIdentifier(name) + ` TO taboo_runtime`,
	} {
		if _, err = bootstrap.Exec(grant); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func FailAudit(t *testing.T, db *sql.DB) {
	t.Helper()
	q := `CREATE TRIGGER fail_audit BEFORE INSERT ON audit_logs BEGIN SELECT RAISE(FAIL,'fixture'); END`
	if store.IsPostgres(db) {
		q = `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END; $$; CREATE TRIGGER fail_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_audit()`
	}
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
