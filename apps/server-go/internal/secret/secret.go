// 密钥服务：CRUD、版本、回滚、导出、reveal 分离 —— 对齐设计文档 §4 / §5 / §6
package secret

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	DEKs      *tc.DEKCache
}

type Meta struct {
	ID      string   `json:"id"`
	Folder  string   `json:"folder"`
	Key     string   `json:"key"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"`
	Version int      `json:"version"`
	Updated string   `json:"updated_at"`
	CanRev  bool     `json:"canReveal"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func clientIP(r *http.Request) string { return r.RemoteAddr }

func (s *Service) orgDEK(orgID string) ([]byte, error) {
	var enc string
	if err := s.DB.QueryRow(`SELECT dek_encrypted FROM orgs WHERE id = ?`, orgID).Scan(&enc); err != nil {
		return nil, err
	}
	return s.DEKs.Get(s.MasterKey, orgID, enc)
}

// env 解析并校验项目归属
func (s *Service) envOf(projectID, envSlug string) (envID string, err error) {
	err = s.DB.QueryRow(`SELECT id FROM environments WHERE project_id = ? AND slug = ?`, projectID, envSlug).Scan(&envID)
	return
}

// ---------- 路由挂载 ----------

func (s *Service) Routes(r chi.Router) {
	r.Get("/export", s.Export)
	r.Get("/secrets", s.List)
	r.Post("/secrets", s.Upsert)
	r.Route("/secrets/{key}", func(r chi.Router) {
		r.Get("/", s.Reveal)
		r.Get("/versions", s.Versions)
		r.Post("/rollback", s.Rollback)
	})
}

// GET /export?env=dev —— 导出 .env（记审计）
func (s *Service) Export(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "reveal", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	rows, err := s.DB.Query(`SELECT s.key, v.ciphertext FROM secrets s
		JOIN secret_versions v ON v.secret_id = s.id AND v.version = s.latest_version
		WHERE s.env_id = ? ORDER BY s.key`, envID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type kv struct{ k, ct string }
	var list []kv
	for rows.Next() {
		var x kv
		_ = rows.Scan(&x.k, &x.ct)
		list = append(list, x)
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+p.Slug+"-"+envSlug+".env\"")
	w.Header().Set("Cache-Control", "no-store")
	for _, x := range list {
		val, err := tc.Decrypt(dek, x.ct)
		if err != nil {
			continue
		}
		_, _ = w.Write([]byte(x.k + "=" + val + "\n"))
	}
	auth.Audit(s.DB, p.OrgID, u, "secrets.export", "project/"+p.Slug+"/env/"+envSlug,
		map[string]any{"count": len(list)}, clientIP(r))
}

// GET /secrets?env=dev —— 列表（无值）
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "read", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	canRev := auth.Can(s.DB, u, p.OrgID, p.ID, "reveal", envSlug)
	rows, err := s.DB.Query(`SELECT id, folder, key, comment, tags, latest_version, updated_at
		FROM secrets WHERE env_id = ? ORDER BY folder, key`, envID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []Meta{}
	count := 0
	for rows.Next() {
		var m Meta
		var tags string
		_ = rows.Scan(&m.ID, &m.Folder, &m.Key, &m.Comment, &tags, &m.Version, &m.Updated)
		_ = json.Unmarshal([]byte(tags), &m.Tags)
		m.CanRev = canRev
		out = append(out, m)
		count++
	}
	auth.Audit(s.DB, p.OrgID, u, "secrets.list", "project/"+p.Slug+"/env/"+envSlug,
		map[string]any{"count": count}, clientIP(r))
	writeJSON(w, 200, map[string]any{"secrets": out})
}

// POST /secrets?env=dev —— 创建/更新（产生新版本）
func (s *Service) Upsert(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "write", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	var b struct {
		Key     string   `json:"key"`
		Value   *string  `json:"value"`
		Comment *string  `json:"comment"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Key == "" || b.Value == nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	folder := q(r, "path", "/")
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	ct, err := tc.Encrypt(dek, *b.Value)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encrypt failed"))
		return
	}
	// 事务：读旧版本 → 写新版本 → 推进 latest_version
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer tx.Rollback()
	var secretID string
	var latest int
	var oldComment, oldTags string
	err = tx.QueryRow(`SELECT id, latest_version, comment, tags FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`,
		envID, folder, b.Key).Scan(&secretID, &latest, &oldComment, &oldTags)
	isCreate := err == sql.ErrNoRows
	if err != nil && !isCreate {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if isCreate {
		secretID = tc.NewID()
		if _, err := tx.Exec(`INSERT INTO secrets (id, env_id, folder, key, comment, tags) VALUES (?, ?, ?, ?, ?, ?)`,
			secretID, envID, folder, b.Key, strOr(b.Comment, ""), tagsOr(b.Tags)); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	} else {
		if _, err := tx.Exec(`UPDATE secrets SET comment = ?, tags = ?, updated_at = datetime('now') WHERE id = ?`,
			strOr(b.Comment, oldComment), tagsOrDefault(b.Tags, oldTags), secretID); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	next := latest + 1
	if _, err := tx.Exec(`INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
		tc.NewID(), secretID, next, ct, u.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := tx.Exec(`UPDATE secrets SET latest_version = ? WHERE id = ?`, next, secretID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	action := "secrets.update"
	if isCreate {
		action = "secrets.create"
	}
	auth.Audit(s.DB, p.OrgID, u, action, "project/"+p.Slug+"/env/"+envSlug+"/secret/"+b.Key,
		map[string]any{"version": next}, clientIP(r))
	writeJSON(w, 200, map[string]any{"key": b.Key, "version": next})
}

// GET /secrets/{key}?env=dev —— reveal 取明文（记审计）
func (s *Service) Reveal(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "reveal", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	folder := q(r, "path", "/")
	var secretID string
	var comment, tags string
	var latest int
	err = s.DB.QueryRow(`SELECT id, comment, tags, latest_version FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`,
		envID, folder, key).Scan(&secretID, &comment, &tags, &latest)
	if err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var ct string
	if err := s.DB.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id = ? AND version = ?`,
		secretID, latest).Scan(&ct); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	val, err := tc.Decrypt(dek, ct)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "decrypt failed"))
		return
	}
	auth.Audit(s.DB, p.OrgID, u, "secrets.reveal", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"version": latest}, clientIP(r))
	var tagsArr []string
	_ = json.Unmarshal([]byte(tags), &tagsArr)
	writeJSON(w, 200, map[string]any{
		"key": key, "value": val, "version": latest, "comment": comment, "tags": tagsArr,
	})
}

