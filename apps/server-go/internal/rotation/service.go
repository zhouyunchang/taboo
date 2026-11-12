// 轮换策略：编排、轮次、生成与通知。taboo 不替外部真源凭空生成它不接受的明文。
package rotation

import (
	"context"
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
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

type SecretWriter interface {
	WriteGeneratedVersion(ctx context.Context, orgID, projectID, folderPath, key, value, reason, actorID, actorName, actorKind string, expected int) (int, error)
}

type Service struct {
	DB      *sql.DB
	Secrets SecretWriter

	WorkerID string
	Role     string
	Version  string
}

type Owner struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RunSummary struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	DueAt  int64  `json:"due_at"`
}

type Policy struct {
	ID               string      `json:"id"`
	Key              string      `json:"key"`
	Folder           string      `json:"folder"`
	Status           string      `json:"status"`
	Enabled          bool        `json:"enabled"`
	Mode             string      `json:"mode"`
	Interval         int         `json:"interval_seconds"`
	Cron             string      `json:"cron"`
	Owner            *Owner      `json:"owner,omitempty"`
	Grace            int         `json:"grace_seconds"`
	NextRun          int64       `json:"next_run_at"`
	LastRun          int64       `json:"last_run_at"`
	LastRunID        string      `json:"last_run_id"`
	FixedReason      string      `json:"fixed_reason,omitempty"`
	FixedBy          string      `json:"fixed_by,omitempty"`
	FixedByName      string      `json:"fixed_by_name,omitempty"`
	FixedAt          string      `json:"fixed_at,omitempty"`
	Generation       *Generation `json:"generation,omitempty"`
	AutoGenerate     bool        `json:"auto_generate"`
	OpenRun          *RunSummary `json:"open_run,omitempty"`
	CreatedAt        string      `json:"created_at"`
	UpdatedAt        string      `json:"updated_at"`
	Version          int         `json:"version"`
	OwnerUnavailable bool        `json:"owner_unavailable,omitempty"`
}

type Run struct {
	ID             string         `json:"id"`
	PolicyID       string         `json:"policy_id,omitempty"`
	SecretID       string         `json:"secret_id,omitempty"`
	Key            string         `json:"key"`
	Folder         string         `json:"folder"`
	PolicySnapshot string         `json:"policy_snapshot,omitempty"`
	Trigger        string         `json:"trigger"`
	Status         string         `json:"status"`
	DueAt          int64          `json:"due_at"`
	GraceUntil     int64          `json:"grace_until"`
	OldVersion     int            `json:"old_version"`
	NewVersion     *int           `json:"new_version"`
	OwnerID        string         `json:"owner_id,omitempty"`
	OwnerName      string         `json:"owner_name,omitempty"`
	ActorID        string         `json:"actor_id,omitempty"`
	ActorKind      string         `json:"actor_kind,omitempty"`
	Attempts       int            `json:"attempts"`
	LastError      string         `json:"last_error,omitempty"`
	Evidence       map[string]any `json:"evidence,omitempty"`
	CreatedAt      string         `json:"created_at"`
	StartedAt      string         `json:"started_at,omitempty"`
	FinishedAt     string         `json:"finished_at,omitempty"`
	UpdatedAt      string         `json:"updated_at"`
}

const (
	statusDraft  = "draft"
	statusActive = "active"
	statusPaused = "paused"
	statusFixed  = "fixed"

	modeGenerate = "generate"
	modeManual   = "manual"
	modeWebhook  = "webhook"

	defaultGrace = 7 * 24 * 3600
	maxGrace     = 30 * 24 * 3600
)

var openRunStatuses = []string{"due", "awaiting_generation", "awaiting_distribution", "awaiting_manual", "awaiting_external", "overdue"}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func (s *Service) Routes(r chi.Router) {
	r.Get("/rotations", s.ListProject)
	r.Route("/secrets/{key}/rotation", func(r chi.Router) {
		r.Get("/", s.Get)
		r.Put("/", s.Put)
		r.Delete("/", s.Delete)
		r.Post("/preview", s.Preview)
		r.Post("/run", s.RunNow)
		r.Get("/runs", s.Runs)
		r.Get("/runs/{run_id}", s.RunDetail)
		r.Post("/runs/{run_id}/generate", s.Generate)
		r.Post("/runs/{run_id}/distribute", s.Distribute)
		r.Post("/runs/{run_id}/complete", s.Complete)
		r.Post("/runs/{run_id}/fail", s.Fail)
		r.Post("/runs/{run_id}/skip", s.Skip)
		r.Post("/runs/{run_id}/notify", s.Notify)
		r.Post("/runs/{run_id}/retry", s.Retry)
	})
}

