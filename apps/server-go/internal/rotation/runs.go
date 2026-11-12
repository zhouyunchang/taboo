package rotation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

func (s *Service) Runs(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "read")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, e := strconv.Atoi(raw); e == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	rows, err := s.DB.Query(`SELECT id FROM rotation_runs WHERE secret_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, secretID, limit)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	out := []Run{}
	for _, id := range ids {
		if run, err := s.runRow(id); err == nil {
			out = append(out, run)
		}
	}
	writeJSON(w, 200, map[string]any{"runs": out})
}

func (s *Service) RunDetail(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "read")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || (run.SecretID != "" && run.SecretID != secretID) {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	writeJSON(w, 200, run)
}

func (s *Service) RunNow(w http.ResponseWriter, r *http.Request) {
	p, folderID, norm, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, version, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	policy, err := s.loadPolicy(secretID, key)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation policy not found"))
		return
	}
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if policy.Status == statusFixed {
		writeErr(w, apperr.New(409, "ROTATION_FIXED", "fixed policies do not run"))
		return
	}
	if policy.Status != statusActive {
		writeErr(w, apperr.New(409, "ROTATION_NOT_ACTIVE", "policy is not active"))
		return
	}
	if _, err := s.openRun(secretID); err == nil {
		writeErr(w, apperr.New(409, "ROTATION_ALREADY_OPEN", "a rotation run is already open"))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	actor := auth.From(r)
	now := time.Now().Unix()
	runID, _, _, err := s.insertRun(tx, p, secretID, key, norm, policy, "manual", now, int64(version), actor)
	if err != nil {
		slog.Error("rotation run now failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, actor, "rotation.run.created", "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": runID, "trigger": "manual", "old_version": version}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	s.writeRun(w, runID)
}

func (s *Service) Generate(w http.ResponseWriter, r *http.Request) {
	p, folderID, norm, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "reveal", "") {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, latest, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	if run.NewVersion != nil || run.Status == "awaiting_distribution" {
		writeErr(w, apperr.New(409, "ROTATION_ALREADY_GENERATED", "this run already has a generated value"))
		return
	}
	if run.Status != "awaiting_generation" && run.Status != "overdue" {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "run is not awaiting generation"))
		return
	}
	policy, err := s.loadPolicy(secretID, key)
	if err != nil || policy.Generation == nil {
		writeErr(w, apperr.New(400, "ROTATION_GENERATION_PROFILE_INVALID", "policy has no generation profile"))
		return
	}
	if latest != run.OldVersion {
		writeErr(w, apperr.New(409, "ROTATION_VERSION_STALE", "secret changed before generation"))
		return
	}
	value, err := GenerateValue(*policy.Generation)
	if err != nil {
		writeErr(w, apperr.New(400, "ROTATION_GENERATION_PROFILE_INVALID", err.Error()))
		return
	}
	actor := auth.From(r)
	if s.Secrets == nil {
		writeErr(w, apperr.New(500, "INTERNAL", "rotation writer is not configured"))
		return
	}
	next, err := s.Secrets.WriteGeneratedVersion(r.Context(), p.OrgID, p.ID, norm, key, value,
		"rotation generated value", actor.ID, actor.Name, actor.Kind, run.OldVersion)
	if err != nil {
		switch {
		case errors.Is(err, secretsvc.ErrRotationApprovalRequired):
			writeErr(w, apperr.New(409, "ROTATION_APPROVAL_REQUIRED", "project approval is required; use the normal write flow"))
		case errors.Is(err, secretsvc.ErrRotationVersionConflict):
			writeErr(w, apperr.New(409, "ROTATION_VERSION_STALE", "secret changed before generation"))
		case errors.Is(err, secretsvc.ErrRotationSecretNotFound):
			writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		default:
			slog.Error("rotation generate failed", "err", err, "run", run.ID)
			writeErr(w, apperr.New(500, "INTERNAL", "generate failed"))
		}
		return
	}
	evidence := map[string]any{
		"generation_profile": policy.Generation.Profile,
		"generation_length":  policy.Generation.Length,
		"generation_entropy": policy.Generation.MinEntropyBits,
	}
	rawEvidence, _ := json.Marshal(evidence)
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE rotation_runs SET new_version=?, status='awaiting_distribution',
		evidence_json=?, started_at=datetime('now'), updated_at=datetime('now') WHERE id=? AND status='awaiting_generation'`,
		next, string(rawEvidence), run.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, actor, "rotation.run.generated", "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": run.ID, "old_version": run.OldVersion, "new_version": next,
			"profile": policy.Generation.Profile, "length": policy.Generation.Length, "entropy": policy.Generation.MinEntropyBits}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, map[string]any{"run_id": run.ID, "version": next, "value": value})
}

