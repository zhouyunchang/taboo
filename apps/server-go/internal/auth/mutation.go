package auth

import (
	"database/sql"
	"net/http"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
)

// BeginMutation and CommitMutation keep the durable state change and its audit
// record in one transaction. Callers must defer Rollback after a successful begin.
func BeginMutation(db *sql.DB, w http.ResponseWriter, r *http.Request) (*sql.Tx, bool) {
	tx, err := db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return nil, false
	}
	return tx, true
}
func CommitMutation(tx *sql.Tx, w http.ResponseWriter, r *http.Request, org string, actor *Actor, action, resource string, metadata map[string]any) bool {
	rec := recOf(org, actor, action, resource, metadata, httpx.ClientIP(r), httpx.UserAgent(r), httpx.RequestID(r))
	rec.FailOpen = false
	if err := audit.Record(tx, rec); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "change was not committed"))
		return false
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(503, "COMMIT_UNCONFIRMED", "reload before retrying the change"))
		return false
	}
	return true
}
