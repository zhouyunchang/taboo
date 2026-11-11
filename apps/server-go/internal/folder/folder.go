// 多级文件夹服务（M2 #5）—— 物化路径 /a/b/，对齐设计文档 §4 / §9
//
//	GET    /projects/:pid/folders                 平铺列表（前端建树）
//	POST   /projects/:pid/folders {path}          创建（自动补中间节点）
//	DELETE /projects/:pid/folders?path=           删除（仅空文件夹：无子目录且无密钥）
//	POST   /projects/:pid/folders/move {from,to}  移动/重命名（级联更新子路径）
package folder

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

type Service struct {
	DB *sql.DB
	// ControlDB 是 PostgreSQL 下的非租户连接：审计链必须由 schema owner 推进
	// （runtime 角色没有 audit_logs 的 SELECT 权限）。SQLite 下留空，沿用 DB。
	ControlDB *sql.DB
}

// control returns the connection that may advance the audit chain.
func (s *Service) control() *sql.DB {
	if s.ControlDB != nil {
		return s.ControlDB
	}
	return s.DB
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
type folderDB interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}

func ResolveID(db folderDB, projectID, path string, create bool) (string, error) {
	path, err := NormalizePath(path)
	if err != nil {
		return "", err
	}
	var id string
	err = db.QueryRow(`SELECT id FROM folders WHERE project_id = ? AND path = ?`, projectID, path).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	if !create {
		return "", apperr.NotFound
	}
	if path == "/" {
		// 根目录：parent 为 NULL，直接插入，勿递归（parentPathOf("/") 仍是 "/"，会自引用爆栈）
		id = tc.NewID()
		if _, err := db.Exec(`INSERT INTO folders (id, project_id, parent_id, name, path) VALUES (?, ?, NULL, '/', '/')`,
			id, projectID); err != nil {
			return "", err
		}
		return id, nil
	}
	// 逐级创建
	parentID, err := ResolveID(db, projectID, parentPathOf(path), true)
	if err != nil {
		return "", err
	}
	id = tc.NewID()
	if _, err := db.Exec(`INSERT INTO folders (id, project_id, parent_id, name, path) VALUES (?, ?, ?, ?, ?)`,
		id, projectID, parentID, nameOf(path), path); err != nil {
		return "", err
	}
	return id, nil
}

// Routes 挂载 /projects/:pid/folders（ProjectCtx 由上层注入）
func (s *Service) Routes(r chi.Router) {
	r.Get("/folders", s.List)
	r.Post("/folders", s.Create)
	r.Delete("/folders", s.Delete)
	r.Post("/folders/move", s.Move)
}

func (s *Service) projectScope(w http.ResponseWriter, r *http.Request, action string) (project.Ctx, bool) {
	p := project.Of(r)
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, action, "") {
		return p, false
	}
	return p, true
}

// List GET /folders
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectScope(w, r, "read")
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, COALESCE(parent_id, ''), name, path FROM folders WHERE project_id = ? ORDER BY path`, p.ID)
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

// Create POST /folders {path}
func (s *Service) Create(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectScope(w, r, "write")
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
	tx, started := auth.BeginMutation(s.control(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	id, err := ResolveID(tx, p.ID, path, true)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if !auth.CommitMutation(tx, w, r, p.OrgID, auth.From(r), "folders.create", "project/"+p.Slug+"/folder/"+path, nil) {
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "path": path})
}

// Delete DELETE /folders?path= —— 仅空文件夹（无子目录、无直接密钥）
func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectScope(w, r, "delete")
	if !ok {
		return
	}
	path, err := NormalizePath(r.URL.Query().Get("path"))
	if err != nil || path == "/" {
		writeErr(w, apperr.New(400, "INVALID", "cannot delete root folder"))
		return
	}
	tx, started := auth.BeginMutation(s.control(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRow(`SELECT id FROM folders WHERE project_id = ? AND path = ?`, p.ID, path).Scan(&id); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var children, secretsN int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM folders WHERE parent_id = ?`, id).Scan(&children); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM secrets WHERE folder_id = ?`, id).Scan(&secretsN); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if children > 0 || secretsN > 0 {
		writeErr(w, apperr.New(400, "NOT_EMPTY", "folder is not empty"))
		return
	}
	if _, err := tx.Exec(`DELETE FROM folders WHERE id = ?`, id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if !auth.CommitMutation(tx, w, r, p.OrgID, auth.From(r), "folders.delete", "project/"+p.Slug+"/folder/"+path, nil) {
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": path})
}

// Move POST /folders/move {from, to} —— 移动/重命名，级联更新子孙路径
func (s *Service) Move(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectScope(w, r, "write")
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
	tx, started := auth.BeginMutation(s.control(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	var id, parentID string
	if err := tx.QueryRow(`SELECT id, COALESCE(parent_id, '') FROM folders WHERE project_id = ? AND path = ?`, p.ID, from).
		Scan(&id, &parentID); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	newParentID, err := ResolveID(tx, p.ID, parentPathOf(to), true)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// 级联更新：本节点 + 所有子孙的物化路径前缀替换
	if _, err := tx.Exec(`UPDATE folders SET path = ? || substr(path, ?), parent_id = ?, name = ? WHERE id = ?`,
		to, utf8.RuneCountInString(from)+1, newParentID, nameOf(to), id); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := tx.Exec(`UPDATE folders SET path = ? || substr(path, ?) WHERE substr(path,1,?)=? AND project_id = ?`,
		to, utf8.RuneCountInString(from)+1, utf8.RuneCountInString(from), from, p.ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// secrets.folder 冗余列同步（folder_id 不变，仅展示列）
	if _, err := tx.Exec(`UPDATE secrets SET folder = ? || substr(folder, ?) WHERE project_id = ? AND substr(folder,1,?)=?`,
		to, utf8.RuneCountInString(from)+1, p.ID, utf8.RuneCountInString(from), from); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if !auth.CommitMutation(tx, w, r, p.OrgID, auth.From(r), "folders.move", "project/"+p.Slug+"/folder/"+from, map[string]any{"to": to}) {
		return
	}
	writeJSON(w, 200, map[string]any{"from": from, "to": to})
}
