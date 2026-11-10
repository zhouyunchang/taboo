// 密钥服务：CRUD、版本、回滚、导出、reveal 分离 —— 对齐设计文档 §4 / §5 / §6
package secret

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// validKey：key 为单段标识（folder 与 key 分离，见 #16）—— 禁 '/'、'.'/'..'、首尾空白与控制字符
func validKey(k string) bool {
	if k == "" || k == "." || k == ".." || strings.TrimSpace(k) != k {
		return false
	}
	return !strings.ContainsAny(k, "/\x00\r\n\t")
}

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	DEKs      *tc.DEKCache
	Events    *events.Bus // M5：密钥变更事件（Sync #11 / Webhooks #12 订阅）
}

type Meta struct {
	ID          string   `json:"id"`
	Folder      string   `json:"folder"`
	Key         string   `json:"key"`
	Comment     string   `json:"comment"`
	Tags        []string `json:"tags"`
	Version     int      `json:"version"`
	Updated     string   `json:"updated_at"`
	CanRev      bool     `json:"canReveal"`
	CanWrite    bool     `json:"canWrite"`
	CanDelete   bool     `json:"canDelete"`
	CanRollback bool     `json:"canRollback"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func (s *Service) enqueue(tx outbox.DBTX, r *http.Request, p ProjectCtx, envSlug, folder, key string, version int, action, secretID, envID string) (events.SecretEvent, error) {
	u := auth.From(r)
	e := events.SecretEvent{
		EventID: tc.NewID(), OrgID: p.OrgID, ProjectID: p.ID, ProjectSlug: p.Slug,
		EnvSlug: envSlug, Folder: folder, Key: key, Version: version, Action: action,
		ActorID: u.ID, ActorName: u.Name, ActorKind: u.Kind, SecretID: secretID, EnvironmentID: envID,
	}
	if action != "deleted" {
		if err := tx.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id=? AND version=?`, secretID, version).Scan(&e.Ciphertext); err != nil {
			return e, err
		}
	}
	if err := outbox.Insert(tx, e); err != nil {
		return e, err
	}
	return e, nil
}

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

// folderRef 解析物化路径 → folder_id（norm 为规范化路径）
func (s *Service) folderRef(envID, path string, create bool) (folderID, norm string, err error) {
	norm, err = folder.NormalizePath(path)
	if err != nil {
		return "", "", err
	}
	folderID, err = folder.ResolveID(s.DB, envID, norm, create)
	return folderID, norm, err
}

// ---------- 路由挂载 ----------

func (s *Service) Routes(r chi.Router) {
	r.Get("/export", s.Export)
	r.Post("/import", s.Import)
	r.Get("/secrets", s.List)
	r.Post("/secrets", s.Upsert)
	r.Route("/secrets/{key}", func(r chi.Router) {
		r.Get("/", s.Reveal)
		r.Delete("/", s.Delete)
		r.Get("/versions", s.Versions)
		r.Get("/versions/{version}", s.RevealVersion)
		r.Post("/rollback", s.Rollback)
	})
}

// GET /export?env=dev —— 导出 .env（记审计）
func (s *Service) Export(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "export", envSlug) {
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
	rows, err := s.DB.Query(`SELECT s.key, v.ciphertext,s.id,s.latest_version FROM secrets s
		JOIN secret_versions v ON v.secret_id = s.id AND v.version = s.latest_version
		WHERE s.env_id = ? ORDER BY s.key`, envID)
	if err != nil {
		internalErr(w, err)
		return
	}
	defer rows.Close()
	type kv struct {
		k, ct, id, value string
		version          int
	}
	var list []kv
	for rows.Next() {
		var x kv
		if err := rows.Scan(&x.k, &x.ct, &x.id, &x.version); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "export read failed"))
			return
		}
		list = append(list, x)
	}
	rows.Close()
	for i := range list {
		value, err := tc.Open(dek, list[i].ct, tc.SecretContext(p.OrgID, p.ID, envID, list[i].id, list[i].version))
		if err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", "export decryption failed"))
			return
		}
		list[i].value = value
		if queryBool(r, "resolve") {
			resolved, aerr := s.resolveStored(r, p.OrgID, p, value)
			if aerr != nil {
				writeErr(w, aerr)
				return
			}
			list[i].value = resolved
		}
	}
	if err := auth.AuditReq(s.DB, r, p.OrgID, u, "secrets.export", "project/"+p.Slug+"/env/"+envSlug,
		map[string]any{"count": len(list)}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+p.Slug+"-"+envSlug+".env\"")
	w.Header().Set("Cache-Control", "no-store")
	for _, x := range list {
		_, _ = w.Write([]byte(x.k + "=" + x.value + "\n"))
	}
}

