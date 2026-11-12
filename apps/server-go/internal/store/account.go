package store

import (
	"database/sql"
	"errors"
	"strings"
)

// ensureAccountModel adds the personal/organization distinction and, once per
// upgrade, classifies tenants that already existed. New databases create the
// columns in the baseline; later startups must not convert a sole-owner
// organization into a personal space.
func ensureAccountModel(db migrationDB, fromVersion int) error {
	if _, err := db.Exec(`ALTER TABLE orgs ADD COLUMN kind TEXT NOT NULL DEFAULT 'organization'`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE orgs ADD COLUMN owner_user_id TEXT REFERENCES users(id)`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS orgs_one_personal_owner ON orgs(owner_user_id) WHERE kind = 'personal'`); err != nil {
		return err
	}
	if fromVersion < 19 {
		return classifyTenants(db)
	}
	return nil
}

// ClassifyTenants marks each user's oldest sole-owner tenant as their personal
// space. A user who already has one is left unchanged, so repeating the
// classification cannot absorb an organization created later.
func ClassifyTenants(db migrationDB) error { return classifyTenants(db) }

func classifyTenants(db migrationDB) error {
	rows, err := db.Query(`
		SELECT m.org_id, m.user_id
		FROM org_members m
		JOIN orgs o ON o.id = m.org_id
		WHERE m.role = 'owner'
		  AND o.kind <> 'personal'
		  AND (SELECT COUNT(*) FROM org_members c WHERE c.org_id = m.org_id) = 1
		ORDER BY o.created_at, o.id`)
	if err != nil {
		return err
	}
	type pair struct{ org, user string }
	found := []pair{}
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.org, &p.user); err != nil {
			rows.Close()
			return err
		}
		found = append(found, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	seen := map[string]bool{}
	for _, p := range found {
		if seen[p.user] {
			continue
		}
		var existing string
		err = db.QueryRow(`SELECT id FROM orgs WHERE kind='personal' AND owner_user_id=?`, p.user).Scan(&existing)
		if err == nil {
			seen[p.user] = true
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = db.Exec(`UPDATE orgs SET kind='personal', owner_user_id=? WHERE id=? AND kind<>'personal'`, p.user, p.org); err != nil {
			return err
		}
		seen[p.user] = true
	}
	return nil
}
