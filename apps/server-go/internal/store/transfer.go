package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// ImportSQLite is an offline, all-or-nothing copy into an empty PostgreSQL
// schema. Stable IDs and ciphertext are preserved. Ephemeral browser login state
// is deliberately invalidated. The source is read in one consistent transaction.
func ImportSQLite(source, target *sql.DB) (map[string]int, error) {
	if IsPostgres(source) || !IsPostgres(target) {
		return nil, fmt.Errorf("transfer requires SQLite source and PostgreSQL target")
	}
	read, err := source.Begin()
	if err != nil {
		return nil, err
	}
	defer read.Rollback()
	write, err := target.Begin()
	if err != nil {
		return nil, err
	}
	defer write.Rollback()
	tables := []string{"users", "orgs", "org_members", "projects", "environments", "folders", "secrets", "secret_versions", "audit_logs", "refresh_tokens", "machine_identities", "identity_scopes", "recovery_codes", "sync_targets", "sync_runs", "webhooks", "webhook_deliveries", "oidc_providers", "audit_exports", "dynamic_engines", "dynamic_leases", "org_invites", "project_grants", "outbox_events", "audit_checkpoints", "external_identities", "federation_admissions", "audit_heads", "mutation_receipts", "oauth_trusts", "workload_bindings", "workload_grants"}
	counts := map[string]int{}
	tables = append(tables, "org_security", "member_action_grants")
	for _, table := range tables {
		var n int
		if err = write.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			return nil, err
		}
		if n != 0 {
			return nil, fmt.Errorf("target table %s is not empty", table)
		}
	}
	for _, table := range tables {
		order := ""
		if table == "folders" {
			order = " ORDER BY length(path),path"
		}
		if table == "audit_logs" {
			order = " ORDER BY rowid"
		}
		rows, err := read.Query(`SELECT * FROM ` + table + order)
		if err != nil {
			return nil, err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		quoted := make([]string, len(columns))
		placeholders := make([]string, len(columns))
		for i, c := range columns {
			quoted[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
			placeholders[i] = "?"
		}
		query := `INSERT INTO ` + table + ` (` + strings.Join(quoted, ",") + `) VALUES (` + strings.Join(placeholders, ",") + `)`
		for rows.Next() {
			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				rows.Close()
				return nil, err
			}
			if _, err = write.Exec(query, values...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("transfer table %s: %w", table, err)
			}
			counts[table]++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		var n int
		if err = write.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != counts[table] {
			return nil, fmt.Errorf("transfer count mismatch for %s", table)
		}
	}
	if err = write.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}