// GET /secrets?env=dev —— 列表（无值）
func (s *Service) List(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "read", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	canRev := auth.Can(s.DB, u, p.OrgID, p.ID, "reveal", envSlug)
	canWrite := auth.Can(s.DB, u, p.OrgID, p.ID, "write", envSlug)
	canDelete := auth.Can(s.DB, u, p.OrgID, p.ID, "delete", envSlug)
	canRollback := auth.Can(s.DB, u, p.OrgID, p.ID, "rollback", envSlug)
	permissions := map[string]bool{"write": canWrite, "reveal": canRev, "delete": canDelete, "rollback": canRollback, "export": auth.Can(s.DB, u, p.OrgID, p.ID, "export", envSlug)}
	// 支持 ?path=/a/b/ 按文件夹过滤；缺省列出整个环境
	sql := `SELECT s.id, f.path, s.key, s.comment, s.tags, s.latest_version, s.updated_at
		FROM secrets s JOIN folders f ON f.id = s.folder_id WHERE s.env_id = ?`
	args := []any{envID}
	if r.URL.Query().Get("path") != "" {
		fid, _, ferr := s.folderRef(envID, r.URL.Query().Get("path"), false)
		if ferr != nil {
			writeJSON(w, 200, map[string]any{"secrets": []Meta{}, "permissions": permissions})
			return
		}
		sql += ` AND s.folder_id = ?`
		args = append(args, fid)
	}
	sql += ` ORDER BY f.path, s.key`
	rows, err := s.DB.Query(sql, args...)
	if err != nil {
		internalErr(w, err)
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
		m.CanWrite = canWrite
		m.CanDelete = canDelete
		m.CanRollback = canRollback
		out = append(out, m)
		count++
	}
	_ = count
	writeJSON(w, 200, map[string]any{"secrets": out, "permissions": permissions})
}