func (s *Service) Distribute(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, latest, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	if (run.Status != "awaiting_distribution" && run.Status != "overdue") || run.NewVersion == nil {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "run is not awaiting distribution"))
		return
	}
	if *run.NewVersion != latest {
		writeErr(w, apperr.New(409, "ROTATION_VERSION_STALE", "a newer version exists; inspect before completing"))
		return
	}
	var b struct {
		Note     string `json:"note"`
		Verified bool   `json:"verified"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&b)
	s.finishRun(w, r, p, key, run, run.NewVersion, "succeeded", b.Note, map[string]any{"verified": b.Verified}, "rotation.run.distributed")
}

func (s *Service) Complete(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, latest, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	if run.Status == "awaiting_distribution" || run.Status == "awaiting_generation" {
		writeErr(w, apperr.New(409, "ROTATION_DISTRIBUTION_REQUIRED", "generated runs must be distributed"))
		return
	}
	var b struct {
		NewVersion int    `json:"new_version"`
		Note       string `json:"note"`
		Verified   bool   `json:"verified"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&b); err != nil || b.NewVersion < 1 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.NewVersion <= run.OldVersion || b.NewVersion != latest {
		writeErr(w, apperr.New(409, "ROTATION_VERSION_STALE", "new_version must be the current version"))
		return
	}
	nv := b.NewVersion
	s.finishRun(w, r, p, key, run, &nv, "succeeded", b.Note, map[string]any{"verified": b.Verified, "new_version": b.NewVersion}, "rotation.run.completed")
}

func (s *Service) Fail(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&b); err != nil || strings.TrimSpace(b.Reason) == "" {
		writeErr(w, apperr.New(400, "ROTATION_REASON_REQUIRED", "reason is required"))
		return
	}
	s.finishRun(w, r, p, key, run, nil, "failed", b.Reason, nil, "rotation.run.failed")
}

func (s *Service) Skip(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&b); err != nil || strings.TrimSpace(b.Reason) == "" {
		writeErr(w, apperr.New(400, "ROTATION_REASON_REQUIRED", "reason is required"))
		return
	}
	s.finishRun(w, r, p, key, run, nil, "skipped", b.Reason, nil, "rotation.run.skipped")
}

