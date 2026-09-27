// Secret Sync 服务（M5 #11）—— 对齐设计文档 §2 / §3.3 / §9
//
//	GET  /api/v1/orgs/:slug/sync/targets              列表（不含平台令牌）
//	POST /api/v1/orgs/:slug/sync/targets              创建目标（owner）
//	DELETE /api/v1/orgs/:slug/sync/targets/:id        删除（owner）
//	POST /api/v1/orgs/:slug/sync/targets/:id/retry    手动全量重推（owner）
//	GET  /api/v1/orgs/:slug/sync/runs                 同步记录（指纹审计，不含明文）
//
// 触发：密钥创建/更新/回滚/删除事件驱动（events.Bus 订阅）+ 手动 Retry；
// 失败指数退避重试，超过上限标记 dead；同步内容只记 sha256 指纹。
package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

// 退避序列：第 N 次失败后 next = backoff[N]（秒），超出即死信
var backoffSec = []int{30, 120, 600, 1800, 3600}

const maxAttempts = 6

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	DEKs      *tc.DEKCache
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func ipOf(r *http.Request) string { return httpx.ClientIP(r) }

// Subscribe 挂到事件总线：密钥变更 → 匹配目标 → 入队同步任务
func (s *Service) Subscribe(bus *events.Bus) {
	bus.Subscribe(func(tx *sql.Tx, e events.SecretEvent) error {
		if e.Action == "deleted" {
			return nil
		}
		rows, err := tx.Query(`SELECT id FROM sync_targets WHERE org_id=? AND enabled=1 AND (project_id IS NULL OR project_id=?) AND (env_slug='*' OR env_slug=?)`, e.OrgID, e.ProjectID, e.EnvSlug)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err = enqueue(tx, id, e); err != nil {
				return err
			}
		}
		return nil
	})
}

type target struct{ id, orgID, platform, projectID, envSlug, configEnc string }

func enqueue(tx *sql.Tx, tid string, e events.SecretEvent) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// Deterministic primary key deduplicates across every status, not only pending.
	id := tc.SHA256(tid + "\x00" + e.EventID + "\x00" + e.ProjectID + "\x00" + e.EnvSlug + "\x00" + e.Folder + "\x00" + e.Key)
	_, err = tx.Exec(`INSERT INTO sync_runs(id,target_id,org_id,event_id,secret_key,action,status,trigger_type,event_payload) VALUES(?,?,?,?,?,?,'pending','event',?) ON CONFLICT(id) DO NOTHING`, id, tid, e.OrgID, e.EventID, e.Key, e.Action, string(payload))
	return err
}

// StartWorker 后台消费：轮询 pending → 取明文 → 适配器推送 → 记指纹审计
// SQLite 单连接下每次只取少量任务，推送超时有限（适配器 15s），不长期占用连接。
func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			s.drain()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *Service) drain() {
	rows, err := s.DB.Query(`SELECT r.id, r.org_id, r.secret_key, r.action, r.attempts,
		t.platform, t.config_enc, t.project_id, t.env_slug
		FROM sync_runs r JOIN sync_targets t ON t.id = r.target_id
		WHERE r.status = 'pending' AND t.enabled = 1 AND r.next_attempt_at <= ?
		ORDER BY r.created_at LIMIT 8`, time.Now().Unix())
	if err != nil {
		return
	}
	type job struct {
		runID, orgID, key, action      string
		attempts                       int
		platform, configEnc, projectID string
		envSlug                        string
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		var pid sql.NullString
		if err := rows.Scan(&j.runID, &j.orgID, &j.key, &j.action, &j.attempts,
			&j.platform, &j.configEnc, &pid, &j.envSlug); err != nil {
			continue
		}
		j.projectID = pid.String
		jobs = append(jobs, j)
	}
	rows.Close() // 先释放连接（MaxOpenConns=1），再逐条处理

	for _, j := range jobs {
		s.execute(j.runID, j.orgID, j.platform, j.configEnc, j.projectID, j.envSlug, j.key, j.attempts)
	}
}