// POST /secrets?env=dev —— 创建/更新（产生新版本）
func (s *Service) Upsert(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "write", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	var b struct {
		Key             string   `json:"key"`
		Value           *string  `json:"value"`
		Comment         *string  `json:"comment"`
		Tags            []string `json:"tags"`
		ExpectedVersion *int     `json:"expectedVersion,omitempty"`
		Reason          string   `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Key == "" || b.Value == nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if s.checkReplay(w, r, p.OrgID, b) {
		return
	}
	// #16：key 与 folder 分离设计 —— 禁止路径分隔符，否则写入后无法经 {key} 路由读取
	if !validKey(b.Key) {
		writeErr(w, apperr.New(400, "INVALID_KEY", "key must not contain '/' or be '.'/'..'"))
		return
	}
	normPath, err := folder.NormalizePath(q(r, "path", "/"))
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "invalid folder path"))
		return
	}
	comment := ""
	if b.Comment != nil {
		comment = *b.Comment
	}
	if !applying(r) && s.queueApproval(w, r, p, envSlug, approvalOp{
		Action: "upsert", OrgID: p.OrgID, EnvID: envID, Key: b.Key, Folder: normPath, Value: *b.Value,
		Comment: comment, Tags: b.Tags, Expected: b.ExpectedVersion, Reason: b.Reason,
	}, b) {
		return
	}
	folderID, normPath, err := s.folderRef(envID, q(r, "path", "/"), true)
	if err != nil {
		writeErr(w, apperr.New(400, "INVALID", "invalid folder path"))
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	// 事务：读旧版本 → 写新版本 → 推进 latest_version
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	if s.replayMutation(tx, w, r, p.OrgID, b) {
		return
	}
	if s.claimApproval(tx, w, r, approvalOp{
		Action: "upsert", OrgID: p.OrgID, EnvID: envID, Key: b.Key, Folder: normPath, Value: *b.Value,
		Comment: comment, Tags: b.Tags, Expected: b.ExpectedVersion,
	}) {
		return
	}
	var secretID string
	var latest int
	var oldComment, oldTags string
	err = tx.QueryRow(`SELECT id, latest_version, comment, tags FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`,
		envID, folderID, b.Key).Scan(&secretID, &latest, &oldComment, &oldTags)
	isCreate := err == sql.ErrNoRows
	if err != nil && !isCreate {
		internalErr(w, err)
		return
	}
	if b.ExpectedVersion != nil && *b.ExpectedVersion != latest {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
		return
	}
	if isCreate {
		secretID = tc.NewID()
		if _, err := tx.Exec(`INSERT INTO secrets (id, env_id, project_id, folder, folder_id, key, comment, tags) VALUES (?, ?, (SELECT project_id FROM environments WHERE id=?), ?, ?, ?, ?, ?)`,
			secretID, envID, envID, normPath, folderID, b.Key, strOr(b.Comment, ""), tagsOr(b.Tags)); err != nil {
			if store.IsUniqueViolation(err) {
				writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret was created concurrently; reload before retrying"))
				return
			}
			internalErr(w, err)
			return
		}
	} else {
		if _, err := tx.Exec(`UPDATE secrets SET comment = ?, tags = ?, updated_at = datetime('now') WHERE id = ?`,
			strOr(b.Comment, oldComment), tagsOrDefault(b.Tags, oldTags), secretID); err != nil {
			internalErr(w, err)
			return
		}
	}
	next := latest + 1
	ct, err := tc.Seal(dek, *b.Value, tc.SecretContext(p.OrgID, p.ID, envID, secretID, next))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encrypt failed"))
		return
	}
	if res, err := tx.Exec(`UPDATE secrets SET latest_version = ? WHERE id = ? AND latest_version=?`, next, secretID, latest); !updatedOne(res, err) {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
		return
	}
	if _, err := tx.Exec(`INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
		tc.NewID(), secretID, next, ct, u.ID); err != nil {
		internalErr(w, err)
		return
	}

	action := "secrets.update"
	eventAction := "updated"
	if isCreate {
		action = "secrets.create"
		eventAction = "created"
	}
	if err := auth.AuditReq(tx, r, p.OrgID, u, action, "project/"+p.Slug+"/env/"+envSlug+"/secret/"+b.Key,
		map[string]any{"version": next}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	_, err = s.enqueue(tx, r, p, envSlug, normPath, b.Key, next, eventAction, secretID, envID)
	if err != nil {
		internalErr(w, err)
		return
	}
	if err := saveMutation(tx, r, p.OrgID, map[string]any{"key": b.Key, "version": next}); err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": b.Key, "version": next})
}

// GET /secrets/{key}?env=dev —— reveal 取明文（记审计）
func (s *Service) Reveal(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "reveal", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	folderID, _, ferr := s.folderRef(envID, q(r, "path", "/"), false)
	if ferr != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var secretID string
	var comment, tags string
	var latest int
	err = s.DB.QueryRow(`SELECT id, comment, tags, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`,
		envID, folderID, key).Scan(&secretID, &comment, &tags, &latest)
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
	val, err := tc.Open(dek, ct, tc.SecretContext(p.OrgID, p.ID, envID, secretID, latest))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "decrypt failed"))
		return
	}
	resolved := false
	if queryBool(r, "resolve") {
		next, aerr := s.resolveStored(r, p.OrgID, p, val)
		if aerr != nil {
			writeErr(w, aerr)
			return
		}
		val = next
		resolved = true
	}
	if err := auth.AuditReq(s.DB, r, p.OrgID, u, "secrets.reveal", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"version": latest, "resolved": resolved}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	var tagsArr []string
	_ = json.Unmarshal([]byte(tags), &tagsArr)
	body := map[string]any{
		"key": key, "value": val, "version": latest, "comment": comment, "tags": tagsArr,
	}
	if refs := referenceLiterals(val); len(refs) > 0 && !resolved {
		body["references"] = refs
	}
	if resolved {
		body["resolved"] = true
	}
	writeJSON(w, 200, body)
}

