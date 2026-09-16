// 审计导出（M5 #14）—— CSV / JSONL 归档；大结果集异步生成 + 下载链接；
// 导出动作本身记审计（append-only 完整性要求，导出留痕）。
package org

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
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
	// 同步导出：流式写出（行数受 threshold 保护，内存可控）
	cond, args := buildAuditQuery(orgID, f)
	rows, err := s.DB.Query(`SELECT actor_name, actor_type, action, resource, metadata, ip, created_at
		FROM audit_logs WHERE `+cond+` ORDER BY created_at LIMIT ?`, append(args, asyncExportThreshold)...)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	count := 0
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="taboo-audit-`+time.Now().Format("20060102-150405")+`.`+format+`"`)
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"created_at", "actor_name", "actor_type", "action", "resource", "metadata", "ip"})
		for rows.Next() {
			var actor, actorType, action, resource, metadata, ip, created string
			_ = rows.Scan(&actor, &actorType, &action, &resource, &metadata, &ip, &created)
			_ = cw.Write([]string{created, actor, actorType, action, resource, metadata, ip})
			count++
		}
		cw.Flush()
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		enc := json.NewEncoder(w)
		for rows.Next() {
			var actor, actorType, action, resource, metadata, ip, created string
			_ = rows.Scan(&actor, &actorType, &action, &resource, &metadata, &ip, &created)
			_ = enc.Encode(map[string]string{
				"created_at": created, "actor_name": actor, "actor_type": actorType,
				"action": action, "resource": resource, "metadata": metadata, "ip": ip,
			})
			count++
		}
	}
	auth.Audit(s.DB, orgID, auth.From(r), "audit.export", "org/audit",
		map[string]any{"format": format, "rows": count, "mode": "sync"}, ipOf(r))
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
	if _, err := s.DB.Exec(`INSERT INTO audit_exports (id, org_id, actor_id, format, filters)
		VALUES (?, ?, ?, ?, ?)`, id, orgID, auth.From(r).ID, format, string(filtersJSON)); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, orgID, auth.From(r), "audit.export.request", "org/audit",
		map[string]any{"format": format, "job": id}, ipOf(r))
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
	rows, err := s.DB.Query(`SELECT id, org_id, format, filters FROM audit_exports WHERE status = 'pending' LIMIT 3`)
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
	var f struct {
		Action, Actor, Resource, From, To string
		Limit                             int
	}
	_ = json.Unmarshal([]byte(filtersJSON), &f)
	flt := auditFilter{Action: f.Action, Actor: f.Actor, Resource: f.Resource, From: f.From, To: f.To}
	if f.Limit <= 0 || f.Limit > 100000 {
		f.Limit = 100000
	}
	cond, args := buildAuditQuery(orgID, flt)
	rows, err := s.DB.Query(`SELECT actor_name, actor_type, action, resource, metadata, ip, created_at
		FROM audit_logs WHERE `+cond+` ORDER BY created_at LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		_, _ = s.DB.Exec(`UPDATE audit_exports SET status = 'failed', error = ? WHERE id = ?`, err.Error(), id)
		return
	}
	defer rows.Close()
	dir := filepath.Join(s.DataDir, "exports", orgID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		_, _ = s.DB.Exec(`UPDATE audit_exports SET status = 'failed', error = ? WHERE id = ?`, err.Error(), id)
		return
	}
	path := filepath.Join(dir, id+"."+format)
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_, _ = s.DB.Exec(`UPDATE audit_exports SET status = 'failed', error = ? WHERE id = ?`, err.Error(), id)
		return
	}
	count := 0
	if format == "csv" {
		cw := csv.NewWriter(fh)
		_ = cw.Write([]string{"created_at", "actor_name", "actor_type", "action", "resource", "metadata", "ip"})
		for rows.Next() {
			var actor, actorType, action, resource, metadata, ip, created string
			_ = rows.Scan(&actor, &actorType, &action, &resource, &metadata, &ip, &created)
			_ = cw.Write([]string{created, actor, actorType, action, resource, metadata, ip})
			count++
		}
		cw.Flush()
	} else {
		enc := json.NewEncoder(fh)
		for rows.Next() {
			var actor, actorType, action, resource, metadata, ip, created string
			_ = rows.Scan(&actor, &actorType, &action, &resource, &metadata, &ip, &created)
			_ = enc.Encode(map[string]string{
				"created_at": created, "actor_name": actor, "actor_type": actorType,
				"action": action, "resource": resource, "metadata": metadata, "ip": ip,
			})
			count++
		}
	}
	_ = fh.Close()
	_, _ = s.DB.Exec(`UPDATE audit_exports SET status = 'done', file_path = ?, row_count = ?,
		completed_at = datetime('now') WHERE id = ?`, path, count, id)
	auth.Audit(s.DB, orgID, nil, "audit.export", "org/audit",
		map[string]any{"format": format, "rows": count, "mode": "async", "job": id}, "")
}