// execute 取明文（DEK 解密）→ 适配器推送 → 记录指纹
func (s *Service) execute(runID, orgID, platform, configEnc, projectID, envSlug, key string, attempts int) {
	lease, err := outbox.Claim(s.DB, "sync_runs", runID)
	if err != nil || lease == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	failRun := func(err error) {
		attempts++
		status, nextAt := retryAfterFail(attempts)
		if status == "dead" {
			_ = auth.Audit(s.DB, orgID, nil, "sync.dead", "sync/"+platform+"/"+key,
				map[string]any{"run_id": runID, "error": err.Error()}, "")
		}
		if _, e := s.DB.Exec(`UPDATE sync_runs SET status = ?, attempts = ?, next_attempt_at = ?, error = ?,
			updated_at = datetime('now'),lease_token='',lease_until=0 WHERE id = ? AND lease_token=?`, status, attempts, nextAt, "sync attempt failed", runID, lease); e != nil {
			slog.Error("sync failRun update", "run", runID, "err", e)
		}
	}

	// 明文取出：仅在内存短暂存在，绝不落库/日志
	value, err := s.snapshotValue(ctx, runID, orgID)
	if err != nil {
		failRun(err)
		return
	}
	var targetID string
	if err := s.DB.QueryRow(`SELECT r.target_id FROM sync_runs r JOIN sync_targets t ON t.id=r.target_id AND t.org_id=r.org_id WHERE r.id=? AND r.org_id=? AND t.enabled=1`, runID, orgID).Scan(&targetID); err != nil {
		failRun(err)
		return
	}
	cfgJSON, err := tc.Open(s.MasterKey, configEnc, tc.Context{Purpose: "sync-target", Org: orgID, Object: targetID, Version: 1})
	if err != nil {
		failRun(err)
		return
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		failRun(err)
		return
	}
	adapter, ok := Adapters[platform]
	if !ok {
		failRun(errUnknownPlatform(platform))
		return
	}
	if err := adapter.Push(ctx, cfg, key, value); err != nil {
		failRun(err)
		return
	}
	fingerprint := tc.SHA256(value)[:16]
	if _, err := s.DB.Exec(`UPDATE sync_runs SET status = 'success', attempts = attempts + 1,
		fingerprint = ?, error = '', updated_at = datetime('now'),lease_token='',lease_until=0 WHERE id = ? AND lease_token=?`,
		fingerprint, runID, lease); err != nil {
		slog.Error("sync success update", "run", runID, "err", err)
	}
	_ = auth.Audit(s.DB, orgID, nil, "sync.push", "sync/"+platform+"/"+key,
		map[string]any{"run_id": runID, "fingerprint": fingerprint, "target": platform, "key": key}, "")
}

func errUnknownPlatform(p string) error {
	return &apperr.Error{Status: 500, Code: "INTERNAL", Message: "unknown platform: " + p}
}

func (s *Service) snapshotValue(ctx context.Context, runID, orgID string) (string, error) {
	var payload, dekEnc string
	if err := s.DB.QueryRowContext(ctx, `SELECT event_payload FROM sync_runs WHERE id=? AND org_id=?`, runID, orgID).Scan(&payload); err != nil {
		return "", err
	}
	var e events.SecretEvent
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return "", err
	}
	if e.OrgID != orgID || e.SecretID == "" || e.ProjectID == "" || e.EnvironmentID == "" || e.Version < 1 || e.Ciphertext == "" {
		return "", fmt.Errorf("legacy job requires explicit requeue with an exact resource snapshot")
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=? AND p.id=? AND p.org_id=?`, e.EnvironmentID, e.ProjectID, orgID).Scan(&n); err != nil || n != 1 {
		return "", fmt.Errorf("resource scope no longer exists")
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT dek_encrypted FROM orgs WHERE id=?`, orgID).Scan(&dekEnc); err != nil {
		return "", err
	}
	dek, err := s.DEKs.Get(s.MasterKey, orgID, dekEnc)
	if err != nil {
		return "", err
	}
	return tc.Open(dek, e.Ciphertext, tc.SecretContext(orgID, e.ProjectID, e.EnvironmentID, e.SecretID, e.Version))
}

// ---------- HTTP ----------

func (s *Service) orgOf(w http.ResponseWriter, r *http.Request) (orgID, slug string, ok bool) {
	slug = chi.URLParam(r, "slug")
	if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&orgID); err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false
	}
	u := auth.From(r)
	if u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	if _, member := auth.Membership(s.DB, orgID, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	return orgID, slug, true
}

// manageOnly 组织管理（owner）：Sync/Webhook/SSO 配置均属高敏操作，仅用户主体可管理
func manageOnly(db *sql.DB, w http.ResponseWriter, r *http.Request, orgID string) bool {
	u := auth.From(r)
	if u.Kind != auth.KindUser || !auth.Can(db, u, orgID, "", rbac.Admin, "") {
		auth.Deny(db, w, r, orgID, rbac.Admin, "org/sync")
		return false
	}
	return true
}