// GET /secrets/{key}/versions
func (s *Service) Versions(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "read", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	folder := q(r, "path", "/")
	var secretID string
	var latest int
	if err := s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`,
		envID, folder, key).Scan(&secretID, &latest); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	rows, err := s.DB.Query(`SELECT version, created_by, created_at FROM secret_versions
		WHERE secret_id = ? ORDER BY version DESC`, secretID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type v struct {
		Version   int    `json:"version"`
		CreatedBy string `json:"created_by"`
		CreatedAt string `json:"created_at"`
	}
	out := []v{}
	for rows.Next() {
		var x v
		_ = rows.Scan(&x.Version, &x.CreatedBy, &x.CreatedAt)
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"key": key, "latest": latest, "versions": out})
}

// POST /secrets/{key}/rollback {version}
func (s *Service) Rollback(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "write", envSlug) {
		writeErr(w, apperr.Forbidden)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	var b struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Version < 1 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	key := chi.URLParam(r, "key")
	folder := q(r, "path", "/")
	var secretID string
	var latest int
	if err := s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder = ? AND key = ?`,
		envID, folder, key).Scan(&secretID, &latest); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var ct string
	if err := s.DB.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id = ? AND version = ?`,
		secretID, b.Version).Scan(&ct); err != nil {
		writeErr(w, apperr.VersionNotFound)
		return
	}
	next := latest + 1
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
		tc.NewID(), secretID, next, ct, u.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := tx.Exec(`UPDATE secrets SET latest_version = ?, updated_at = datetime('now') WHERE id = ?`,
		next, secretID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	auth.Audit(s.DB, p.OrgID, u, "secrets.rollback", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"from": b.Version, "to": next}, clientIP(r))
	writeJSON(w, 200, map[string]any{"key": key, "version": next, "rolledBackFrom": b.Version})
}

// ---------- helpers ----------

type ctxKey string

const ProjectKey ctxKey = "taboo.project"

type ProjectCtx struct {
	ID    string
	OrgID string
	Slug  string
}

func projectOf(r *http.Request) ProjectCtx {
	p, _ := r.Context().Value(ProjectKey).(ProjectCtx)
	return p
}

func q(r *http.Request, k, def string) string {
	if v := r.URL.Query().Get(k); v != "" {
		return v
	}
	return def
}

func strOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func tagsOr(tags []string) string {
	if tags == nil {
		tags = []string{}
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

func tagsOrDefault(tags []string, old string) string {
	if tags == nil {
		return old
	}
	return tagsOr(tags)
}
