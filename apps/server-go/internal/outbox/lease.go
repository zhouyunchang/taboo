package outbox

import (
	"database/sql"
	"errors"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"time"
)

// Claim fences concurrent workers. Every completion must compare lease_token;
// an expired worker can never overwrite the new owner's result. External effects
// remain at-least-once, with a stable event ID available for receiver deduplication.
func Claim(db *sql.DB, table, id string) (string, error) {
	if table != "sync_runs" && table != "webhook_deliveries" {
		return "", errors.New("unknown job table")
	}
	token := tc.NewID()
	now := time.Now().Unix()
	result, err := db.Exec(`UPDATE `+table+` SET lease_token=?,lease_until=? WHERE id=? AND status='pending' AND lease_until<=? AND next_attempt_at<=?`, token, now+60, id, now, now)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", nil
	}
	return token, nil
}