// Routes 挂载到 /orgs/{slug}/sync
func (s *Service) Routes(r chi.Router) {
	r.Route("/orgs/{slug}/sync", func(r chi.Router) {
		r.Get("/targets", s.ListTargets)
		r.Post("/targets", s.CreateTarget)
		r.Delete("/targets/{id}", s.DeleteTarget)
		r.Post("/targets/{id}/retry", s.RetryTarget)
		r.Get("/runs", s.ListRuns)
	})
}

type targetOut struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Platform    string   `json:"platform"`
	ProjectID   string   `json:"project_id,omitempty"`
	EnvSlug     string   `json:"env_slug"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   string   `json:"created_at"`
	ConfigHints []string `json:"config_keys"` // 已配置的键名（不含值），便于 UI 回显
}

// ListTargets GET /targets
func (s *Service) ListTargets(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, platform, project_id, env_slug, config_enc, enabled, created_at
		FROM sync_targets WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []targetOut{}
	for rows.Next() {
		var t targetOut
		var pid sql.NullString
		var cfgEnc string
		var en int
		if err := rows.Scan(&t.ID, &t.Name, &t.Platform, &pid, &t.EnvSlug, &cfgEnc, &en, &t.CreatedAt); err != nil {
			continue
		}
		t.ProjectID = pid.String
		t.Enabled = en == 1
		if cfgJSON, err := tc.Open(s.MasterKey, cfgEnc, tc.Context{Purpose: "sync-target", Org: orgID, Object: t.ID, Version: 1}); err == nil {
			var cfg map[string]string
			if json.Unmarshal([]byte(cfgJSON), &cfg) == nil {
				for k := range cfg {
					t.ConfigHints = append(t.ConfigHints, k)
				}
			}
		}
		out = append(out, t)
	}
	writeJSON(w, 200, map[string]any{"targets": out})
}

// CreateTarget POST /targets {name, platform, project_id?, env_slug?, config{}, ping?}
func (s *Service) CreateTarget(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	var b struct {
		Name      string            `json:"name"`
		Platform  string            `json:"platform"`
		ProjectID string            `json:"project_id"`
		EnvSlug   string            `json:"env_slug"`
		Config    map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Name == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	adapter, ok2 := Adapters[b.Platform]
	if !ok2 {
		writeErr(w, apperr.New(400, "INVALID", "platform must be one of github/vercel/cloudflare"))
		return
	}
	for _, k := range requiredKeys[b.Platform] {
		if b.Config[k] == "" {
			writeErr(w, apperr.New(400, "INVALID", "missing config key: "+k))
			return
		}
	}
	if b.EnvSlug == "" {
		b.EnvSlug = "*"
	}
	if b.ProjectID != "" {
		var orgCheck string
		if err := s.DB.QueryRow(`SELECT org_id FROM projects WHERE id = ?`, b.ProjectID).Scan(&orgCheck); err != nil || orgCheck != orgID {
			writeErr(w, apperr.New(400, "INVALID", "project not in this org"))
			return
		}
	}
	// 可选连通性自检
	if r.URL.Query().Get("ping") == "1" {
		if err := adapter.Ping(r.Context(), b.Config); err != nil {
			writeErr(w, apperr.New(400, "PING_FAILED", err.Error()))
			return
		}
	}
	cfgJSON, _ := json.Marshal(b.Config)
	id := tc.NewID()
	cfgEnc, err := tc.Seal(s.MasterKey, string(cfgJSON), tc.Context{Purpose: "sync-target", Org: orgID, Object: id, Version: 1})
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	var pid interface{}
	if b.ProjectID != "" {
		pid = b.ProjectID
	}
	if _, err := s.DB.Exec(`INSERT INTO sync_targets (id, org_id, name, platform, project_id, env_slug, config_enc)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, orgID, b.Name, b.Platform, pid, b.EnvSlug, cfgEnc); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "sync.target.create", "org/"+slug+"/sync/"+b.Name,
		map[string]any{"platform": b.Platform, "project_id": b.ProjectID, "env_slug": b.EnvSlug}, ipOf(r))
	writeJSON(w, 201, map[string]any{"id": id, "name": b.Name, "platform": b.Platform})
}

