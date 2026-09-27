package store

import "fmt"

// SQLite cannot add composite foreign keys with ALTER TABLE. Equivalent guards
// validate existing rows before installing insert/update constraints. Ownership
// edges are immutable: resources must be explicitly copied to another tenant.
func migrateIntegrity(db migrationDB) error {
	checks := map[string]string{
		"secrets":            `EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.folder_id AND f.env_id=NEW.env_id)`,
		"folders":            `NEW.parent_id IS NULL OR EXISTS(SELECT 1 FROM folders f WHERE f.id=NEW.parent_id AND f.env_id=NEW.env_id)`,
		"project_grants":     `EXISTS(SELECT 1 FROM projects p JOIN org_members m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id AND m.user_id=NEW.user_id)`,
		"identity_scopes":    `EXISTS(SELECT 1 FROM projects p JOIN environments e ON e.project_id=p.id JOIN machine_identities m ON m.org_id=p.org_id WHERE p.id=NEW.project_id AND e.id=NEW.env_id AND m.id=NEW.identity_id)`,
		"dynamic_engines":    `EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)`,
		"sync_targets":       `NEW.project_id IS NULL OR EXISTS(SELECT 1 FROM projects p WHERE p.id=NEW.project_id AND p.org_id=NEW.org_id)`,
		"sync_runs":          `EXISTS(SELECT 1 FROM sync_targets t WHERE t.id=NEW.target_id AND t.org_id=NEW.org_id)`,
		"webhook_deliveries": `EXISTS(SELECT 1 FROM webhooks w WHERE w.id=NEW.webhook_id AND w.org_id=NEW.org_id)`,
	}
	for table, condition := range checks {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` AS NEW WHERE NOT (` + condition + `)`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("migration refused: %s contains %d invalid ownership references; repair from backup before retrying", table, n)
		}
		for _, operation := range []string{"INSERT", "UPDATE"} {
			if _, err := db.Exec(`CREATE TRIGGER IF NOT EXISTS integrity_` + table + `_` + operation + ` BEFORE ` + operation + ` ON ` + table + ` WHEN NOT (` + condition + `) BEGIN SELECT RAISE(ABORT,'invalid resource ownership'); END`); err != nil {
				return err
			}
		}
	}
	for table, column := range map[string]string{"projects": "org_id", "environments": "project_id", "folders": "env_id", "secrets": "env_id", "machine_identities": "org_id", "sync_targets": "org_id", "webhooks": "org_id", "dynamic_engines": "org_id"} {
		if _, err := db.Exec(`CREATE TRIGGER IF NOT EXISTS immutable_` + table + ` BEFORE UPDATE ON ` + table + ` WHEN NEW.id<>OLD.id OR NEW.` + column + `<>OLD.` + column + ` BEGIN SELECT RAISE(ABORT,'resource ownership is immutable'); END`); err != nil {
			return err
		}
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("migration refused: existing foreign key violation")
	}
	return rows.Err()
}
