package store

import (
	"database/sql"
	"errors"
	"strings"
)

func ensurePathCodes(db migrationDB) error {
	if err := addColumn(db, `ALTER TABLE users ADD COLUMN handle TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addColumn(db, `ALTER TABLE users ADD COLUMN handle_confirmed INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS path_codes (
  code   TEXT PRIMARY KEY,
  kind   TEXT NOT NULL CHECK (kind IN ('user','organization')),
  ref_id TEXT NOT NULL
)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_handle ON users(handle) WHERE handle <> ''`); err != nil {
		return err
	}
	return backfillPathCodes(db)
}

func addColumn(db migrationDB, stmt string) error {
	_, err := db.Exec(stmt)
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "duplicate column") || strings.Contains(msg, "already exists") {
		return nil
	}
	return err
}

func backfillPathCodes(db migrationDB) error {
	rows, err := db.Query(`SELECT id, slug, kind, COALESCE(owner_user_id,'') FROM orgs`)
	if err != nil {
		return err
	}
	type row struct{ id, slug, kind, owner string }
	found := []row{}
	for rows.Next() {
		var item row
		if err = rows.Scan(&item.id, &item.slug, &item.kind, &item.owner); err != nil {
			rows.Close()
			return err
		}
		found = append(found, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range found {
		if item.kind == "personal" && item.owner != "" {
			if _, err = db.Exec(`UPDATE users SET handle=? WHERE id=? AND handle=''`, item.slug, item.owner); err != nil {
				return err
			}
			if err = insertPathCode(db, item.slug, "user", item.owner); err != nil {
				return err
			}
			continue
		}
		if err = insertPathCode(db, item.slug, "organization", item.id); err != nil {
			return err
		}
	}
	return nil
}

func insertPathCode(db migrationDB, code, kind, ref string) error {
	var gotKind, gotRef string
	err := db.QueryRow(`SELECT kind, ref_id FROM path_codes WHERE code=?`, code).Scan(&gotKind, &gotRef)
	if err == nil {
		if gotKind == kind && gotRef == ref {
			return nil
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = db.Exec(`INSERT INTO path_codes(code, kind, ref_id) VALUES(?,?,?)`, code, kind, ref)
	if err != nil && (strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key")) {
		return nil
	}
	return err
}