// GET /secrets/{key}/versions/{version} —— 读取某一历史版本的明文（reveal，默认掩码由界面负责）
func (s *Service) RevealVersion(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "reveal", envSlug) {
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version < 1 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	folderID, _, ferr := s.folderRef(envID, q(r, "path", "/"), false)
	if ferr != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var secretID string
	if err = s.DB.QueryRow(`SELECT id FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`, envID, folderID, key).Scan(&secretID); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var ct string
	if err = s.DB.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id = ? AND version = ?`, secretID, version).Scan(&ct); err != nil {
		writeErr(w, apperr.VersionNotFound)
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "dek unwrap failed"))
		return
	}
	val, err := tc.Open(dek, ct, tc.SecretContext(p.OrgID, p.ID, envID, secretID, version))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "decrypt failed"))
		return
	}
	resolved := false
	if queryBool(r, "resolve") {
		next, aerr := s.resolveStored(r, p.OrgID, p, val)
		if aerr != nil {
			writeErr(w, aerr)
			return
		}
		val = next
		resolved = true
	}
	if err := auth.AuditReq(s.DB, r, p.OrgID, u, "secrets.reveal", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"version": version, "resolved": resolved}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	body := map[string]any{"key": key, "version": version, "value": val, "resolved": resolved}
	writeJSON(w, 200, body)
}

// GET /secrets/{key}/versions
func (s *Service) Versions(w http.ResponseWriter, r *http.Request) {
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "read", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	folderID, _, ferr := s.folderRef(envID, q(r, "path", "/"), false)
	if ferr != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var secretID string
	var latest int
	if err := s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`,
		envID, folderID, key).Scan(&secretID, &latest); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	canRollback := auth.Can(s.DB, auth.From(r), p.OrgID, p.ID, "rollback", envSlug)
	rows, err := s.DB.Query(`SELECT version, created_by, created_at FROM secret_versions
		WHERE secret_id = ? ORDER BY version DESC`, secretID)
	if err != nil {
		internalErr(w, err)
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
	writeJSON(w, 200, map[string]any{"key": key, "latest": latest, "versions": out, "canRollback": canRollback})
}

// POST /secrets/{key}/rollback {version}
func (s *Service) Rollback(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "rollback", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	var b struct {
		Version         int  `json:"version"`
		ExpectedVersion *int `json:"expectedVersion,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Version < 1 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if s.checkReplay(w, r, p.OrgID, b) {
		return
	}
	key := chi.URLParam(r, "key")
	folderID, norm, ferr := s.folderRef(envID, q(r, "path", "/"), false)
	if ferr != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var secretID string
	var latest int
	if err := s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`,
		envID, folderID, key).Scan(&secretID, &latest); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var ct string
	if err := s.DB.QueryRow(`SELECT ciphertext FROM secret_versions WHERE secret_id = ? AND version = ?`,
		secretID, b.Version).Scan(&ct); err != nil {
		writeErr(w, apperr.VersionNotFound)
		return
	}
	if !applying(r) && s.queueApproval(w, r, p, envSlug, approvalOp{
		Action: "rollback", OrgID: p.OrgID, EnvID: envID, Key: key, Folder: norm, Expected: b.ExpectedVersion, Rollback: b.Version,
	}, b) {
		return
	}
	dek, err := s.orgDEK(p.OrgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "DEK unavailable"))
		return
	}
	value, err := tc.Open(dek, ct, tc.SecretContext(p.OrgID, p.ID, envID, secretID, b.Version))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "decrypt failed"))
		return
	}
	ct, err = tc.Seal(dek, value, tc.SecretContext(p.OrgID, p.ID, envID, secretID, latest+1))
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "encrypt failed"))
		return
	}
	next := latest + 1
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	if s.replayMutation(tx, w, r, p.OrgID, b) {
		return
	}
	if s.claimApproval(tx, w, r, approvalOp{
		Action: "rollback", OrgID: p.OrgID, EnvID: envID, Key: key, Folder: norm, Expected: b.ExpectedVersion, Rollback: b.Version,
	}) {
		return
	}
	if b.ExpectedVersion != nil && *b.ExpectedVersion != latest {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
		return
	}
	if res, err := tx.Exec(`UPDATE secrets SET latest_version = ?, updated_at = datetime('now') WHERE id = ? AND latest_version=?`,
		next, secretID, latest); !updatedOne(res, err) {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
		return
	}
	if _, err := tx.Exec(`INSERT INTO secret_versions (id, secret_id, version, ciphertext, created_by) VALUES (?, ?, ?, ?, ?)`,
		tc.NewID(), secretID, next, ct, u.ID); err != nil {
		internalErr(w, err)
		return
	}

	if err := auth.AuditReq(tx, r, p.OrgID, u, "secrets.rollback", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"from": b.Version, "to": next}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	_, err = s.enqueue(tx, r, p, envSlug, norm, key, next, "rolled_back", secretID, envID)
	if err != nil {
		internalErr(w, err)
		return
	}
	if err := saveMutation(tx, r, p.OrgID, map[string]any{"key": key, "version": next, "rolledBackFrom": b.Version}); err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "version": next, "rolledBackFrom": b.Version})
}

