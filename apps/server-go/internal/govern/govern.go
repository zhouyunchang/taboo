// Approval decisions and just-in-time access. Expiry is enforced by the
// timestamp on every authorization check; the sweeper only records it.
package govern

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/cluster"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

type Service struct {
	DB      *sql.DB
	Secrets *secret.Service
}

func Start(ctx context.Context, db *sql.DB) {
	if db == nil {
		return
	}
	go func() {
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			Sweep(db)
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *Service) ApprovalRoutes(r chi.Router) {
	r.Get("/", s.listApprovals)
	r.Post("/{id}/approve", s.approve)
	r.Post("/{id}/reject", s.reject)
	r.Post("/{id}/cancel", s.cancel)
}

func (s *Service) AccessRoutes(r chi.Router) {
	r.Get("/", s.listAccess)
	r.Post("/", s.createAccess)
	r.Post("/{id}/approve", s.approveAccess)
	r.Post("/{id}/reject", s.rejectAccess)
	r.Post("/{id}/revoke", s.revokeAccess)
}

func (s *Service) listApprovals(w http.ResponseWriter, r *http.Request) {
	org, actor, ok := s.member(w, r)
	if !ok {
		return
	}
	q := `SELECT id, project_id, env_id, folder, secret_key, action, status, requester_id, requester_kind, approver_id, reason, decision_note, expires_at, created_at FROM approval_requests WHERE org_id=?`
	args := []any{org}
	if !s.privileged(actor, org) {
		q += ` AND requester_id=?`
		args = append(args, actor.ID)
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer rows.Close()
	writeJSON(w, 200, map[string]any{"approvals": scanMaps(rows, []string{"id", "project_id", "env_id", "folder", "key", "action", "status", "requester_id", "requester_kind", "approver_id", "reason", "decision_note", "expires_at", "created_at"})})
}

func (s *Service) approve(w http.ResponseWriter, r *http.Request) {
	org, ok := s.orgID(w, r)
	if !ok {
		return
	}
	s.Secrets.ApplyApproval(w, r, org, chi.URLParam(r, "id"))
}

func (s *Service) reject(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, "rejected", "approval.reject")
}

func (s *Service) cancel(w http.ResponseWriter, r *http.Request) {
	org, actor, ok := s.member(w, r)
	if !ok {
		return
	}
	note := noteOf(r)
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE approval_requests SET status='cancelled', payload_enc='', decision_note=?, decided_at=datetime('now') WHERE id=? AND org_id=? AND requester_id=? AND status='pending' AND expires_at>?`,
		note, chi.URLParam(r, "id"), org, actor.ID, time.Now().Unix())
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, "approval.cancel", "approval/"+chi.URLParam(r, "id"), nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"id": chi.URLParam(r, "id"), "status": "cancelled"})
}

func (s *Service) decide(w http.ResponseWriter, r *http.Request, status, action string) {
	org, actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	note := noteOf(r)
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE approval_requests SET status=?, approver_id=?, payload_enc='', decision_note=?, decided_at=datetime('now') WHERE id=? AND org_id=? AND status='pending' AND expires_at>? AND requester_id<>?`,
		status, actor.ID, note, chi.URLParam(r, "id"), org, time.Now().Unix(), actor.ID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, action, "approval/"+chi.URLParam(r, "id"), nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"id": chi.URLParam(r, "id"), "status": status})
}

