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
	if v != 0 && v != 11 && v != 12 && v != 13 && v != 14 && v != 15 && v != 16 && v != 17 && v != 18 && v != SchemaVersion {
		return fmt.Errorf("unsupported PostgreSQL schema version: %d", v)
	}
	if v == 0 {
		if _, err = tx.Exec(postgresBaseline); err != nil {
			return fmt.Errorf("PostgreSQL baseline: %w", err)
		}
		if err = installPolicies(tx, owner, role); err != nil {
			return err
		}
	}
	if v < 12 {
		if err = installWorkloads(tx, owner, role); err != nil {
			return err
		}
	}
	if v < 13 {
		if err = installGovernance(tx, owner, role); err != nil {
			return err
		}
	}
	if v < 14 {
		if err = installBrowserSecurity(tx, owner, role); err != nil {
			return err
		}
	}
	if v < 15 {
		for _, table := range []string{"audit_exports", "dynamic_leases"} {
			if _, err = tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN IF NOT EXISTS lease_token TEXT NOT NULL DEFAULT '', ADD COLUMN IF NOT EXISTS lease_until BIGINT NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
		for _, table := range []string{"sync_runs", "outbox_events"} {
			if _, err = tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN IF NOT EXISTS event_sequence BIGINT NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}

		predicate := `current_user=` + pq.QuoteLiteral(owner) + ` OR user_id=nullif(current_setting('taboo.user_id',true),'') OR (user_id IN (SELECT user_id FROM org_members WHERE org_id=nullif(current_setting('taboo.org_id',true),'')) AND EXISTS(SELECT 1 FROM org_members WHERE org_id=nullif(current_setting('taboo.org_id',true),'') AND user_id=nullif(current_setting('taboo.user_id',true),'') AND role='owner'))`
		if _, err = tx.Exec(`ALTER POLICY tenant_scope ON external_identities USING (` + predicate + `) WITH CHECK (` + predicate + `)`); err != nil {
			return err
		}

	}
	if v < 16 {
		if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS scan_reports (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  created_by TEXT NOT NULL,
  actor_name TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT '',
  finding_count INTEGER NOT NULL DEFAULT 0,
  critical_count INTEGER NOT NULL DEFAULT 0,
  warning_count INTEGER NOT NULL DEFAULT 0,
  findings_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
			return err
		}
		if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_scan_reports_org ON scan_reports(org_id, created_at)`); err != nil {
			return err
		}
		predicate := `org_id=nullif(current_setting('taboo.org_id',true),'')`
		if _, err = tx.Exec(`ALTER TABLE scan_reports ENABLE ROW LEVEL SECURITY; ALTER TABLE scan_reports FORCE ROW LEVEL SECURITY; DROP POLICY IF EXISTS tenant_scope ON scan_reports; CREATE POLICY tenant_scope ON scan_reports USING (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)); GRANT SELECT,INSERT,UPDATE,DELETE ON scan_reports TO ` + pq.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	if v < 17 {
		if _, err = tx.Exec(`ALTER TABLE machine_identities ADD COLUMN IF NOT EXISTS allowed_cidrs TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
		if _, err = tx.Exec(`ALTER TABLE machine_identities ADD COLUMN IF NOT EXISTS list_scan_reports INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if _, err = tx.Exec(`ALTER TABLE dynamic_engines DROP CONSTRAINT IF EXISTS dynamic_engines_type_check`); err != nil {
			return err
		}
		if _, err = tx.Exec(`ALTER TABLE dynamic_engines ADD CONSTRAINT dynamic_engines_type_check CHECK (type IN ('postgres','mysql'))`); err != nil {
			return err
		}
		if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS rotation_policies (
  id TEXT PRIMARY KEY,
  org_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  secret_id TEXT NOT NULL UNIQUE REFERENCES secrets(id) ON DELETE CASCADE,
  interval_seconds INTEGER NOT NULL DEFAULT 0,
  cron TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  next_run_at BIGINT NOT NULL,
  last_run_at BIGINT NOT NULL DEFAULT 0,
  last_event_id TEXT NOT NULL DEFAULT '',
  lease_token TEXT NOT NULL DEFAULT '',
  lease_until BIGINT NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
)`); err != nil {
			return err
		}
		if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_rotation_due ON rotation_policies(enabled, next_run_at)`); err != nil {
			return err
		}
		predicate := `org_id=nullif(current_setting('taboo.org_id',true),'')`
		if _, err = tx.Exec(`ALTER TABLE rotation_policies ENABLE ROW LEVEL SECURITY; ALTER TABLE rotation_policies FORCE ROW LEVEL SECURITY; DROP POLICY IF EXISTS tenant_scope ON rotation_policies; CREATE POLICY tenant_scope ON rotation_policies USING (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)); GRANT SELECT,INSERT,UPDATE,DELETE ON rotation_policies TO ` + pq.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	if v < 18 {
		if err = installPhase6(tx, owner, role); err != nil {
			return err
		}
	}
	if v < 19 {
		if _, err = tx.Exec(`ALTER TABLE orgs ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'organization'`); err != nil {
			return err
		}
		if _, err = tx.Exec(`ALTER TABLE orgs ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id)`); err != nil {
			return err
		}
		if _, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS orgs_one_personal_owner ON orgs(owner_user_id) WHERE kind = 'personal'`); err != nil {
			return err
		}
		if _, err = tx.Exec(`DO $$ BEGIN ALTER TABLE orgs ADD CONSTRAINT orgs_kind_check CHECK (kind IN ('personal','organization')); EXCEPTION WHEN duplicate_object THEN NULL; END $$`); err != nil {
			return err
		}
		if err = classifyTenants(tx); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO schema_version(singleton,version) VALUES(true,?) ON CONFLICT(singleton) DO UPDATE SET version=excluded.version`, SchemaVersion); err != nil {
		return err
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

func installWorkloads(tx *sql.Tx, owner, role string) error {
	if _, err := tx.Exec(workloadSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE workload_grants ADD CONSTRAINT workloads_project_org FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE; ALTER TABLE workload_grants ADD CONSTRAINT workloads_env_project FOREIGN KEY(env_id,project_id) REFERENCES environments(id,project_id) ON DELETE CASCADE; ALTER TABLE machine_identities ADD CONSTRAINT identities_id_org UNIQUE(id,org_id); ALTER TABLE workload_bindings ADD CONSTRAINT workloads_machine_org FOREIGN KEY(id,org_id) REFERENCES machine_identities(id,org_id) ON DELETE CASCADE;`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE FUNCTION check_workload_identity() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF NOT EXISTS(SELECT 1 FROM machine_identities WHERE id=NEW.id AND org_id=NEW.org_id AND auth_type='external_oauth') THEN
 RAISE EXCEPTION 'external workload identity required' USING ERRCODE='23503';
 END IF; RETURN NEW; END $$;
 CREATE TRIGGER integrity_workload_identity BEFORE INSERT OR UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION check_workload_identity();
 CREATE TRIGGER immutable_oauth_trusts BEFORE UPDATE ON oauth_trusts FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
 CREATE TRIGGER immutable_workload_bindings BEFORE UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION immutable_ownership('org_id');
 CREATE TRIGGER immutable_workload_binding_trust BEFORE UPDATE ON workload_bindings FOR EACH ROW EXECUTE FUNCTION immutable_ownership('trust_id');
 CREATE TRIGGER immutable_machine_auth BEFORE UPDATE ON machine_identities FOR EACH ROW EXECUTE FUNCTION immutable_ownership('auth_type');`); err != nil {
		return err
	}
	for _, table := range []string{"oauth_trusts", "workload_bindings", "workload_grants"} {
		if _, err := tx.Exec(`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_scope ON ` + table + ` USING (current_user=` + pq.QuoteLiteral(owner) + ` OR org_id=nullif(current_setting('taboo.org_id',true),'')) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR org_id=nullif(current_setting('taboo.org_id',true),'')); GRANT SELECT,INSERT,UPDATE,DELETE ON ` + table + ` TO ` + pq.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	return nil
}

func installGovernance(tx *sql.Tx, owner, role string) error {
	if _, err := tx.Exec(governanceSchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE workload_grants DROP CONSTRAINT workload_grants_action_check; ALTER TABLE workload_grants ADD CONSTRAINT workload_grants_action_check CHECK(action IN ('read','reveal','write','export','rollback','delete')); ALTER TABLE member_action_grants ADD CONSTRAINT member_actions_project_org FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE;
 ALTER TABLE member_action_grants ADD CONSTRAINT member_actions_env_project FOREIGN KEY(env_id,project_id) REFERENCES environments(id,project_id) ON DELETE CASCADE;`); err != nil {
		return err
	}
	for _, table := range []string{"org_security", "member_action_grants"} {
		condition := `current_user=` + pq.QuoteLiteral(owner) + ` OR org_id=nullif(current_setting('taboo.org_id',true),'')`
		if table == "org_security" {
			condition += ` OR (nullif(current_setting('taboo.org_id',true),'') IS NULL AND EXISTS(SELECT 1 FROM org_members m WHERE m.org_id=org_security.org_id AND m.user_id=nullif(current_setting('taboo.user_id',true),'')))`
		}
		if _, err := tx.Exec(`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_scope ON ` + table + ` USING (` + condition + `) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR org_id=nullif(current_setting('taboo.org_id',true),'')); GRANT SELECT,INSERT,UPDATE,DELETE ON ` + table + ` TO ` + pq.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	return nil
}

func installBrowserSecurity(tx *sql.Tx, owner, role string) error {
	if _, err := tx.Exec(browserSecuritySchema); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE oidc_providers ADD CONSTRAINT provider_id_org UNIQUE(id,org_id);
 ALTER TABLE provider_security ADD CONSTRAINT security_provider_org FOREIGN KEY(provider_id,org_id) REFERENCES oidc_providers(id,org_id) ON DELETE CASCADE;
 ALTER TABLE browser_sessions ADD CONSTRAINT browser_token_org UNIQUE(token_hash,org_id);
 ALTER TABLE browser_credentials ADD CONSTRAINT credentials_session_org FOREIGN KEY(token_hash,org_id) REFERENCES browser_sessions(token_hash,org_id) ON DELETE CASCADE;`); err != nil {
		return err
	}
	for _, table := range []string{"provider_security", "browser_credentials"} {
		condition := `current_user=` + pq.QuoteLiteral(owner) + ` OR org_id=nullif(current_setting('taboo.org_id',true),'')`
		if _, err := tx.Exec(`ALTER TABLE ` + table + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + table + ` FORCE ROW LEVEL SECURITY; CREATE POLICY tenant_scope ON ` + table + ` USING (` + condition + `) WITH CHECK (` + condition + `); GRANT SELECT,INSERT,UPDATE,DELETE ON ` + table + ` TO ` + pq.QuoteIdentifier(role)); err != nil {
			return err
		}
	}
	return nil
}

func installPhase6(tx *sql.Tx, owner, role string) error {
	for _, stmt := range []string{
		`ALTER TABLE environments ADD COLUMN IF NOT EXISTS approval_required INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE org_members ADD COLUMN IF NOT EXISTS scim_managed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE browser_sessions ADD COLUMN IF NOT EXISTS sid TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE browser_sessions ADD COLUMN IF NOT EXISTS subject TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_browser_logout_sid ON browser_sessions(provider_id, sid)`,
		`CREATE INDEX IF NOT EXISTS idx_browser_logout_sub ON browser_sessions(provider_id, subject)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(phase6Tables); err != nil {
		return err
	}
	orgScoped := []string{"approval_requests", "access_requests", "scim_tokens", "scim_users", "scim_groups", "scim_group_members"}
	predicate := `org_id=nullif(current_setting('taboo.org_id',true),'')`
	for _, table := range orgScoped {
		q := `ALTER TABLE ` + pq.QuoteIdentifier(table) + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + pq.QuoteIdentifier(table) + ` FORCE ROW LEVEL SECURITY; DROP POLICY IF EXISTS tenant_scope ON ` + pq.QuoteIdentifier(table) + `; CREATE POLICY tenant_scope ON ` + pq.QuoteIdentifier(table) + ` USING (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + ` OR (` + predicate + `)); GRANT SELECT,INSERT,UPDATE,DELETE ON ` + pq.QuoteIdentifier(table) + ` TO ` + pq.QuoteIdentifier(role)
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("RLS %s: %w", table, err)
		}
	}
	// Device codes, logout replays, rate counters and leases stay on the control
	// pool. The runtime role is not granted access.
	for _, table := range []string{"device_authorizations", "cli_auth_codes", "logout_replays", "rate_counters", "cluster_leases"} {
		q := `ALTER TABLE ` + pq.QuoteIdentifier(table) + ` ENABLE ROW LEVEL SECURITY; ALTER TABLE ` + pq.QuoteIdentifier(table) + ` FORCE ROW LEVEL SECURITY; DROP POLICY IF EXISTS tenant_scope ON ` + pq.QuoteIdentifier(table) + `; CREATE POLICY tenant_scope ON ` + pq.QuoteIdentifier(table) + ` USING (current_user=` + pq.QuoteLiteral(owner) + `) WITH CHECK (current_user=` + pq.QuoteLiteral(owner) + `)`
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("RLS %s: %w", table, err)
		}
	}
	return nil
}