// DELETE /secrets/{key}?env=dev —— 删除密钥（版本级联删除；事件驱动 webhook/sync 感知）
func (s *Service) Delete(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	p := projectOf(r)
	envSlug := q(r, "env", "dev")
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, "delete", envSlug) {
		return
	}
	envID, err := s.envOf(p.ID, envSlug)
	if err != nil {
		writeErr(w, apperr.EnvNotFound)
		return
	}
	if s.checkReplay(w, r, p.OrgID, nil) {
		return
	}
	key := chi.URLParam(r, "key")
	folderID, normPath, ferr := s.folderRef(envID, q(r, "path", "/"), false)
	if ferr != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var secretID string
	var latest int
	if err := s.DB.QueryRow(`SELECT id, latest_version FROM secrets WHERE env_id = ? AND folder_id = ? AND key = ?`,
		envID, folderID, key).Scan(&secretID, &latest); err != nil {
		writeErr(w, apperr.SecretNotFound)
		return
	}
	var expectedPtr *int
	if expected := r.URL.Query().Get("expectedVersion"); expected != "" {
		version, e := strconv.Atoi(expected)
		if e != nil || version < 1 {
			writeErr(w, apperr.InvalidInput)
			return
		}
		if version != latest {
			writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
			return
		}
		expectedPtr = &version
	}
	if !applying(r) && s.queueApproval(w, r, p, envSlug, approvalOp{
		Action: "delete", OrgID: p.OrgID, EnvID: envID, Key: key, Folder: normPath, Expected: expectedPtr,
	}, nil) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		internalErr(w, err)
		return
	}
	defer tx.Rollback()
	if s.replayMutation(tx, w, r, p.OrgID, nil) {
		return
	}
	if s.claimApproval(tx, w, r, approvalOp{
		Action: "delete", OrgID: p.OrgID, EnvID: envID, Key: key, Folder: normPath, Expected: expectedPtr,
	}) {
		return
	}
	if res, err := tx.Exec(`DELETE FROM secrets WHERE id = ? AND latest_version=?`, secretID, latest); !updatedOne(res, err) {
		writeErr(w, apperr.New(409, "VERSION_CONFLICT", "secret changed; reload before retrying"))
		return
	}
	if err := auth.AuditReq(tx, r, p.OrgID, u, "secrets.delete", "project/"+p.Slug+"/env/"+envSlug+"/secret/"+key,
		map[string]any{"version": latest}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", "audit failed"))
		return
	}
	_, err = s.enqueue(tx, r, p, envSlug, normPath, key, latest, "deleted", secretID, envID)
	if err != nil {
		internalErr(w, err)
		return
	}
	if err := saveMutation(tx, r, p.OrgID, map[string]any{"key": key, "deleted": true}); err != nil {
		writeErr(w, apperr.New(503, "MUTATION_UNAVAILABLE", "retry with the same idempotency key"))
		return
	}
	if err := tx.Commit(); err != nil {
		internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"key": key, "deleted": true})
}

// ---------- helpers ----------

// ProjectCtx 保持对 org 等模块的兼容别名
type ProjectCtx = project.Ctx

func projectOf(r *http.Request) ProjectCtx { return project.Of(r) }

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

func internalErr(w http.ResponseWriter, err error) {
	slog.Error("secret internal error", "err", err)
	writeErr(w, apperr.New(500, "INTERNAL", "internal error"))
}

func updatedOne(result sql.Result, err error) bool {
	if err != nil {
		return false
	}
	n, err := result.RowsAffected()
	return err == nil && n == 1
}
