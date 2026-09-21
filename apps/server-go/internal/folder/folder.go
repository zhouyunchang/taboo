// 多级文件夹服务（M2 #5）—— 物化路径 /a/b/，对齐设计文档 §4 / §9
//
//	GET    /projects/:pid/folders?env=            平铺列表（前端建树）
//	POST   /projects/:pid/folders?env= {path}     创建（自动补中间节点）
//	DELETE /projects/:pid/folders?env=&path=      删除（仅空文件夹：无子目录且无密钥）
//	POST   /projects/:pid/folders/move?env= {from,to}  移动/重命名（级联更新子路径）
package folder

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

type Service struct {
	DB *sql.DB
}

type Folder struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

// NormalizePath 规范化物化路径：/a/b/（拒绝 .. 与空段）
func NormalizePath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "/", nil
	}
	segs := strings.Split(p, "/")
	clean := make([]string, 0, len(segs))
	for _, s := range segs {
		s = strings.TrimSpace(s)
		if s == "" || s == "." || s == ".." {
			return "", apperr.New(400, "INVALID", "invalid folder path")
		}
		clean = append(clean, s)
	}
	return "/" + strings.Join(clean, "/") + "/", nil
}

func parentPathOf(path string) string {
	trimmed := strings.Trim(path, "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return "/"
	}
	return "/" + trimmed[:i] + "/"
}

func nameOf(path string) string {
	trimmed := strings.Trim(path, "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return trimmed
	}
	return trimmed[i+1:]
}

// ResolveID 解析路径 → folder id；create=true 时逐级创建（含根）
func ResolveID(db *sql.DB, envID, path string, create bool) (string, error) {
	path, err := NormalizePath(path)
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRow(`SELECT id FROM folders WHERE env_id = ? AND path = ?`, envID, path).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !create {
		return "", apperr.NotFound
	}
	if path == "/" {
		// 根目录：parent 为 NULL，直接插入，勿递归（parentPathOf("/") 仍是 "/"，会自引用爆栈）
		id = tc.NewID()
		if _, err := db.Exec(`INSERT INTO folders (id, env_id, parent_id, name, path) VALUES (?, ?, NULL, '/', '/')`,
			id, envID); err != nil {
			return "", err
		}
		return id, nil
	}
	// 逐级创建
	parentID, err := ResolveID(db, envID, parentPathOf(path), true)
	if err != nil {
		return "", err
	}
	id = tc.NewID()
	if _, err := db.Exec(`INSERT INTO folders (id, env_id, parent_id, name, path) VALUES (?, ?, ?, ?, ?)`,
		id, envID, parentID, nameOf(path), path); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) envIDOf(projectID, envSlug string) (string, error) {
	var envID string
	err := s.DB.QueryRow(`SELECT id FROM environments WHERE project_id = ? AND slug = ?`, projectID, envSlug).Scan(&envID)
	return envID, err
}

// Routes 挂载 /projects/:pid/folders（ProjectCtx 由上层注入）
func (s *Service) Routes(r chi.Router) {
	r.Get("/folders", s.List)
	r.Post("/folders", s.Create)
	r.Delete("/folders", s.Delete)
	r.Post("/folders/move", s.Move)
}

func (s *Service) envOf(w http.ResponseWriter, r *http.Request, action string) (string, project.Ctx, bool) {
	p := project.Of(r)
	envSlug := r.URL.Query().Get("env")
	if envSlug == "" {
		envSlug = "dev"
	}
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, action, envSlug) {
		return "", p, false
	}
	envID, err := s.envIDOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "env not found"))
		return "", p, false
	}
	return envID, p, true
}

// List GET /folders?env=
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	envID, _, ok := s.envOf(w, r, "read")
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, COALESCE(parent_id, ''), name, path FROM folders WHERE env_id = ? ORDER BY path`, envID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		_ = rows.Scan(&f.ID, &f.ParentID, &f.Name, &f.Path)
		out = append(out, f)
	}
	writeJSON(w, 200, map[string]any{"folders": out})
}

// Create POST /folders?env= {path}
func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	envID, p, ok := s.envOf(w, r, "write")
	if !ok {
		return
	}
	var b struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Path == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	path, err := NormalizePath(b.Path)
	if err != nil {
		writeErr(w, err.(*apperr.Error))
		return
	}
	id, err := ResolveID(s.DB, envID, path, true)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, auth.From(r), "folders.create", "project/"+p.Slug+"/env/"+r.URL.Query().Get("env")+"/folder/"+path, nil)
	writeJSON(w, 201, map[string]any{"id": id, "path": path})
}

// Delete DELETE /folders?env=&path= —— 仅空文件夹（无子目录、无直接密钥）
func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	envID, p, ok := s.envOf(w, r, "write")
	if !ok {
		return
	}
	path, err := NormalizePath(r.URL.Query().Get("path"))
	if err != nil || path == "/" {
		writeErr(w, apperr.New(400, "INVALID", "cannot delete root folder"))
		return
	}
	var id string
	if err := s.DB.QueryRow(`SELECT id FROM folders WHERE env_id = ? AND path = ?`, envID, path).Scan(&id); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var children, secretsN int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM folders WHERE parent_id = ?`, id).Scan(&children)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM secrets WHERE folder_id = ?`, id).Scan(&secretsN)
	if children > 0 || secretsN > 0 {
		writeErr(w, apperr.New(400, "NOT_EMPTY", "folder is not empty"))
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM folders WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, auth.From(r), "folders.delete", "project/"+p.Slug+"/env/"+r.URL.Query().Get("env")+"/folder/"+path, nil)
	writeJSON(w, 200, map[string]any{"deleted": path})
}

// Move POST /folders/move?env= {from, to} —— 移动/重命名，级联更新子孙路径
func (s *Service) Move(w http.ResponseWriter, r *http.Request) {
	envID, p, ok := s.envOf(w, r, "write")
	if !ok {
		return
	}
	var b struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	from, err := NormalizePath(b.From)
	if err != nil || from == "/" {
		writeErr(w, apperr.New(400, "INVALID", "cannot move root folder"))
		return
	}
	to, err := NormalizePath(b.To)
	if err != nil {
		writeErr(w, err.(*apperr.Error))
		return
	}
	if strings.HasPrefix(to, from) {
		writeErr(w, apperr.New(400, "INVALID", "cannot move into itself"))
		return
	}
	var id, parentID string
	if err := s.DB.QueryRow(`SELECT id, COALESCE(parent_id, '') FROM folders WHERE env_id = ? AND path = ?`, envID, from).
		Scan(&id, &parentID); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	newParentID, err := ResolveID(s.DB, envID, parentPathOf(to), true)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer tx.Rollback()
	// 级联更新：本节点 + 所有子孙的物化路径前缀替换
	if _, err := tx.Exec(`UPDATE folders SET path = ? || substr(path, ?), parent_id = ?, name = ? WHERE id = ?`,
		to, len(from)+1, newParentID, nameOf(to), id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := tx.Exec(`UPDATE folders SET path = ? || substr(path, ?) WHERE path LIKE ? AND env_id = ?`,
		to, len(from)+1, from+"%", envID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// secrets.folder 冗余列同步（folder_id 不变，仅展示列）
	if _, err := tx.Exec(`UPDATE secrets SET folder = ? || substr(folder, ?) WHERE env_id = ? AND folder LIKE ?`,
		to, len(from)+1, envID, from+"%"); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, auth.From(r), "folders.move", "project/"+p.Slug+"/env/"+r.URL.Query().Get("env")+"/folder/"+from,
		map[string]any{"to": to})
	writeJSON(w, 200, map[string]any{"from": from, "to": to})
}
