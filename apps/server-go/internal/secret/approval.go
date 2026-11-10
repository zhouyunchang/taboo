package secret

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

const approvalTTL = 24 * time.Hour

type applyKey struct{}

type applyTicket struct {
	ID string
}

type approvalOp struct {
	Action   string
	OrgID    string
	EnvID    string
	Key      string
	Folder   string
	Value    string
	Comment  string
	Tags     []string
	Expected *int
	Rollback int
	Reason   string
}

type approvalMeta struct {
	Comment  string   `json:"comment,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Expected *int     `json:"expected,omitempty"`
	Rollback int      `json:"rollback,omitempty"`
}

func (op approvalOp) meta() approvalMeta {
	return approvalMeta{Comment: op.Comment, Tags: op.Tags, Expected: op.Expected, Rollback: op.Rollback}
}

func (op approvalOp) reason() string {
	if len(op.Reason) > 200 {
		return op.Reason[:200]
	}
	return op.Reason
}

func (op approvalOp) mac(dek []byte) (string, error) {
	raw, err := json.Marshal(op.meta())
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, dek)
	m.Write([]byte(op.Action))
	m.Write([]byte{0})
	m.Write([]byte(op.Key))
	m.Write([]byte{0})
	m.Write([]byte(op.Folder))
	m.Write([]byte{0})
	m.Write([]byte(op.Value))
	m.Write([]byte{0})
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil)), nil
}

// currentVersion returns the latest version of a secret, or 0 when it does not exist.
func (s *Service) currentVersion(envID, folder, key string) (int, error) {
	var v int
	err := s.DB.QueryRow(`SELECT latest_version FROM secrets WHERE env_id=? AND folder=? AND key=?`, envID, folder, key).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

func applying(r *http.Request) bool {
	_, ok := r.Context().Value(applyKey{}).(applyTicket)
	return ok
}

func (s *Service) needsApproval(envID string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT CASE WHEN COALESCE(p.approval_required,0)=1 OR COALESCE(e.approval_required,0)=1 THEN 1 ELSE 0 END FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=?`, envID).Scan(&n)
	return n == 1, err
}

// queueApproval stores the change and writes 202. It returns true when the
// HTTP response has been written, including when approval is not required.
func (s *Service) queueApproval(w http.ResponseWriter, r *http.Request, p project.Ctx, envSlug string, op approvalOp, replayBody any) bool {
	required, err := s.needsApproval(op.EnvID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return true
	}
	if !required || applying(r) {
		return false
	}
	actor := auth.From(r)
	if actor == nil {
		writeErr(w, apperr.Unauthorized)
		return true
	}
	if op.Expected == nil {
		// Pin the version the requester saw. A change that lands while the
		// request is pending makes the approval fail with VERSION_CONFLICT
		// instead of silently overwriting it.
		base, berr := s.currentVersion(op.EnvID, op.Folder, op.Key)
		if berr != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
			return true
		}
		if op.Action != "upsert" && base == 0 {
			writeErr(w, apperr.SecretNotFound)
			return true
		}
		op.Expected = &base
	}
	dek, err := s.orgDEK(op.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return true
	}
	mac, err := op.mac(dek)
	if err != nil {
		internalErr(w, err)
		return true
	}
	metaRaw, err := json.Marshal(op.meta())
	if err != nil {
		internalErr(w, err)
		return true
	}
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return true
	}
	defer tx.Rollback()
	if s.replayMutation(tx, w, r, op.OrgID, replayBody) {
		return true
	}
	var existing string
	var exp int64
	err = tx.QueryRow(`SELECT id, expires_at FROM approval_requests WHERE org_id=? AND requester_id=? AND env_id=? AND secret_key=? AND folder=? AND action=? AND payload_mac=? AND status='pending' AND expires_at>?`,
		op.OrgID, actor.ID, op.EnvID, op.Key, op.Folder, op.Action, mac, time.Now().Unix()).Scan(&existing, &exp)
	if err != nil && err != sql.ErrNoRows {
		internalErr(w, err)
		return true
	}
	id := existing
	if id == "" {
		id = tc.NewID()
		enc := ""
		if op.Action == "upsert" {
			enc, err = tc.Seal(dek, op.Value, tc.Context{Purpose: "approval", Org: op.OrgID, Project: p.ID, Environment: op.EnvID, Object: id, Version: 1})
			if err != nil {
				writeErr(w, apperr.New(500, "INTERNAL", "encrypt failed"))
				return true
			}
		}
		exp = time.Now().Add(approvalTTL).Unix()
		if _, err = tx.Exec(`INSERT INTO approval_requests(id,org_id,project_id,env_id,folder,secret_key,action,payload_enc,payload_mac,meta_json,status,requester_id,requester_kind,reason,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,'pending',?,?,?,?)`,
			id, op.OrgID, p.ID, op.EnvID, op.Folder, op.Key, op.Action, enc, mac, string(metaRaw), actor.ID, actor.Kind, op.reason(), exp); err != nil {
			internalErr(w, err)
			return true
		}
		if err = auth.AuditReq(tx, r, op.OrgID, actor, "approval.create", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+op.Key, map[string]any{"approval_id": id, "action": op.Action}); err != nil {
			writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "approval could not be recorded"))
			return true
		}
	}
	body := map[string]any{"approval_id": id, "status": "pending", "expires_at": exp}
	if err = saveMutation(tx, r, op.OrgID, body); err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return true
	}
	if err = tx.Commit(); err != nil {
		internalErr(w, err)
		return true
	}
	writeJSON(w, http.StatusAccepted, body)
	return true
}