// DeleteTarget DELETE /targets/{id}
func (s *Service) DeleteTarget(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	id := chi.URLParam(r, "id")
	var name string
	if err := s.DB.QueryRow(`SELECT name FROM sync_targets WHERE id = ? AND org_id = ?`, id, orgID).Scan(&name); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM sync_targets WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "sync.target.delete", "org/"+slug+"/sync/"+name, nil, ipOf(r))
	writeJSON(w, 200, map[string]any{"id": id, "deleted": true})
}

// RetryTarget POST /targets/{id}/retry —— 手动全量重推：绑定范围内所有密钥入队
func (s *Service) RetryTarget(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !manageOnly(s.DB, w, r, orgID) {
		return
	}
	id := chi.URLParam(r, "id")
	var t target
	var pid sql.NullString
	err := s.DB.QueryRow(`SELECT id, org_id, platform, project_id, env_slug, config_enc
		FROM sync_targets WHERE id = ? AND org_id = ?`, id, orgID).
		Scan(&t.id, &t.orgID, &t.platform, &pid, &t.envSlug, &t.configEnc)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	t.projectID = pid.String
	// 遍历目标范围内所有密钥，逐条入队（trigger=manual）
	cond := `p.org_id = ?`
	args := []any{orgID}
	if t.projectID != "" {
		cond += ` AND p.id = ?`
		args = append(args, t.projectID)
	}
	if t.envSlug != "" && t.envSlug != "*" {
		cond += ` AND e.slug = ?`
		args = append(args, t.envSlug)
	}
	rows, err := s.DB.Query(`SELECT s.key,p.id,e.slug,f.path,s.latest_version,s.id,e.id,v.ciphertext FROM secrets s
 JOIN secret_versions v ON v.secret_id=s.id AND v.version=s.latest_version
 JOIN folders f ON f.id=s.folder_id
		JOIN environments e ON e.id = s.env_id JOIN projects p ON p.id = e.project_id WHERE `+cond, args...)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	items := []events.SecretEvent{}
	eventID := "manual-" + tc.NewID()
	for rows.Next() {
		e := events.SecretEvent{EventID: eventID, OrgID: orgID, Action: "updated"}
		if err = rows.Scan(&e.Key, &e.ProjectID, &e.EnvSlug, &e.Folder, &e.Version, &e.SecretID, &e.EnvironmentID, &e.Ciphertext); err != nil {
			rows.Close()
			writeErr(w, apperr.New(500, "INTERNAL", "read failed"))
			return
		}
		items = append(items, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "read failed"))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "enqueue failed"))
		return
	}
	defer tx.Rollback()
	enqueued := len(items)
	for _, e := range items {
		if err = enqueue(tx, id, e); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "enqueue failed"))
			return
		}
	}
	if err = auth.AuditReq(tx, r, orgID, auth.From(r), "sync.retry", "org/"+slug+"/sync/"+id, map[string]any{"keys": enqueued}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "enqueue failed"))
		return
	}

	writeJSON(w, 200, map[string]any{"target_id": id, "enqueued": enqueued})
}

// ListRuns GET /runs?target_id=&status=&limit=
func (s *Service) ListRuns(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	cond := `org_id = ?`
	args := []any{orgID}
	if v := r.URL.Query().Get("target_id"); v != "" {
		cond += ` AND target_id = ?`
		args = append(args, v)
	}
	if v := r.URL.Query().Get("status"); v != "" {
		cond += ` AND status = ?`
		args = append(args, v)
	}
	rows, err := s.DB.Query(`SELECT id, target_id, secret_key, action, fingerprint, status,
		trigger_type, attempts, error, created_at, updated_at
		FROM sync_runs WHERE `+cond+` ORDER BY created_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type run struct {
		ID          string `json:"id"`
		TargetID    string `json:"target_id"`
		SecretKey   string `json:"secret_key"`
		Action      string `json:"action"`
		Fingerprint string `json:"fingerprint"`
		Status      string `json:"status"`
		Trigger     string `json:"trigger_type"`
		Attempts    int    `json:"attempts"`
		Error       string `json:"error,omitempty"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	out := []run{}
	for rows.Next() {
		var x run
		_ = rows.Scan(&x.ID, &x.TargetID, &x.SecretKey, &x.Action, &x.Fingerprint, &x.Status,
			&x.Trigger, &x.Attempts, &x.Error, &x.CreatedAt, &x.UpdatedAt)
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"runs": out})
}
