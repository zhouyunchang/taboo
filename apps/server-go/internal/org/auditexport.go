// 审计导出（M5 #14）—— CSV / JSONL 归档；大结果集异步生成 + 下载链接；
// 导出动作本身记审计（append-only 完整性要求，导出留痕）。
package org

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

// asyncExportThreshold：超过该行数走异步任务（生成文件 + 下载链接）
const asyncExportThreshold = 10000

type auditFilter struct {
	Action   string
	Actor    string
	Resource string
	From     string // YYYY-MM-DD
	To       string
	Limit    int
}

func parseAuditFilter(r *http.Request) auditFilter {
	f := auditFilter{
		Action:   r.URL.Query().Get("action"),
		Actor:    r.URL.Query().Get("actor"),
		Resource: r.URL.Query().Get("resource"),
		From:     r.URL.Query().Get("from"),
		To:       r.URL.Query().Get("to"),
		Limit:    100,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = n
		}
	}
	if f.Limit > 500 {
		f.Limit = 500
	}
	return f
}

// buildAuditQuery 组装筛选 SQL（orgID 注入 WHERE）
func buildAuditQuery(orgID string, f auditFilter) (string, []any) {
	cond := `org_id = ?`
	args := []any{orgID}
	if f.Action != "" {
		cond += ` AND action = ?`
		args = append(args, f.Action)
	}
	if f.Actor != "" {
		cond += ` AND (actor_name = ? OR actor_id = ?)`
		args = append(args, f.Actor, f.Actor)
	}
	if f.Resource != "" {
		cond += ` AND resource LIKE ?`
		args = append(args, "%"+f.Resource+"%")
	}
	if f.From != "" {
		cond += ` AND created_at >= ?`
		args = append(args, f.From)
	}
	if f.To != "" {
		cond += ` AND created_at <= ?`
		args = append(args, f.To+" 23:59:59")
	}
	return cond, args
}

// AuditExport GET /orgs/:slug/audit/export?format=csv|jsonl&limit=&async=1
// 小结果集直接流式返回；大结果集或 async=1 创建任务，经 GET /audit/exports/{id} 下载。
func (s *Service) AuditExport(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditExport, "") {
		return
	}
	format := r.URL.Query().Get("format")
	if format != "csv" && format != "jsonl" {
		format = "csv"
	}
	f := parseAuditFilter(r)
	// 同步导出：最多 asyncExportThreshold 行；显式 async=1 才走异步任务
	async := r.URL.Query().Get("async") == "1"

	if async {
		f.Limit = 100000
		s.createExportJob(w, r, orgID, format, f)
		return
	}
	// Record authorization before releasing any export bytes. Query/storage
	// errors never emit a partially prepared success response.
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "audit.export", "org/audit", map[string]any{"format": format, "mode": "sync"}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "export unavailable"))
		return
	}
	// A temporary file bounds memory and ensures DB/encoding errors are discovered
	// before the response begins. It is removed even when the client disconnects.
	temp, err := os.CreateTemp("", "taboo-audit-*")
	if err != nil {
		writeErr(w, apperr.New(503, "EXPORT_UNAVAILABLE", "export unavailable"))
		return
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err = s.writeExport(r.Context(), temp, orgID, format, f, asyncExportThreshold); err != nil {
		writeErr(w, apperr.New(503, "EXPORT_UNAVAILABLE", "export unavailable"))
		return
	}
	if _, err = temp.Seek(0, 0); err != nil {
		writeErr(w, apperr.New(503, "EXPORT_UNAVAILABLE", "export unavailable"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="taboo-audit.`+format+`"`)
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	}
	_, _ = io.Copy(w, temp)

}

// createExportJob 异步导出：落任务行，worker 生成文件
func (s *Service) createExportJob(w http.ResponseWriter, r *http.Request, orgID, format string, f auditFilter) {
	if s.DataDir == "" {
		writeErr(w, apperr.New(500, "INTERNAL", "export data dir not configured"))
		return
	}
	filtersJSON, _ := json.Marshal(map[string]any{
		"action": f.Action, "actor": f.Actor, "resource": f.Resource,
		"from": f.From, "to": f.To, "limit": f.Limit,
	})
	id := tc.NewID()
	tx, ok := auth.BeginMutation(s.DB, w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO audit_exports (id, org_id, actor_id, format, filters)
		VALUES (?, ?, ?, ?, ?)`, id, orgID, auth.From(r).ID, format, string(filtersJSON)); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, auth.From(r), "audit.export.request", "org/audit", map[string]any{"format": format, "job": id}) {
		return
	}
	writeJSON(w, 202, map[string]any{
		"id": id, "status": "pending",
		"download_url": "/api/v1/orgs/" + chi.URLParam(r, "slug") + "/audit/exports/" + id,
	})
}

// AuditExportDownload GET /orgs/:slug/audit/exports/{id} —— 下载已生成的导出文件
func (s *Service) AuditExportDownload(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditExport, "") {
		return
	}
	id := chi.URLParam(r, "id")
	var status, format, filePath string
	err := s.DB.QueryRow(`SELECT status, format, file_path FROM audit_exports
		WHERE id = ? AND org_id = ?`, id, orgID).Scan(&status, &format, &filePath)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if status != "done" {
		writeJSON(w, 200, map[string]any{"id": id, "status": status})
		return
	}
	root, err := filepath.Abs(filepath.Join(s.DataDir, "exports", orgID))
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	absolute, err := filepath.Abs(filePath)
	if err != nil || filepath.Dir(absolute) != root || s.DataDir == "" {
		writeErr(w, apperr.NotFound)
		return
	}
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "audit.export", "org/audit", map[string]any{"mode": "download", "job": id}); err != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "download unavailable"))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="taboo-audit-`+id+`.`+format+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	}
	http.ServeFile(w, r, filePath)
}

