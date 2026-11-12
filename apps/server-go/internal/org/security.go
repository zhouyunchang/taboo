package org

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
)

type actionGrant struct {
	ProjectID string `json:"project_id"`
	Action    string `json:"action"`
}

func (s *Service) Security(w http.ResponseWriter, r *http.Request) {
	org, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	policy, err := auth.OrganizationLoginPolicy(s.DB, org)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "policy unavailable"))
		return
	}
	writeJSON(w, 200, map[string]any{"login_policy": policy, "external_secret_permissions": "EXPLICIT_ACTIONS"})
}

func (s *Service) PutSecurity(w http.ResponseWriter, r *http.Request) {
	org, _, ok := s.orgOf(w, r)
	if !ok || !s.sharedOrg(w, org) || !s.requireMembers(w, r, org) {
		return
	}
	var b struct {
		LoginPolicy string `json:"login_policy"`
	}
	if jsonDecode(r, &b) != nil || (b.LoginPolicy != "LOCAL_OR_EXTERNAL" && b.LoginPolicy != "EXTERNAL_REQUIRED") {
		writeErr(w, apperr.InvalidInput)
		return
	}
	a := auth.From(r)
	if b.LoginPolicy == "EXTERNAL_REQUIRED" && (a.AuthenticationSource == "" || a.BrowserOrigin == "" || a.OrganizationID != org) {
		writeErr(w, apperr.New(409, "EXTERNAL_OWNER_REQUIRED", "an owner must prove external sign-in to this organization before requiring it"))
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO org_security(org_id,login_policy) VALUES(?,?) ON CONFLICT(org_id) DO UPDATE SET login_policy=excluded.login_policy`, org, b.LoginPolicy); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	if err = auth.AuditReq(tx, r, org, a, "members.security", "organization/"+org, map[string]any{"login_policy": b.LoginPolicy}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "policy not changed"))
		return
	}
	if tx.Commit() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	writeJSON(w, 200, map[string]any{"login_policy": b.LoginPolicy})
}

func (s *Service) ListActionGrants(w http.ResponseWriter, r *http.Request) {
	org, _, ok := s.orgOf(w, r)
	if !ok || !s.sharedOrg(w, org) || !s.requireMembers(w, r, org) {
		return
	}
	uid := chi.URLParam(r, "userId")
	rows, err := s.DB.Query(`SELECT project_id,action FROM member_action_grants WHERE org_id=? AND user_id=? ORDER BY project_id,action`, org, uid)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	defer rows.Close()
	grants := []actionGrant{}
	for rows.Next() {
		var g actionGrant
		if rows.Scan(&g.ProjectID, &g.Action) != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
			return
		}
		grants = append(grants, g)
	}
	if rows.Err() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	writeJSON(w, 200, map[string]any{"grants": grants})
}

func (s *Service) PutActionGrants(w http.ResponseWriter, r *http.Request) {
	org, _, ok := s.orgOf(w, r)
	if !ok || !s.sharedOrg(w, org) || !s.requireMembers(w, r, org) {
		return
	}
	uid := chi.URLParam(r, "userId")
	var b struct {
		Grants []actionGrant `json:"grants"`
	}
	if jsonDecode(r, &b) != nil || b.Grants == nil || len(b.Grants) > 200 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	defer tx.Rollback()
	var found string
	if err = tx.QueryRow(`UPDATE org_members SET role=role WHERE org_id=? AND user_id=? RETURNING user_id`, org, uid).Scan(&found); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, err = tx.Exec(`DELETE FROM member_action_grants WHERE org_id=? AND user_id=?`, org, uid); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	seen := map[string]bool{}
	for i := range b.Grants {
		g := &b.Grants[i]
		switch g.Action {
		case "read", "write", "reveal", "export", "rollback", "delete":
		default:
			writeErr(w, apperr.InvalidInput)
			return
		}
		key := g.ProjectID + "\x00" + g.Action
		if seen[key] {
			writeErr(w, apperr.InvalidInput)
			return
		}
		seen[key] = true
		var n int
		if err = tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE org_id=? AND id=?`, org, g.ProjectID).Scan(&n); err != nil || n != 1 {
			writeErr(w, apperr.InvalidInput)
			return
		}
		if _, err = tx.Exec(`INSERT INTO member_action_grants(org_id,user_id,project_id,action) VALUES(?,?,?,?)`, org, uid, g.ProjectID, g.Action); err != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
			return
		}
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "members.actions", "member/"+uid, map[string]any{"grants": b.Grants}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "permissions not changed"))
		return
	}
	if tx.Commit() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "request could not be completed"))
		return
	}
	writeJSON(w, 200, map[string]any{"grants": b.Grants})
}

// Lock the organization before counting owners or changing membership. This
// serializes concurrent demotions/removals on PostgreSQL as well as SQLite.
func lockMembership(tx *sql.Tx, org string) error {
	_, err := tx.Exec(`UPDATE orgs SET name=name WHERE id=?`, org)
	return err
}

func preserveExternalOwner(tx *sql.Tx, org, user, nextRole string) bool {
	if nextRole == "owner" {
		return true
	}
	var policy string
	err := tx.QueryRow(`SELECT login_policy FROM org_security WHERE org_id=?`, org).Scan(&policy)
	if err == sql.ErrNoRows || policy == "LOCAL_OR_EXTERNAL" {
		return true
	}
	if err != nil {
		return false
	}
	var n int
	err = tx.QueryRow(`SELECT COUNT(*) FROM org_members m JOIN users u ON u.id=m.user_id WHERE m.org_id=? AND m.role='owner' AND m.user_id<>? AND u.status='active' AND EXISTS(SELECT 1 FROM federation_admissions a JOIN oidc_providers p ON p.id=a.provider_id AND p.org_id=a.org_id JOIN external_identities e ON e.user_id=a.user_id AND e.issuer=p.issuer WHERE a.user_id=m.user_id AND a.org_id=m.org_id AND p.enabled=1)`, org, user).Scan(&n)
	return err == nil && n > 0
}
