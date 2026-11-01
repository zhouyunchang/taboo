package auth

import (
	"database/sql"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

// jitAllows is an exact, expiring grant. It never implies admin, membership
// management, or a different action than the one that was approved.
func jitAllows(db *sql.DB, actor *Actor, orgID, projectID, action, envSlug string) bool {
	if actor == nil || actor.Kind != KindUser || projectID == "" {
		return false
	}
	switch action {
	case rbac.Reveal, rbac.Export, rbac.Write, rbac.Delete, rbac.Rollback:
	default:
		return false
	}
	envID := ""
	if envSlug != "" {
		if err := db.QueryRow(`SELECT id FROM environments WHERE project_id=? AND slug=?`, projectID, envSlug).Scan(&envID); err != nil {
			return false
		}
	}
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM access_requests WHERE org_id=? AND project_id=? AND requester_id=? AND action=? AND status='approved' AND expires_at>? AND (env_id='' OR env_id=?)`,
		orgID, projectID, actor.ID, action, time.Now().Unix(), envID).Scan(&n)
	return err == nil && n > 0
}