// ListAuditExports GET /orgs/:slug/audit/exports —— 任务列表
func (s *Service) ListAuditExports(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditExport, "") {
		return
	}
	rows, err := s.DB.Query(`SELECT id, format, status, row_count, error, created_at, completed_at
		FROM audit_exports WHERE org_id = ? ORDER BY created_at DESC LIMIT 50`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type job struct {
		ID          string `json:"id"`
		Format      string `json:"format"`
		Status      string `json:"status"`
		RowCount    int    `json:"row_count"`
		Error       string `json:"error,omitempty"`
		CreatedAt   string `json:"created_at"`
		CompletedAt string `json:"completed_at,omitempty"`
	}
	out := []job{}
	for rows.Next() {
		var j job
		var completedAt sql.NullString
		_ = rows.Scan(&j.ID, &j.Format, &j.Status, &j.RowCount, &j.Error, &j.CreatedAt, &completedAt)
		j.CompletedAt = completedAt.String
		out = append(out, j)
	}
	writeJSON(w, 200, map[string]any{"exports": out})
}

func (s *Service) RetryAuditExport(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditExport, "") {
		return
	}
	id := chi.URLParam(r, "id")
	var status string
	if err := s.DB.QueryRow(`SELECT status FROM audit_exports WHERE id = ? AND org_id = ?`, id, orgID).Scan(&status); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if status != "failed" {
		writeErr(w, apperr.New(400, "INVALID", "only failed exports can be retried"))
		return
	}
	tx, ok := auth.BeginMutation(s.DB, w, r)
	if !ok {
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE audit_exports SET status='pending',error='',lease_token='',lease_until=0 WHERE id=? AND org_id=? AND status='failed'`, id, orgID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.New(409, "JOB_CHANGED", "reload before retrying"))
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, auth.From(r), "audit.export.retry", "org/audit", map[string]any{"job": id}) {
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": "pending"})
}

// StartExportWorker 后台生成导出文件（dataDir/exports/{org}/{id}.fmt）
func (s *Service) StartExportWorker(ctx context.Context, interval time.Duration) {
	if s.DataDir == "" {
		return
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			s.runExportJobs()
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (s *Service) runExportJobs() {
	rows, err := s.DB.Query(`SELECT id, org_id, format, filters FROM audit_exports WHERE status = 'pending' AND lease_until<=? ORDER BY created_at,id LIMIT 3`, time.Now().Unix())
	if err != nil {
		return
	}
	type job struct {
		id, orgID, format, filters string
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.orgID, &j.format, &j.filters); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		s.generateExport(j.id, j.orgID, j.format, j.filters)
	}
}

func (s *Service) generateExport(id, orgID, format, filtersJSON string) {
	lease, err := outbox.Claim(s.DB, "audit_exports", id)
	if err != nil || lease == "" {
		return
	}
	fail := func() {
		_, _ = s.DB.Exec(`UPDATE audit_exports SET status='failed',error='export generation failed',lease_token='',lease_until=0 WHERE id=? AND lease_token=?`, id, lease)
	}
	var f auditFilter
	if json.Unmarshal([]byte(filtersJSON), &f) != nil || (format != "csv" && format != "jsonl") {
		fail()
		return
	}
	if f.Limit <= 0 || f.Limit > 100000 {
		f.Limit = 100000
	}
	dir := filepath.Join(s.DataDir, "exports", orgID)
	if err = os.MkdirAll(dir, 0700); err != nil {
		fail()
		return
	}
	// Each lease owns its file; a late worker cannot truncate the winning output.
	file, err := os.CreateTemp(dir, id+"-"+lease+"-*."+format)
	if err != nil {
		fail()
		return
	}
	keep := false
	defer func() {
		file.Close()
		if !keep {
			os.Remove(file.Name())
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	count, err := s.writeExport(ctx, file, orgID, format, f, f.Limit)
	if err != nil {
		fail()
		return
	}
	if file.Sync() != nil {
		fail()
		return
	}
	if file.Close() != nil {
		fail()
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE audit_exports SET status='done',file_path=?,row_count=?,completed_at=datetime('now'),lease_token='',lease_until=0 WHERE id=? AND org_id=? AND status='pending' AND lease_token=? AND lease_until>?`, file.Name(), count, id, orgID, lease, time.Now().Unix())
	if err != nil {
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return
	}
	if err = audit.Record(tx, audit.Rec{OrgID: orgID, ActorID: "system", ActorType: "system", Action: "audit.export", Resource: "org/audit", Metadata: map[string]any{"format": format, "rows": count, "mode": "async", "job": id}}); err != nil {
		return
	}
	// If commit acknowledgement is uncertain, keep the private file: the durable
	// row may reference it. Orphans are safe to remove in the documented cleanup.
	keep = true
	if tx.Commit() != nil {
		return
	}
}

