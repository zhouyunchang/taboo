// 轮换策略：到期把 secret.rotation_due 写入 outbox，由 webhook 通知外部轮换器回写新版本。
// taboo 不生成新明文。
package rotation

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

type Service struct {
	DB *sql.DB
}

func (s *Service) WithDB(db *sql.DB) *Service { return &Service{DB: db} }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func (s *Service) Routes(r chi.Router) {
	r.Route("/secrets/{key}/rotation", func(r chi.Router) {
		r.Get("/", s.Get)
		r.Put("/", s.Put)
		r.Delete("/", s.Delete)
	})
}

type policy struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Interval  int    `json:"interval_seconds"`
	Cron      string `json:"cron"`
	Enabled   bool   `json:"enabled"`
	NextRun   int64  `json:"next_run_at"`
	LastRun   int64  `json:"last_run_at"`
	LastEvent string `json:"last_event_id"`
}

func (s *Service) locate(w http.ResponseWriter, r *http.Request, action string) (project.Ctx, string, string, string, string, bool) {
	p := project.Of(r)
	envSlug := r.URL.Query().Get("env")
	if envSlug == "" {
		envSlug = "dev"
	}
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, action, envSlug) {
		return p, "", "", "", "", false
	}
	var envID string
	if err := s.DB.QueryRow(`SELECT id FROM environments WHERE project_id = ? AND slug = ?`, p.ID, envSlug).Scan(&envID); err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "env not found"))
		return p, "", "", "", "", false
	}
	norm, err := folder.NormalizePath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "invalid folder path"))
		return p, "", "", "", "", false
	}
	if norm == "" {
		norm = "/"
	}
	folderID, err := folder.ResolveID(s.DB, envID, norm, false)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return p, "", "", "", "", false
	}
	return p, envSlug, envID, folderID, norm, true
}

func (s *Service) secretOf(envID, folderID, key string) (id string, version int, err error) {
	err = s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`, envID, folderID, key).Scan(&id, &version)
	return
}

func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
	_, _, envID, folderID, _, ok := s.locate(w, r, "read")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(envID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	item, err := s.load(secretID, key)
	if err == sql.ErrNoRows {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation policy not found"))
		return
	}
	if err != nil {
		slog.Error("rotation load failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, item)
}

func (s *Service) load(secretID, key string) (policy, error) {
	var item policy
	var enabled int
	err := s.DB.QueryRow(`SELECT id, interval_seconds, cron, enabled, next_run_at, last_run_at, last_event_id FROM rotation_policies WHERE secret_id = ?`, secretID).
		Scan(&item.ID, &item.Interval, &item.Cron, &enabled, &item.NextRun, &item.LastRun, &item.LastEvent)
	item.Key = key
	item.Enabled = enabled == 1
	return item, err
}

func (s *Service) Put(w http.ResponseWriter, r *http.Request) {
	p, envSlug, envID, folderID, norm, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	var b struct {
		Interval int    `json:"interval_seconds"`
		Cron     string `json:"cron"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	b.Cron = strings.TrimSpace(b.Cron)
	if err := validatePolicy(b.Interval, b.Cron); err != nil {
		writeErr(w, apperr.New(400, "INVALID", err.Error()))
		return
	}
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	secretID, version, err := s.secretOf(envID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	next, err := nextRun(time.Now(), b.Interval, b.Cron)
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "could not compute the next run"))
		return
	}
	u := auth.From(r)
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	id := tc.NewID()
	flag := 0
	if enabled {
		flag = 1
	}
	if _, err = tx.Exec(`INSERT INTO rotation_policies (id, org_id, project_id, secret_id, interval_seconds, cron, enabled, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(secret_id) DO UPDATE SET interval_seconds=excluded.interval_seconds, cron=excluded.cron, enabled=excluded.enabled, next_run_at=excluded.next_run_at, updated_at=datetime('now')`,
		id, p.OrgID, p.ID, secretID, b.Interval, b.Cron, flag, next.Unix()); err != nil {
		slog.Error("rotation save failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if err = auth.AuditReq(tx, r, p.OrgID, u, "secrets.rotation.update", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"interval_seconds": b.Interval, "cron": b.Cron, "enabled": enabled, "version": version, "folder": norm, "next_run_at": next.Unix()}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		slog.Error("rotation commit failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	item, err := s.load(secretID, key)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, item)
}