// finishRun closes an open run and advances a due schedule if it was missed
// while the run was open.
func (s *Service) finishRun(w http.ResponseWriter, r *http.Request, p project.Ctx, key string, run Run, newVersion *int, status, note string, extra map[string]any, auditAction string) {
	if !isOpenStatus(run.Status) {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "run is not open"))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	evidence := run.Evidence
	if evidence == nil {
		evidence = map[string]any{}
	}
	for k, v := range extra {
		evidence[k] = v
	}
	if note != "" {
		evidence["note"] = note
	}
	raw, _ := json.Marshal(evidence)
	var nv any
	if newVersion != nil {
		nv = *newVersion
	}
	now := time.Now()
	res, err := tx.Exec(`UPDATE rotation_runs SET status=?, new_version=COALESCE(?, new_version), last_error=?, evidence_json=?, finished_at=datetime('now'), updated_at=datetime('now')
		WHERE id=? AND status IN (?,?,?,?,?,?)`, status, nv, note, string(raw), run.ID,
		openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5])
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "run is no longer open"))
		return
	}
	if run.PolicyID != "" {
		if err := s.advancePolicyAfterRun(tx, run.PolicyID, now); err != nil {
			slog.Error("rotation policy advance failed", "err", err, "policy", run.PolicyID)
		}
	}
	actor := auth.From(r)
	if err := auth.AuditReq(tx, r, p.OrgID, actor, auditAction, "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": run.ID, "old_version": run.OldVersion, "note": note, "status": status}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	eventAction := ""
	switch status {
	case "succeeded":
		eventAction = "rotation_succeeded"
	case "failed":
		eventAction = "rotation_failed"
	case "skipped":
		eventAction = "rotation_skipped"
	}
	if eventAction != "" {
		version := run.OldVersion
		if newVersion != nil {
			version = *newVersion
		}
		e := events.SecretEvent{
			EventID: tc.NewID(), OrgID: p.OrgID, ProjectID: p.ID, ProjectSlug: p.Slug,
			Folder: run.Folder, Key: run.Key, Version: version, Action: eventAction,
			ActorID: actor.ID, ActorName: actor.Name, ActorKind: actor.Kind, SecretID: run.SecretID,
			RunID: run.ID, PolicyID: run.PolicyID, Mode: "", OldVersion: run.OldVersion,
			DueAt: run.DueAt, GraceUntil: run.GraceUntil,
		}
		if err := outbox.Insert(tx, e); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	s.writeRun(w, run.ID)
}

func isOpenStatus(status string) bool {
	for _, s := range openRunStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func (s *Service) advancePolicyAfterRun(tx *sql.Tx, policyID string, now time.Time) error {
	var interval int
	var cron, status string
	var next int64
	if err := tx.QueryRow(`SELECT interval_seconds, cron, status, next_run_at FROM rotation_policies WHERE id=?`, policyID).
		Scan(&interval, &cron, &status, &next); err != nil {
		return err
	}
	if status != statusActive {
		return nil
	}
	if next > now.Unix() {
		return nil
	}
	n, err := nextRun(now, interval, cron)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE rotation_policies SET next_run_at=?, updated_at=datetime('now') WHERE id=?`, n.Unix(), policyID)
	return err
}

func (s *Service) Notify(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	if !isOpenStatus(run.Status) {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "run is not open"))
		return
	}
	policy, err := s.loadPolicy(secretID, key)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	eventID := tc.NewID()
	e := events.SecretEvent{
		EventID: eventID, OrgID: p.OrgID, ProjectID: p.ID, ProjectSlug: p.Slug,
		Folder: run.Folder, Key: run.Key, Version: run.OldVersion, Action: "rotation_due",
		ActorID: "system", ActorName: "rotation", ActorKind: "system", SecretID: run.SecretID,
		RunID: run.ID, PolicyID: run.PolicyID, Mode: policy.Mode, OldVersion: run.OldVersion,
		DueAt: run.DueAt, GraceUntil: run.GraceUntil,
	}
	if err := outbox.Insert(tx, e); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if _, err := tx.Exec(`UPDATE rotation_runs SET attempts=attempts+1, last_error='', updated_at=datetime('now') WHERE id=?`, run.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, auth.From(r), "rotation.run.notified", "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": run.ID, "event_id": eventID}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	s.writeRun(w, run.ID)
}

func (s *Service) Retry(w http.ResponseWriter, r *http.Request) {
	p, folderID, norm, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, version, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	run, err := s.runRow(parseRunID(r))
	if err != nil || run.SecretID != secretID || run.Status != "failed" {
		writeErr(w, apperr.New(409, "ROTATION_RUN_NOT_OPEN", "only failed runs can be retried"))
		return
	}
	policy, err := s.loadPolicy(secretID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation policy not found"))
		return
	}
	if policy.Status == statusFixed {
		writeErr(w, apperr.New(409, "ROTATION_FIXED", "fixed policies do not retry"))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	newID, _, _, err := s.insertRun(tx, p, secretID, key, norm, policy, "retry", time.Now().Unix(), int64(version), auth.From(r))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, auth.From(r), "rotation.run.retried", "project/"+p.Slug+"/secret/"+key,
		map[string]any{"run_id": newID, "previous_run_id": run.ID}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	s.writeRun(w, newID)
}