func (s *Service) writeExport(ctx context.Context, dst io.Writer, org, format string, f auditFilter, limit int) (int, error) {
	cond, args := buildAuditQuery(org, f)
	rows, err := s.DB.QueryContext(ctx, `SELECT actor_name,actor_type,action,resource,metadata,ip,created_at FROM audit_logs WHERE `+cond+` ORDER BY created_at,id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	cw := csv.NewWriter(dst)
	enc := json.NewEncoder(dst)
	if format == "csv" {
		if err = cw.Write([]string{"created_at", "actor_name", "actor_type", "action", "resource", "metadata", "ip"}); err != nil {
			return 0, err
		}
	}
	for rows.Next() {
		var actor, kind, action, resource, metadata, ip, created string
		if err = rows.Scan(&actor, &kind, &action, &resource, &metadata, &ip, &created); err != nil {
			return count, err
		}
		if format == "csv" {
			err = cw.Write([]string{created, actor, kind, action, resource, metadata, ip})
		} else {
			err = enc.Encode(map[string]string{"created_at": created, "actor_name": actor, "actor_type": kind, "action": action, "resource": resource, "metadata": metadata, "ip": ip})
		}
		if err != nil {
			return count, err
		}
		count++
	}
	if format == "csv" {
		cw.Flush()
		if err = cw.Error(); err != nil {
			return count, err
		}
	}
	return count, errors.Join(rows.Err(), ctx.Err())
}