func (s *Service) OrgRoutes(r chi.Router) {
	r.Get("/orgs/{slug}/rotation/health", s.Health)
}

func (s *Service) locate(w http.ResponseWriter, r *http.Request, action string) (project.Ctx, string, string, bool) {
	p := project.Of(r)
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, action, "") {
		return p, "", "", false
	}
	norm, err := folder.NormalizePath(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "invalid folder path"))
		return p, "", "", false
	}
	if norm == "" {
		norm = "/"
	}
	folderID, err := folder.ResolveID(s.DB, p.ID, norm, false)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return p, "", "", false
	}
	return p, folderID, norm, true
}

func (s *Service) secretOf(projectID, folderID, key string) (id string, version int, err error) {
	err = s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE project_id = ? AND folder_id = ? AND key = ?`, projectID, folderID, key).Scan(&id, &version)
	return
}

func (s *Service) Get(w http.ResponseWriter, r *http.Request) {
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
	item, err := s.loadPolicy(secretID, key)
	if errors.Is(err, sql.ErrNoRows) {
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

func (s *Service) loadPolicy(secretID, key string) (Policy, error) {
	var p Policy
	var auto int
	var ownerID, ownerName, fixedBy, fixedByName string
	var profile, prefix string
	var genLength int
	err := s.DB.QueryRow(`SELECT p.id, p.interval_seconds, p.cron, p.mode, p.status,
		p.owner_user_id, COALESCE(u.name, ''), p.grace_seconds, p.next_run_at, p.last_run_at,
		p.last_run_id, p.fixed_reason, p.fixed_by, COALESCE(u2.name, ''), p.fixed_at,
		p.generation_profile, p.generation_length, p.generation_prefix, p.auto_generate,
		p.created_at, p.updated_at, p.version
		FROM rotation_policies p
		LEFT JOIN users u ON u.id = p.owner_user_id
		LEFT JOIN users u2 ON u2.id = p.fixed_by
		WHERE p.secret_id = ?`, secretID).
		Scan(&p.ID, &p.Interval, &p.Cron, &p.Mode, &p.Status, &ownerID, &ownerName,
			&p.Grace, &p.NextRun, &p.LastRun, &p.LastRunID, &p.FixedReason, &fixedBy,
			&fixedByName, &p.FixedAt, &profile, &genLength, &prefix, &auto,
			&p.CreatedAt, &p.UpdatedAt, &p.Version)
	if err != nil {
		return p, err
	}
	p.Key = key
	p.Enabled = p.Status == statusActive
	p.Owner = &Owner{ID: ownerID, Name: ownerName}
	p.FixedBy = fixedBy
	p.FixedByName = fixedByName
	p.AutoGenerate = auto == 1
	if profile != "" {
		g := Generation{Profile: profile, Length: genLength, Prefix: prefix}
		g.MinEntropyBits = generationEntropy(profile, genLength)
		p.Generation = &g
	}
	if summary, err := s.openRun(secretID); err == nil {
		p.OpenRun = summary
	}
	return p, nil
}

func (s *Service) openRun(secretID string) (*RunSummary, error) {
	var x RunSummary
	err := s.DB.QueryRow(`SELECT id, status, due_at FROM rotation_runs
		WHERE secret_id = ? AND status IN (?,?,?,?,?,?)
		ORDER BY created_at DESC LIMIT 1`, secretID,
		openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5]).
		Scan(&x.ID, &x.Status, &x.DueAt)
	if err != nil {
		return nil, err
	}
	return &x, nil
}

func (s *Service) Put(w http.ResponseWriter, r *http.Request) {
	p, folderID, _, ok := s.locate(w, r, "write")
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	var b struct {
		Status          string      `json:"status"`
		Enabled         *bool       `json:"enabled"`
		Mode            string      `json:"mode"`
		Interval        int         `json:"interval_seconds"`
		Cron            string      `json:"cron"`
		OwnerID         string      `json:"owner_id"`
		Grace           *int        `json:"grace_seconds"`
		Generation      *Generation `json:"generation"`
		AutoGenerate    *bool       `json:"auto_generate"`
		FixedReason     string      `json:"fixed_reason"`
		ExpectedVersion *int        `json:"expected_version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	status := strings.TrimSpace(b.Status)
	if status == "" && b.Enabled != nil {
		if *b.Enabled {
			status = statusActive
		} else {
			status = statusPaused
		}
	}
	switch status {
	case statusDraft, statusActive, statusPaused, statusFixed:
	default:
		writeErr(w, apperr.New(400, "ROTATION_STATUS_REQUIRED", "status is required"))
		return
	}
	mode := strings.TrimSpace(b.Mode)
	if status == statusFixed {
		mode = ""
	} else if mode == "" {
		mode = modeManual
	}
	if mode != "" && mode != modeManual && mode != modeWebhook && mode != modeGenerate {
		writeErr(w, apperr.New(400, "INVALID", "invalid rotation mode"))
		return
	}
	b.Cron = strings.TrimSpace(b.Cron)
	if status == statusFixed {
		if strings.TrimSpace(b.FixedReason) == "" {
			writeErr(w, apperr.New(400, "ROTATION_FIXED_REASON_REQUIRED", "a reason is required to fix a secret"))
			return
		}
		b.Interval = 0
		b.Cron = ""
	} else if b.Interval != 0 || b.Cron != "" {
		if err := validatePolicy(b.Interval, b.Cron); err != nil {
			writeErr(w, apperr.New(400, "INVALID", err.Error()))
			return
		}
	} else if status == statusActive {
		writeErr(w, apperr.New(400, "ROTATION_SCHEDULE_REQUIRED", "active policies need a schedule"))
		return
	}
	grace := defaultGrace
	if b.Grace != nil {
		grace = *b.Grace
	}
	if grace < 0 || grace > maxGrace {
		writeErr(w, apperr.New(400, "INVALID", "grace_seconds out of range"))
		return
	}
	ownerID := strings.TrimSpace(b.OwnerID)
	actor := auth.From(r)
	if ownerID == "" && actor != nil && actor.Kind == auth.KindUser {
		ownerID = actor.ID
	}
	if status == statusActive && ownerID == "" {
		writeErr(w, apperr.New(400, "ROTATION_OWNER_REQUIRED", "active policies need an owner"))
		return
	}
	if ownerID != "" && status == statusActive {
		if _, _, ok := auth.MemberInfo(s.DB, p.OrgID, ownerID); !ok {
			writeErr(w, apperr.New(400, "INVALID", "owner is not a member of this organization"))
			return
		}
		ownerActor := &auth.Actor{ID: ownerID, Kind: auth.KindUser}
		if !auth.Can(s.DB, ownerActor, p.OrgID, p.ID, "write", "") {
			writeErr(w, apperr.New(400, "INVALID", "owner does not have write access to this project"))
			return
		}
		if mode == modeGenerate && !auth.Can(s.DB, ownerActor, p.OrgID, p.ID, "reveal", "") {
			writeErr(w, apperr.New(400, "ROTATION_REVEAL_REQUIRED", "the owner needs reveal access for generated rotation"))
			return
		}
	}
	var generation *Generation
	auto := false
	if mode == modeGenerate {
		var err error
		var g Generation
		if b.Generation != nil {
			g = *b.Generation
		}
		g, err = NormalizeGeneration(g)
		if err != nil {
			writeErr(w, apperr.New(400, "ROTATION_GENERATION_PROFILE_INVALID", err.Error()))
			return
		}
		generation = &g
		if b.AutoGenerate != nil {
			auto = *b.AutoGenerate
		}
	}
	next := int64(0)
	if status == statusActive {
		n, err := nextRun(time.Now(), b.Interval, b.Cron)
		if err != nil {
			writeErr(w, apperr.New(400, "INVALID", "could not compute the next run"))
			return
		}
		next = n.Unix()
	}
	secretID, _, err := s.secretOf(p.ID, folderID, key)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "secret not found"))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	var id string
	var currentVersion int
	var lastEvent, leaseToken string
	var lastRun, leaseUntil int64
	err = tx.QueryRow(`SELECT id, version, last_event_id, lease_token, lease_until, last_run_at
		FROM rotation_policies WHERE secret_id = ?`, secretID).
		Scan(&id, &currentVersion, &lastEvent, &leaseToken, &leaseUntil, &lastRun)
	if errors.Is(err, sql.ErrNoRows) {
		id = tc.NewID()
		currentVersion = 0
	} else if err != nil {
		slog.Error("rotation load failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if b.ExpectedVersion != nil && *b.ExpectedVersion != currentVersion {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "rotation policy changed; reload before retrying"))
		return
	}
	fixedBy := ""
	fixedAt := ""
	fixedReason := ""
	if status == statusFixed {
		fixedReason = strings.TrimSpace(b.FixedReason)
		if actor != nil {
			fixedBy = actor.ID
			fixedAt = time.Now().UTC().Format("2006-01-02 15:04:05")
		}
	}
	enabled := 0
	if status == statusActive {
		enabled = 1
	}
	genProfile, genLength, genPrefix := "", 0, ""
	if generation != nil {
		genProfile, genLength, genPrefix = generation.Profile, generation.Length, generation.Prefix
	}
	if _, err = tx.Exec(`INSERT INTO rotation_policies (
		id, org_id, project_id, secret_id, interval_seconds, cron, enabled, next_run_at,
		last_run_at, last_event_id, lease_token, lease_until, created_at, updated_at,
		created_by, mode, owner_user_id, grace_seconds, status, fixed_reason, fixed_by, fixed_at,
		generation_profile, generation_length, generation_prefix, auto_generate, last_run_id, version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,datetime('now'),datetime('now'),?,?,?,?,?,?,?,?,?,?,?,?, '', 1)
		ON CONFLICT(secret_id) DO UPDATE SET
			interval_seconds=excluded.interval_seconds, cron=excluded.cron, enabled=excluded.enabled,
			next_run_at=excluded.next_run_at, updated_at=datetime('now'), mode=excluded.mode,
			owner_user_id=excluded.owner_user_id, grace_seconds=excluded.grace_seconds,
			status=excluded.status, fixed_reason=excluded.fixed_reason, fixed_by=excluded.fixed_by,
			fixed_at=excluded.fixed_at, generation_profile=excluded.generation_profile,
			generation_length=excluded.generation_length, generation_prefix=excluded.generation_prefix,
			auto_generate=excluded.auto_generate, version=rotation_policies.version+1`,
		id, p.OrgID, p.ID, secretID, b.Interval, b.Cron, enabled, next,
		lastRun, lastEvent, leaseToken, leaseUntil,
		actor.ID, mode, ownerID, grace, status, fixedReason, fixedBy, fixedAt,
		genProfile, genLength, genPrefix, boolInt(auto)); err != nil {
		slog.Error("rotation save failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	if status == statusFixed {
		if _, err := tx.Exec(`UPDATE rotation_runs SET status='canceled', last_error='policy_fixed',
			finished_at=datetime('now'), updated_at=datetime('now') WHERE secret_id = ? AND status IN (?,?,?,?,?,?)`,
			secretID, openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5]); err != nil {
			slog.Error("rotation cancel failed", "err", err)
			writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
			return
		}
	}
	meta := map[string]any{"status": status, "mode": mode, "next_run_at": next, "owner": ownerID, "grace_seconds": grace}
	if generation != nil {
		meta["generation_profile"] = generation.Profile
		meta["generation_entropy"] = generation.MinEntropyBits
	}
	if err := auth.AuditReq(tx, r, p.OrgID, actor, "secrets.rotation.update", "project/"+p.Slug+"/secret/"+key, meta); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("rotation commit failed", "err", err)
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	item, err := s.loadPolicy(secretID, key)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, item)
}

