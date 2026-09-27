package outbox

import (
	"database/sql"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
)

// Complete commits a fenced job update and its audit together. The caller's
// update must compare the current lease token and deadline. A stale claimant
// affects no row and produces no completion audit.
func Complete(db *sql.DB, query string, args []any, record audit.Rec) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return nil
	}
	record.FailOpen = false
	if err = audit.Record(tx, record); err != nil {
		return err
	}
	return tx.Commit()
}