// claimApproval marks a pending request applied in the same transaction as the
// secret write. A mismatch rolls the write back and leaves the request pending.
func (s *Service) claimApproval(tx *sql.Tx, w http.ResponseWriter, r *http.Request, op approvalOp) bool {
	ticket, ok := r.Context().Value(applyKey{}).(applyTicket)
	if !ok {
		return false
	}
	actor := auth.From(r)
	var enc string
	if err := tx.QueryRow(`SELECT dek_encrypted FROM orgs WHERE id=?`, op.OrgID).Scan(&enc); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return true
	}
	dek, err := s.DEKs.Get(s.MasterKey, op.OrgID, enc)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return true
	}
	mac, err := op.mac(dek)
	if err != nil {
		internalErr(w, err)
		return true
	}
	var action, key, folder, env, stored string
	err = tx.QueryRow(`UPDATE approval_requests SET status='applied', approver_id=?, payload_enc='', decided_at=datetime('now')
		WHERE id=? AND org_id=? AND status='pending' AND expires_at>? AND requester_id<>?
		RETURNING action, secret_key, folder, env_id, payload_mac`,
		actor.ID, ticket.ID, op.OrgID, time.Now().Unix(), actor.ID).Scan(&action, &key, &folder, &env, &stored)
	if err != nil {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return true
	}
	if action != op.Action || key != op.Key || folder != op.Folder || env != op.EnvID || !hmac.Equal([]byte(stored), []byte(mac)) {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval does not match this change"))
		return true
	}
	if err = auth.AuditReq(tx, r, op.OrgID, actor, "approval.approve", "approval/"+ticket.ID, map[string]any{"action": action, "key": key}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "approval could not be recorded"))
		return true
	}
	return false
}

// ApplyApproval checks the approver, then performs the stored change. The
// secret write and the status change commit together.
func (s *Service) ApplyApproval(w http.ResponseWriter, r *http.Request, orgID, id string) {
	actor := auth.From(r)
	if actor == nil || actor.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	role, _, ok := auth.MemberInfo(s.DB, orgID, actor.ID)
	if !ok || !rbac.Privileged(role) {
		auth.Deny(s.DB, w, r, orgID, "admin", "approval/"+id)
		return
	}
	var projectID, envID, folder, key, action, enc, metaRaw, requester, status string
	var expires int64
	err := s.DB.QueryRow(`SELECT project_id, env_id, folder, secret_key, action, payload_enc, meta_json, requester_id, status, expires_at FROM approval_requests WHERE id=? AND org_id=?`, id, orgID).
		Scan(&projectID, &envID, &folder, &key, &action, &enc, &metaRaw, &requester, &status, &expires)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if requester == actor.ID {
		writeErr(w, apperr.New(403, "SELF_APPROVAL", "a different administrator must approve this change"))
		return
	}
	if status != "pending" || expires <= time.Now().Unix() {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return
	}
	var meta approvalMeta
	if json.Unmarshal([]byte(metaRaw), &meta) != nil {
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return
	}
	var slug, envSlug string
	if err = s.DB.QueryRow(`SELECT slug FROM projects WHERE id=? AND org_id=?`, projectID, orgID).Scan(&slug); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if err = s.DB.QueryRow(`SELECT slug FROM environments WHERE id=? AND project_id=?`, envID, projectID).Scan(&envSlug); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	value := ""
	if action == "upsert" {
		dek, derr := s.orgDEK(orgID)
		if derr != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
			return
		}
		value, err = tc.Open(dek, enc, tc.Context{Purpose: "approval", Org: orgID, Project: projectID, Environment: envID, Object: id, Version: 1})
		if err != nil {
			writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval payload cannot be opened"))
			return
		}
	}
	ctx := project.With(r.Context(), project.Ctx{ID: projectID, OrgID: orgID, Slug: slug})
	ctx = context.WithValue(ctx, applyKey{}, applyTicket{ID: id})
	route := chi.NewRouteContext()
	route.URLParams.Add("key", key)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
	q := url.Values{"env": {envSlug}, "path": {folder}}
	if meta.Expected != nil {
		q.Set("expectedVersion", strconv.Itoa(*meta.Expected))
	}
	var raw []byte
	method := http.MethodPost
	switch action {
	case "upsert":
		body := map[string]any{"key": key, "value": value, "comment": meta.Comment, "tags": meta.Tags}
		if meta.Expected != nil {
			body["expectedVersion"] = *meta.Expected
		}
		raw, _ = json.Marshal(body)
	case "delete":
		method = http.MethodDelete
	case "rollback":
		raw, _ = json.Marshal(map[string]any{"version": meta.Rollback, "expectedVersion": meta.Expected})
	default:
		writeErr(w, apperr.New(409, "APPROVAL_CONFLICT", "approval is no longer pending"))
		return
	}
	req := r.Clone(ctx)
	req.Method = method
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.ContentLength = int64(len(raw))
	req.URL.RawQuery = q.Encode()
	req.Header.Del("Idempotency-Key")
	switch action {
	case "upsert":
		s.Upsert(w, req)
	case "delete":
		s.Delete(w, req)
	case "rollback":
		s.Rollback(w, req)
	}
}