func (s *Service) listAccess(w http.ResponseWriter, r *http.Request) {
	org, actor, ok := s.member(w, r)
	if !ok {
		return
	}
	q := `SELECT id, project_id, env_id, action, status, requester_id, approver_id, reason, decision_note, ttl_seconds, expires_at, created_at FROM access_requests WHERE org_id=?`
	args := []any{org}
	if !s.privileged(actor, org) {
		q += ` AND requester_id=?`
		args = append(args, actor.ID)
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer rows.Close()
	writeJSON(w, 200, map[string]any{"requests": scanMaps(rows, []string{"id", "project_id", "env_id", "action", "status", "requester_id", "approver_id", "reason", "decision_note", "ttl_seconds", "expires_at", "created_at"})})
}

func (s *Service) createAccess(w http.ResponseWriter, r *http.Request) {
	org, actor, ok := s.member(w, r)
	if !ok {
		return
	}
	var b struct {
		ProjectID  string `json:"project_id"`
		Env        string `json:"env"`
		Action     string `json:"action"`
		TTLSeconds int    `json:"ttl_seconds"`
		Reason     string `json:"reason"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b) != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	switch b.Action {
	case rbac.Reveal, rbac.Export, rbac.Write, rbac.Delete, rbac.Rollback:
	default:
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.TTLSeconds == 0 {
		b.TTLSeconds = 1800
	}
	if b.TTLSeconds < 60 || b.TTLSeconds > 4*3600 || len(b.Reason) > 200 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var projectOrg, envID string
	if s.DB.QueryRow(`SELECT org_id FROM projects WHERE id=?`, b.ProjectID).Scan(&projectOrg) != nil || projectOrg != org {
		writeErr(w, apperr.NotFound)
		return
	}
	if b.Env == "" {
		if s.DB.QueryRow(`SELECT id FROM environments WHERE project_id=? ORDER BY sort_order LIMIT 1`, b.ProjectID).Scan(&envID) != nil {
			writeErr(w, apperr.NotFound)
			return
		}
	} else if s.DB.QueryRow(`SELECT id FROM environments WHERE project_id=? AND slug=?`, b.ProjectID, b.Env).Scan(&envID) != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var pending int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM access_requests WHERE org_id=? AND requester_id=? AND status='pending'`, org, actor.ID).Scan(&pending); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if pending >= 20 {
		writeErr(w, apperr.New(429, "TOO_MANY_REQUESTS", "too many open access requests"))
		return
	}
	id := tc.NewID()
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO access_requests(id,org_id,project_id,env_id,action,requester_id,reason,status,ttl_seconds,expires_at) VALUES(?,?,?,?,?,?,?,'pending',?,?)`,
		id, org, b.ProjectID, envID, b.Action, actor.ID, b.Reason, b.TTLSeconds, time.Now().Add(24*time.Hour).Unix()); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, "access.request", "access/"+id, map[string]any{"action": b.Action, "ttl_seconds": b.TTLSeconds}) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "status": "pending"})
}

func (s *Service) approveAccess(w http.ResponseWriter, r *http.Request) {
	org, actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	var ttl int
	var requester string
	err = tx.QueryRow(`SELECT ttl_seconds, requester_id FROM access_requests WHERE id=? AND org_id=? AND status='pending' AND expires_at>?`, chi.URLParam(r, "id"), org, time.Now().Unix()).Scan(&ttl, &requester)
	if err != nil || requester == actor.ID {
		writeErr(w, apperr.New(409, "ACCESS_CONFLICT", "request is no longer pending"))
		return
	}
	exp := time.Now().Add(time.Duration(ttl) * time.Second).Unix()
	res, err := tx.Exec(`UPDATE access_requests SET status='approved', approver_id=?, expires_at=?, decision_note=?, decided_at=datetime('now') WHERE id=? AND org_id=? AND status='pending' AND requester_id<>?`,
		actor.ID, exp, noteOf(r), chi.URLParam(r, "id"), org, actor.ID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "ACCESS_CONFLICT", "request is no longer pending"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, "access.approve", "access/"+chi.URLParam(r, "id"), map[string]any{"expires_at": exp}) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"id": chi.URLParam(r, "id"), "status": "approved", "expires_at": exp})
}

func (s *Service) rejectAccess(w http.ResponseWriter, r *http.Request) {
	s.accessStatus(w, r, "rejected", "access.reject", true)
}

func (s *Service) revokeAccess(w http.ResponseWriter, r *http.Request) {
	s.accessStatus(w, r, "revoked", "access.revoke", false)
}

func (s *Service) accessStatus(w http.ResponseWriter, r *http.Request, status, action string, fromPending bool) {
	org, actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	q := `UPDATE access_requests SET status=?, approver_id=?, decision_note=?, expires_at=0, decided_at=datetime('now') WHERE id=? AND org_id=? AND requester_id<>? AND status=?`
	from := "approved"
	if fromPending {
		from = "pending"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(q, status, actor.ID, noteOf(r), chi.URLParam(r, "id"), org, actor.ID, from)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "ACCESS_CONFLICT", "request cannot be changed"))
		return
	}
	if auth.AuditReq(tx, r, org, actor, action, "access/"+chi.URLParam(r, "id"), nil) != nil || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"id": chi.URLParam(r, "id"), "status": status})
}

// Sweep marks expired rows and drops old rate-limit windows. Safe for several
// replicas: the lease picks one scanner and each update matches only the old status.
func Sweep(db *sql.DB) {
	if db == nil {
		return
	}
	ok, err := cluster.Acquire(db, "phase6-sweep", 30*time.Second)
	if err != nil || !ok {
		return
	}
	now := time.Now().Unix()
	expire(db, `UPDATE approval_requests SET status='expired', payload_enc='', decided_at=datetime('now') WHERE id=? AND status='pending' AND expires_at<=?`,
		`SELECT id, org_id FROM approval_requests WHERE status='pending' AND expires_at<=? LIMIT 100`, "approval.expire", now)
	expire(db, `UPDATE access_requests SET status='expired', decided_at=datetime('now') WHERE id=? AND status='approved' AND expires_at<=? AND expires_at>0`,
		`SELECT id, org_id FROM access_requests WHERE status='approved' AND expires_at<=? AND expires_at>0 LIMIT 100`, "access.expire", now)
	_, _ = db.Exec(`UPDATE device_authorizations SET status='expired' WHERE status='pending' AND expires_at<=?`, now)
	_, _ = db.Exec(`DELETE FROM cli_auth_codes WHERE expires_at<=? OR consumed=1`, now-3600)
	_, _ = db.Exec(`DELETE FROM logout_replays WHERE expires_at<=?`, now)
	_, _ = db.Exec(`DELETE FROM rate_counters WHERE window_start<=?`, now-3600)
	_, _ = db.Exec(`DELETE FROM device_authorizations WHERE expires_at<=?`, now-24*3600)
}

func expire(db *sql.DB, update, query, action string, now int64) {
	rows, err := db.Query(query, now)
	if err != nil {
		return
	}
	defer rows.Close()
	type item struct{ id, org string }
	var list []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.org) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	for _, it := range list {
		tx, err := db.Begin()
		if err != nil {
			return
		}
		res, err := tx.Exec(update, it.id, now)
		if err != nil {
			tx.Rollback()
			continue
		}
		n, _ := res.RowsAffected()
		if n != 1 || auth.Audit(tx, it.org, &auth.Actor{ID: "system", Kind: "system"}, action, action+"/"+it.id, nil, "") != nil || tx.Commit() != nil {
			tx.Rollback()
		}
	}
}

func (s *Service) orgID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var org string
	if s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, chi.URLParam(r, "slug")).Scan(&org) != nil {
		writeErr(w, apperr.NotFound)
		return "", false
	}
	return org, true
}

func (s *Service) member(w http.ResponseWriter, r *http.Request) (string, *auth.Actor, bool) {
	org, ok := s.orgID(w, r)
	if !ok {
		return "", nil, false
	}
	actor := auth.From(r)
	if actor == nil || actor.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return "", nil, false
	}
	if _, ok := auth.Membership(s.DB, org, actor.ID); !ok {
		writeErr(w, apperr.Forbidden)
		return "", nil, false
	}
	return org, actor, true
}

func (s *Service) admin(w http.ResponseWriter, r *http.Request) (string, *auth.Actor, bool) {
	org, actor, ok := s.member(w, r)
	if !ok {
		return "", nil, false
	}
	if !s.privileged(actor, org) {
		auth.Deny(s.DB, w, r, org, "admin", "approval")
		return "", nil, false
	}
	return org, actor, true
}

func (s *Service) privileged(actor *auth.Actor, org string) bool {
	role, _, ok := auth.MemberInfo(s.DB, org, actor.ID)
	return ok && rbac.Privileged(role)
}

func noteOf(r *http.Request) string {
	var b struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b)
	if len(b.Note) > 200 {
		return b.Note[:200]
	}
	return strings.TrimSpace(b.Note)
}

func scanMaps(rows *sql.Rows, keys []string) []map[string]any {
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(keys))
		ptrs := make([]any, len(keys))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		row := map[string]any{}
		for i, key := range keys {
			switch v := vals[i].(type) {
			case []byte:
				row[key] = string(v)
			default:
				row[key] = v
			}
		}
		out = append(out, row)
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }
