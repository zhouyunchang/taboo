package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

//go:embed migrations/001_postgres.sql
var postgresBaseline string

// PostgreSQL uses a provisioned control login (schema owner) and a NOLOGIN,
// NOSUPERUSER, NOBYPASSRLS runtime role. The latter owns no tables. Public
// authentication and trusted workers use the control pool; authenticated HTTP
// handlers receive a separate connection with immutable tenant/user scope.
type pgDriver struct {
	connector   driver.Connector
	runtimeRole string
}

func (d *pgDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use PostgreSQL connector")
}

type pgConnector struct {
	d         *pgDriver
	scoped    bool
	org, user string
	scopeCtx  context.Context
}

func (c *pgConnector) Driver() driver.Driver { return c.d }
func (c *pgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.d.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	x := &pgConn{Conn: conn, scopeCtx: c.scopeCtx}
	if c.scoped {
		if _, err = x.exec(ctx, "SET ROLE "+pq.QuoteIdentifier(c.d.runtimeRole), nil); err == nil {
			_, err = x.exec(ctx, "SELECT set_config('taboo.org_id',$1,false),set_config('taboo.user_id',$2,false)", []driver.NamedValue{{Ordinal: 1, Value: c.org}, {Ordinal: 2, Value: c.user}})
		}
		if err != nil {
			conn.Close()
			return nil, err
		}
	}
	return x, nil
}

type pgConn struct {
	driver.Conn
	scopeCtx context.Context
}

func (c *pgConn) queryContext(ctx context.Context) context.Context {
	if c.scopeCtx != nil {
		return c.scopeCtx
	}
	return ctx
}

func (c *pgConn) Prepare(q string) (driver.Stmt, error) { return c.Conn.Prepare(pgSQL(q)) }
func (c *pgConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(c.queryContext(ctx), pgSQL(q))
}
func (c *pgConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(c.queryContext(ctx), opts)
}
func (c *pgConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *pgConn) exec(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(c.queryContext(ctx), q, args)
}
func (c *pgConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.exec(ctx, pgSQL(q), args)
}
func (c *pgConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(c.queryContext(ctx), pgSQL(q), args)
}

// Only SQL authored in this repository passes here; values are always bound.
// Preserve quoted strings/identifiers and comments when rebinding parameters.
func pgSQL(q string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); {
		if strings.HasPrefix(q[i:], "datetime('now')") {
			b.WriteString("to_char(timezone('UTC',clock_timestamp()),'YYYY-MM-DD HH24:MI:SS')")
			i += len("datetime('now')")
			continue
		}
		if q[i] == '\'' || q[i] == '"' {
			quote := q[i]
			b.WriteByte(quote)
			i++
			for i < len(q) {
				v := q[i]
				b.WriteByte(v)
				i++
				if v == quote {
					if i < len(q) && q[i] == quote {
						b.WriteByte(q[i])
						i++
						continue
					}
					break
				}
			}
			continue
		}
		if strings.HasPrefix(q[i:], "--") {
			j := strings.IndexByte(q[i:], '\n')
			if j < 0 {
				b.WriteString(q[i:])
				break
			}
			b.WriteString(q[i : i+j+1])
			i += j + 1
			continue
		}
		if strings.HasPrefix(q[i:], "/*") {
			j := strings.Index(q[i+2:], "*/")
			if j < 0 {
				b.WriteString(q[i:])
				break
			}
			j += 4
			b.WriteString(q[i : i+j])
			i += j
			continue
		}
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(q[i])
		}
		i++
	}
	return b.String()
}

func IsPostgres(db *sql.DB) bool { _, ok := db.Driver().(*pgDriver); return ok }

func OpenPostgres(ctx context.Context, dsn, runtimeRole string) (*sql.DB, error) {
	if runtimeRole == "" {
		return nil, errors.New("TABOO_DATABASE_RUNTIME_ROLE is required for PostgreSQL")
	}
	c, err := pq.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	d := &pgDriver{connector: c, runtimeRole: runtimeRole}
	db := sql.OpenDB(&pgConnector{d: d})
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(15 * time.Minute)
	if err = migratePostgres(ctx, db, runtimeRole); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Scoped deliberately does not share session-stateful PostgreSQL connections
// across requests. Closing the request pool destroys every physical connection,
// including on panic/cancellation. The control pool is never changed with SET ROLE.
func Scoped(ctx context.Context, db *sql.DB, org, user string) (*sql.DB, func(), error) {
	d, ok := db.Driver().(*pgDriver)
	if !ok {
		return db, func() {}, nil
	}
	x := sql.OpenDB(&pgConnector{d: d, scoped: true, org: org, user: user, scopeCtx: ctx})
	x.SetMaxOpenConns(1)
	if err := x.PingContext(ctx); err != nil {
		x.Close()
		return nil, nil, err
	}
	return x, func() { x.Close() }, nil
}

func Version(db *sql.DB) (int, error) {
	q := "PRAGMA user_version"
	if IsPostgres(db) {
		q = "SELECT version FROM schema_version WHERE singleton=true"
	}
	var v int
	err := db.QueryRow(q).Scan(&v)
	return v, err
}
func HasTable(db *sql.DB, name string) (bool, error) {
	q := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
	if IsPostgres(db) {
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=?`
	}
	var n int
	err := db.QueryRow(q, name).Scan(&n)
	return n == 1, err
}

func migratePostgres(ctx context.Context, db *sql.DB, role string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(735642019)`); err != nil {
		return err
	}
	var owner string
	if err = tx.QueryRow(`SELECT current_user`).Scan(&owner); err != nil {
		return err
	}
	var privileged bool
	if err = tx.QueryRow(`SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&privileged); err != nil || privileged {
		return errors.New("control login must be NOSUPERUSER NOBYPASSRLS; provision roles with a separate administrator")
	}
	if err = tx.QueryRow(`SELECT pg_has_role(?,?,'MEMBER')`, role, owner).Scan(&privileged); err != nil || privileged {
		return errors.New("runtime role must not inherit the control role")
	}
	var safe bool
	if err = tx.QueryRow(`SELECT NOT rolsuper AND NOT rolbypassrls AND NOT rolcanlogin FROM pg_roles WHERE rolname=?`, role).Scan(&safe); err != nil || !safe || owner == role {
		return errors.New("runtime role must exist, be NOLOGIN NOSUPERUSER NOBYPASSRLS, and differ from control login")
	}
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_version (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK(singleton), version BIGINT NOT NULL)`); err != nil {
		return err
	}
	var v int
	err = tx.QueryRow(`SELECT version FROM schema_version WHERE singleton=true`).Scan(&v)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if v > SchemaVersion {
		return fmt.Errorf("database schema %d is newer than server %d", v, SchemaVersion)
	}
	if v == 0 {
		if _, err = tx.Exec(postgresBaseline); err != nil {
			return fmt.Errorf("PostgreSQL baseline: %w", err)
		}
		if err = installPolicies(tx, owner, role); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO schema_version(singleton,version) VALUES(true,?)`, SchemaVersion); err != nil {
			return err
		}
	} else if v != SchemaVersion {
		return fmt.Errorf("unsupported PostgreSQL schema version: %d", v)
	}
	return tx.Commit()
}

func installPolicies(tx *sql.Tx, owner, role string) error {
	org := `nullif(current_setting('taboo.org_id',true),'')`
	user := `nullif(current_setting('taboo.user_id',true),'')`
	policies := map[string]string{
		"org_members":         "org_id=" + org + " OR (" + org + " IS NULL AND user_id=" + user + ")",
		"orgs":                "id=" + org + " OR (" + org + " IS NULL AND id IN (SELECT org_id FROM org_members WHERE user_id=" + user + "))",
		"users":               "id=" + user + " OR id IN (SELECT user_id FROM org_members WHERE org_id=" + org + ")",
		"environments":        "project_id IN (SELECT id FROM projects)",
		"folders":             "env_id IN (SELECT id FROM environments)",
		"secrets":             "env_id IN (SELECT id FROM environments)",
		"secret_versions":     "secret_id IN (SELECT id FROM secrets)",
		"identity_scopes":     "identity_id IN (SELECT id FROM machine_identities) AND project_id IN (SELECT id FROM projects)",
		"dynamic_leases":      "engine_id IN (SELECT id FROM dynamic_engines)",
		"refresh_tokens":      "user_id=" + user,
		"recovery_codes":      "user_id=" + user,
		"browser_sessions":    "user_id=" + user,
		"external_identities": "user_id=" + user,
		"oidc_login_requests": "false",
	}
	for _, table := range []string{"projects", "audit_logs", "machine_identities", "sync_targets", "sync_runs", "webhooks", "webhook_deliveries", "oidc_providers", "audit_exports", "dynamic_engines", "org_invites", "project_grants", "outbox_events", "audit_checkpoints", "federation_admissions", "audit_heads", "mutation_receipts"} {
		policies[table] = "org_id=" + org
	}
	for table, predicate := range policies {
		// Control access is explicit and limited to the provisioned owner role;
		// the HTTP runtime role cannot inherit this policy.
		q := `ALTER TABLE ` + pq.QuoteIdentifier(table) + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + pq.QuoteIdentifier(table) + ` FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_scope ON ` + pq.QuoteIdentifier(table) + ` USING (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `));`
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("RLS %s: %w", table, err)
		}
	}
	var schema string
	if err := tx.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		return err
	}
	_, err := tx.Exec(`GRANT USAGE ON SCHEMA ` + pq.QuoteIdentifier(schema) + ` TO ` + pq.QuoteIdentifier(role) + `; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ` + pq.QuoteIdentifier(schema) + ` TO ` + pq.QuoteIdentifier(role) + `; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA ` + pq.QuoteIdentifier(schema) + ` TO ` + pq.QuoteIdentifier(role) + `; REVOKE INSERT,UPDATE,DELETE ON schema_version FROM ` + pq.QuoteIdentifier(role))
	return err
}