func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
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
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE rotation_runs SET status='canceled', last_error='policy_deleted',
		finished_at=datetime('now'), updated_at=datetime('now') WHERE secret_id = ? AND status IN (?,?,?,?,?,?)`,
		secretID, openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5]); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	res, err := tx.Exec(`DELETE FROM rotation_policies WHERE secret_id = ? AND org_id = ?`, secretID, p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation policy not found"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, auth.From(r), "secrets.rotation.delete", "project/"+p.Slug+"/secret/"+key, nil); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "deleted": true})
}

func (s *Service) Preview(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := s.locate(w, r, "read"); !ok {
		return
	}
	var b struct {
		Interval int    `json:"interval_seconds"`
		Cron     string `json:"cron"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	b.Cron = strings.TrimSpace(b.Cron)
	if err := validatePolicy(b.Interval, b.Cron); err != nil {
		writeErr(w, apperr.New(400, "INVALID", err.Error()))
		return
	}
	next := make([]string, 0, 3)
	local := make([]string, 0, 3)
	t := time.Now()
	for i := 0; i < 3; i++ {
		n, err := nextRun(t, b.Interval, b.Cron)
		if err != nil {
			writeErr(w, apperr.New(400, "INVALID", "could not compute the next run"))
			return
		}
		next = append(next, n.UTC().Format(time.RFC3339))
		local = append(local, n.Local().Format("2006-01-02 15:04"))
		t = n
	}
	writeJSON(w, 200, map[string]any{"next": next, "local": local, "timezone": time.Now().Location().String()})
}