func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	p, envSlug, envID, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	secretID, _, err := s.secretOf(envID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	u := auth.From(r)
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM rotation_policies WHERE secret_id = ? AND org_id = ?`, secretID, p.OrgID)
	if err != nil {
		slog.Error("rotation delete failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation policy not found"))
		return
	}
	if err = auth.AuditReq(tx, r, p.OrgID, u, "secrets.rotation.delete", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key, nil); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "deleted": true})
}

func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	go func() {
		timer := time.NewTicker(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.Sweep(ctx)
			}
		}
	}()
}

// Sweep 认领到期策略，在同一事务里写 outbox 与审计，然后把下次时间推后。
func (s *Service) Sweep(ctx context.Context) {
	if s == nil || s.DB == nil || ctx.Err() != nil {
		return
	}
	now := time.Now().Unix()
	rows, err := s.DB.Query(`SELECT id FROM rotation_policies WHERE enabled = 1 AND next_run_at <= ? AND lease_until <= ? ORDER BY next_run_at, id LIMIT 32`, now, now)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := s.fire(id); err != nil {
			slog.Error("rotation fire failed", "id", id, "err", err)
		}
	}
}

func (s *Service) fire(id string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	claim := tc.NewID()
	now := time.Now().Unix()
	res, err := tx.Exec(`UPDATE rotation_policies SET lease_token=?, lease_until=? WHERE id=? AND enabled=1 AND next_run_at<=? AND lease_until<=?`, claim, now+60, id, now, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return nil
	}
	var orgID, projectID, slug, envSlug, path, key, cron string
	var secretID, envID string
	var version, interval int
	var enabled int
	err = tx.QueryRow(`SELECT p.org_id, p.project_id, pr.slug, e.slug, e.id, f.path, s.key, s.latest_version, s.id, p.interval_seconds, p.cron, p.enabled
		FROM rotation_policies p
		JOIN secrets s ON s.id = p.secret_id
		JOIN environments e ON e.id = s.env_id
		JOIN projects pr ON pr.id = p.project_id
		JOIN folders f ON f.id = s.folder_id
		WHERE p.id = ? AND p.lease_token = ?`, id, claim).
		Scan(&orgID, &projectID, &slug, &envSlug, &envID, &path, &key, &version, &secretID, &interval, &cron, &enabled)
	if err != nil {
		return err
	}
	next, err := nextRun(time.Unix(now, 0), interval, cron)
	if err != nil {
		next = time.Unix(now, 0).Add(24 * time.Hour)
	}
	eventID := tc.NewID()
	event := events.SecretEvent{
		EventID: eventID, OrgID: orgID, ProjectID: projectID, ProjectSlug: slug,
		EnvSlug: envSlug, Folder: path, Key: key, Version: version, Action: "rotation_due",
		ActorID: "system", ActorName: "rotation", ActorKind: "system",
		SecretID: secretID, EnvironmentID: envID,
	}
	if err = outbox.Insert(tx, event); err != nil {
		return err
	}
	if err = auth.Audit(tx, orgID, &auth.Actor{ID: "system", Name: "rotation", Kind: "system"}, "secrets.rotation.due", "project/"+slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"version": version, "interval_seconds": interval, "cron": cron, "event_id": eventID}, ""); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE rotation_policies SET next_run_at=?, last_run_at=?, last_event_id=?, lease_token='', lease_until=0, updated_at=datetime('now') WHERE id=? AND lease_token=?`,
		next.Unix(), now, eventID, id, claim)
	if err != nil {
		return err
	}
	return tx.Commit()
}
