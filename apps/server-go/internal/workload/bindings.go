package workload

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type Grant struct {
	ProjectID     string `json:"project_id"`
	EnvironmentID string `json:"environment_id"`
	Action        string `json:"action"`
	RequiredScope string `json:"required_scope"`
}
type Binding struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	TrustID  string  `json:"trust_id"`
	Subject  string  `json:"subject"`
	ClientID string  `json:"client_id"`
	Enabled  bool    `json:"enabled"`
	Grants   []Grant `json:"grants"`
}

func (s *Service) validateGrants(w http.ResponseWriter, org string, grants []Grant) bool {
	if len(grants) == 0 || len(grants) > 200 {
		fail(w, 400, "INVALID_GRANTS", "between 1 and 200 explicit resource grants are required")
		return false
	}
	seen := map[string]bool{}
	for _, g := range grants {
		if g.Action != "read" && g.Action != "reveal" && g.Action != "write" && g.Action != "export" {
			fail(w, 400, "INVALID_GRANTS", "unsupported workload action")
			return false
		}
		if g.RequiredScope == "" || len(g.RequiredScope) > 200 || strings.ContainsAny(g.RequiredScope, " \t\r\n") {
			fail(w, 400, "INVALID_GRANTS", "one exact OAuth scope is required for each action")
			return false
		}
		key := g.ProjectID + "\x00" + g.EnvironmentID + "\x00" + g.Action
		if seen[key] {
			fail(w, 400, "INVALID_GRANTS", "duplicate resource grant")
			return false
		}
		seen[key] = true
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=? AND p.id=? AND p.org_id=?`, g.EnvironmentID, g.ProjectID, org).Scan(&n); err != nil || n != 1 {
			fail(w, 400, "INVALID_GRANTS", "project and environment must belong to this organization")
			return false
		}
	}
	return true
}
func insertGrants(tx *sql.Tx, org, id string, grants []Grant) error {
	for _, g := range grants {
		if _, err := tx.Exec(`INSERT INTO workload_grants(binding_id,org_id,project_id,env_id,action,required_scope) VALUES(?,?,?,?,?,?)`, id, org, g.ProjectID, g.EnvironmentID, g.Action, g.RequiredScope); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) CreateBinding(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	var b Binding
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.Name == "" || len(b.Name) > 200 || b.Subject == "" || len(b.Subject) > 512 || b.ClientID == "" || len(b.ClientID) > 512 {
		fail(w, 400, "INVALID_BINDING", "name, exact external subject and client ID are required")
		return
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM oauth_trusts WHERE id=? AND org_id=? AND enabled=1`, b.TrustID, org).Scan(&n); err != nil || n != 1 {
		fail(w, 400, "INVALID_TRUST", "select an enabled trust in this organization")
		return
	}
	if !s.validateGrants(w, org, b.Grants) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding unavailable")
		return
	}
	defer tx.Rollback()
	id := tc.NewID()
	if _, err = tx.Exec(`INSERT INTO machine_identities(id,org_id,name,auth_type,client_id,secret_hash) VALUES(?,?,?,'external_oauth',?,'!')`, id, org, b.Name, "external:"+id); err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding could not be saved")
		return
	}
	if _, err = tx.Exec(`INSERT INTO workload_bindings(id,org_id,trust_id,subject,client_id) VALUES(?,?,?,?,?)`, id, org, b.TrustID, b.Subject, b.ClientID); err != nil {
		fail(w, 409, "BINDING_CONFLICT", "this external identity is already bound or its trust changed")
		return
	}
	if err = insertGrants(tx, org, id, b.Grants); err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding grants could not be saved")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.binding.create", "workload/"+id, map[string]any{"trust_id": b.TrustID, "subject": b.Subject, "client_id": b.ClientID, "grant_count": len(b.Grants)}); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "binding could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "binding could not be saved")
		return
	}
	reply(w, 201, map[string]any{"id": id})
}
func (s *Service) ListBindings(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT b.id,m.name,b.trust_id,b.subject,b.client_id,b.enabled,m.status FROM workload_bindings b JOIN machine_identities m ON m.id=b.id AND m.org_id=b.org_id WHERE b.org_id=? ORDER BY m.name,b.id`, org)
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding list unavailable")
		return
	}
	bindings := []Binding{}
	for rows.Next() {
		var b Binding
		var enabled int
		var status string
		if err = rows.Scan(&b.ID, &b.Name, &b.TrustID, &b.Subject, &b.ClientID, &enabled, &status); err != nil {
			rows.Close()
			fail(w, 500, "STORAGE_ERROR", "binding list unavailable")
			return
		}
		b.Enabled = enabled == 1 && status == "active"
		b.Grants = []Grant{}
		bindings = append(bindings, b)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding list unavailable")
		return
	}
	for i := range bindings {
		rows, err := s.DB.Query(`SELECT project_id,env_id,action,required_scope FROM workload_grants WHERE binding_id=? AND org_id=? ORDER BY project_id,env_id,action`, bindings[i].ID, org)
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "binding grants unavailable")
			return
		}
		for rows.Next() {
			var g Grant
			if err = rows.Scan(&g.ProjectID, &g.EnvironmentID, &g.Action, &g.RequiredScope); err != nil {
				rows.Close()
				fail(w, 500, "STORAGE_ERROR", "binding grants unavailable")
				return
			}
			bindings[i].Grants = append(bindings[i].Grants, g)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			fail(w, 500, "STORAGE_ERROR", "binding grants unavailable")
			return
		}
	}
	reply(w, 200, map[string]any{"workloads": bindings})
}
func (s *Service) PutGrants(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var b struct {
		Grants []Grant `json:"grants"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		fail(w, 400, "INVALID_INPUT", "invalid grants")
		return
	}
	if !s.validateGrants(w, org, b.Grants) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding unavailable")
		return
	}
	defer tx.Rollback()
	var found string
	if err = tx.QueryRow(`UPDATE workload_bindings SET enabled=enabled WHERE id=? AND org_id=? AND enabled=1 RETURNING id`, id, org).Scan(&found); err != nil {
		fail(w, 404, "NOT_FOUND", "enabled binding not found")
		return
	}
	if _, err = tx.Exec(`DELETE FROM workload_grants WHERE binding_id=? AND org_id=?`, id, org); err != nil {
		fail(w, 500, "STORAGE_ERROR", "grants could not be saved")
		return
	}
	if err = insertGrants(tx, org, id, b.Grants); err != nil {
		fail(w, 500, "STORAGE_ERROR", "grants could not be saved")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.grants.update", "workload/"+id, map[string]any{"grant_count": len(b.Grants)}); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "grants could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "grants could not be saved")
		return
	}
	reply(w, 200, map[string]any{"id": id})
}
func (s *Service) RevokeBinding(w http.ResponseWriter, r *http.Request) {
	org, ok := s.owner(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := s.DB.Begin()
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding unavailable")
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE workload_bindings SET enabled=0 WHERE id=? AND org_id=?`, id, org)
	if err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding could not be revoked")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "NOT_FOUND", "binding not found")
		return
	}
	if _, err = tx.Exec(`UPDATE machine_identities SET status='revoked' WHERE id=? AND org_id=? AND auth_type='external_oauth'`, id, org); err != nil {
		fail(w, 500, "STORAGE_ERROR", "binding could not be revoked")
		return
	}
	if err = auth.AuditReq(tx, r, org, auth.From(r), "workload.binding.revoke", "workload/"+id, nil); err != nil {
		fail(w, 503, "AUDIT_UNAVAILABLE", "revocation could not be recorded")
		return
	}
	if tx.Commit() != nil {
		fail(w, 503, "STORAGE_ERROR", "binding could not be revoked")
		return
	}
	reply(w, 200, map[string]any{"id": id, "revoked": true})
}