func (s *Service) ListProject(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "read", "") {
		return
	}
	type policyRef struct{ secretID, key, path string }
	refs := []policyRef{}
	rows, err := s.DB.Query(`SELECT s.id, s.key, f.path FROM rotation_policies p
		JOIN secrets s ON s.id = p.secret_id
		JOIN folders f ON f.id = s.folder_id
		WHERE s.project_id = ? ORDER BY f.path, s.key`, p.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
		return
	}
	for rows.Next() {
		var ref policyRef
		if rows.Scan(&ref.secretID, &ref.key, &ref.path) == nil {
			refs = append(refs, ref)
		}
	}
	rows.Close()
	items := []Policy{}
	for _, ref := range refs {
		item, err := s.loadPolicy(ref.secretID, ref.key)
		if err != nil {
			continue
		}
		item.Folder = ref.path
		items = append(items, item)
	}
	counts := map[string]int{}
	if rows, err := s.DB.Query(`SELECT p.status, COUNT(*) FROM rotation_policies p
		JOIN secrets s ON s.id=p.secret_id WHERE s.project_id=? GROUP BY p.status`, p.ID); err == nil {
		for rows.Next() {
			var status string
			var n int
			if rows.Scan(&status, &n) == nil {
				counts[status] = n
			}
		}
		rows.Close()
	}
	var unset int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM secrets s LEFT JOIN rotation_policies p ON p.secret_id=s.id
		WHERE s.project_id=? AND p.id IS NULL`, p.ID).Scan(&unset)
	writeJSON(w, 200, map[string]any{"policies": items, "counts": counts, "unset": unset})
}

func (s *Service) Health(w http.ResponseWriter, r *http.Request) {
	actor := auth.From(r)
	if actor == nil {
		writeErr(w, apperr.Unauthorized)
		return
	}
	var orgID string
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, chi.URLParam(r, "slug")).Scan(&orgID); err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "organization not found"))
		return
	}
	if _, ok := auth.Membership(s.DB, orgID, actor.ID); !ok {
		auth.Deny(s.DB, w, r, orgID, "read", "org/"+chi.URLParam(r, "slug")+"/rotation")
		return
	}
	var lastSeen, lastSweep int64
	var workerID, role, version string
	err := s.DB.QueryRow(`SELECT worker_id, role, version, last_seen_at, last_sweep_at
		FROM worker_heartbeats ORDER BY last_seen_at DESC LIMIT 1`).
		Scan(&workerID, &role, &version, &lastSeen, &lastSweep)
	now := time.Now().Unix()
	online := err == nil && now-lastSeen < 90
	health := "offline"
	if online && now-lastSeen < 30 {
		health = "online"
	} else if online {
		health = "slow"
	}
	var active, open, overdue, failed int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM rotation_policies p JOIN secrets s ON s.id=p.secret_id
		WHERE s.project_id IN (SELECT id FROM projects WHERE org_id=?) AND p.status='active'`, orgID).Scan(&active)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM rotation_runs WHERE org_id=? AND status IN (?,?,?,?,?,?)`, orgID,
		openRunStatuses[0], openRunStatuses[1], openRunStatuses[2], openRunStatuses[3], openRunStatuses[4], openRunStatuses[5]).Scan(&open)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM rotation_runs WHERE org_id=? AND status='overdue'`, orgID).Scan(&overdue)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM rotation_runs WHERE org_id=? AND status='failed'`, orgID).Scan(&failed)
	writeJSON(w, 200, map[string]any{
		"online": online, "status": health, "worker_id": workerID, "role": role,
		"version": version, "last_seen_at": lastSeen, "last_sweep_at": lastSweep,
		"next_sweep_at": lastSweep + 15, "active_policies": active, "open_runs": open,
		"overdue_runs": overdue, "failed_runs": failed,
	})
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Service) runRow(runID string) (Run, error) {
	var x Run
	var newVersion sql.NullInt64
	var evidence string
	err := s.DB.QueryRow(`SELECT id, COALESCE(policy_id,''), COALESCE(secret_id,''), secret_key, folder_path,
		policy_snapshot, trigger, status, due_at, grace_until, old_version, new_version,
		owner_user_id, actor_id, actor_kind, attempts, last_error, evidence_json,
		created_at, started_at, finished_at, updated_at
		FROM rotation_runs WHERE id = ?`, runID).
		Scan(&x.ID, &x.PolicyID, &x.SecretID, &x.Key, &x.Folder, &x.PolicySnapshot, &x.Trigger,
			&x.Status, &x.DueAt, &x.GraceUntil, &x.OldVersion, &newVersion, &x.OwnerID,
			&x.ActorID, &x.ActorKind, &x.Attempts, &x.LastError, &evidence,
			&x.CreatedAt, &x.StartedAt, &x.FinishedAt, &x.UpdatedAt)
	if err != nil {
		return x, err
	}
	if newVersion.Valid {
		n := int(newVersion.Int64)
		x.NewVersion = &n
	}
	_ = json.Unmarshal([]byte(evidence), &x.Evidence)
	var name string
	_ = s.DB.QueryRow(`SELECT COALESCE(name,'') FROM users WHERE id=?`, x.OwnerID).Scan(&name)
	x.OwnerName = name
	return x, nil
}

func (s *Service) writeRun(w http.ResponseWriter, runID string) {
	run, err := s.runRow(runID)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "rotation run not found"))
		return
	}
	writeJSON(w, 200, run)
}

func parseRunID(r *http.Request) string { return chi.URLParam(r, "run_id") }

func (s *Service) insertRun(tx *sql.Tx, p project.Ctx, secretID, key, folderPath string, policy Policy, trigger string, dueAt, oldVersion int64, actor *auth.Actor) (string, string, string, error) {
	status := "awaiting_manual"
	switch policy.Mode {
	case modeGenerate:
		status = "awaiting_generation"
	case modeWebhook:
		status = "awaiting_external"
	}
	id := tc.NewID()
	runKey := id
	if trigger == "schedule" {
		runKey = policy.ID + "|" + strconv.FormatInt(dueAt, 10)
	}
	snapshot, _ := json.Marshal(map[string]any{"mode": policy.Mode, "interval_seconds": policy.Interval, "cron": policy.Cron})
	actorID, actorKind, actorName := "system", "system", "rotation"
	if actor != nil {
		actorID, actorKind = actor.ID, actor.Kind
		if actor.Name != "" {
			actorName = actor.Name
		}
	}
	grace := policy.Grace
	if grace < 0 {
		grace = 0
	}
	graceUntil := dueAt + int64(grace)
	_, err := tx.Exec(`INSERT INTO rotation_runs (id, org_id, project_id, policy_id, secret_id, secret_key, folder_path,
		policy_snapshot, run_key, trigger, status, due_at, grace_until, old_version, owner_user_id,
		actor_id, actor_kind, evidence_json, created_at, started_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?, '{}', datetime('now'), datetime('now'), datetime('now'))`,
		id, p.OrgID, p.ID, policy.ID, secretID, key, folderPath, string(snapshot), runKey,
		trigger, status, dueAt, graceUntil, oldVersion, policy.Owner.ID, actorID, actorKind)
	if err != nil {
		return "", "", "", err
	}
	eventID := tc.NewID()
	e := events.SecretEvent{
		EventID: eventID, OrgID: p.OrgID, ProjectID: p.ID, ProjectSlug: p.Slug,
		Folder: folderPath, Key: key, Version: int(oldVersion), Action: "rotation_due",
		ActorID: actorID, ActorName: actorName, ActorKind: actorKind, SecretID: secretID,
		RunID: id, PolicyID: policy.ID, Mode: policy.Mode, OldVersion: int(oldVersion),
		DueAt: dueAt, GraceUntil: graceUntil,
	}
	if err := outbox.Insert(tx, e); err != nil {
		return "", "", "", err
	}
	if _, err := tx.Exec(`UPDATE rotation_policies SET last_run_at=?, last_run_id=?, last_event_id=?, updated_at=datetime('now') WHERE id=?`,
		dueAt, id, eventID, policy.ID); err != nil {
		return "", "", "", err
	}
	return id, status, eventID, nil
}

// Run actions are in runs.go; scheduling/heartbeat in worker.go.
