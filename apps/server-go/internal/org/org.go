// 组织/项目/环境 + 认证（注册/登录/refresh）+ 审计查询
package org

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	JWTSecret string
	DEKs      *tc.DEKCache
	DataDir   string // M5 #14：审计导出文件落盘目录（空 = 禁用异步导出）
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "org"
	}
	return out
}

func ipOf(r *http.Request) string { return r.RemoteAddr }

// ---------- 路由 ----------

func (s *Service) PublicRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", s.Register)
		// 登录限流：5 次/分钟/IP（设计文档 §8）
		r.With(auth.LoginRateLimit(time.Minute, 5)).Post("/login", s.Login)
		r.Post("/refresh", s.Refresh)
	})
}

func (s *Service) Routes(r chi.Router) {
	r.Get("/me", s.Me)
	r.Route("/orgs/{slug}", func(r chi.Router) {
		r.Get("/projects", s.ListProjects)
		r.Post("/projects", s.CreateProject)
		r.Get("/audit", s.Audit)
		r.Get("/audit/export", s.AuditExport)
		r.Get("/audit/exports", s.ListAuditExports)
		r.Get("/audit/exports/{id}", s.AuditExportDownload)
	})
	// 注意：/projects/{pid} 作用域在 internal/server 统一挂载（含密钥路由），此处只提供 handler
}

// ---------- 认证 ----------

type tokens struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	email := strings.ToLower(strings.TrimSpace(b.Email))
	if !emailRe.MatchString(email) {
		writeErr(w, apperr.InvalidEmail)
		return
	}
	if len(b.Password) < 8 {
		writeErr(w, apperr.WeakPassword)
		return
	}
	var exists string
	if err := s.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&exists); err == nil {
		writeErr(w, apperr.EmailTaken)
		return
	}
	hash, err := tc.HashPassword(b.Password)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	userID := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)`,
		userID, email, b.Name, hash); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	// 个人组织（DEK 经 Root Key 加密落库）+ 默认项目 + dev/staging/prod
	orgID := tc.NewID()
	slug := slugify(strings.Split(email, "@")[0])
	for {
		var x string
		if err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&x); err != nil {
			break
		}
		slug += "-" + tc.NewID()[:4]
	}
	dek := randomBytes(tc.KeyLen)
	dekPlain, _ := tc.Encrypt(s.MasterKey, hexEncode(dek))
	if _, err := s.DB.Exec(`INSERT INTO orgs (id, name, slug, dek_encrypted) VALUES (?, ?, ?, ?)`,
		orgID, email+"'s org", slug, dekPlain); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if _, err := s.DB.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, 'owner')`, orgID, userID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	projectID := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, 'default', 'default')`, projectID, orgID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	for i, env := range []string{"dev", "staging", "prod"} {
		envID := tc.NewID()
		if _, err := s.DB.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`,
			envID, projectID, env, env, i); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		ensureRootFolder(s.DB, envID)
	}
	u := &auth.Actor{ID: userID, Email: email, Name: b.Name, Kind: auth.KindUser}
	auth.Audit(s.DB, orgID, u, "auth.register", "user/"+email, nil, ipOf(r))
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 201, map[string]any{"user": u, "tokens": tk})
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// ensureRootFolder 为环境补建根文件夹（幂等，已存在则忽略冲突）
func ensureRootFolder(db *sql.DB, envID string) {
	_, _ = db.Exec(`INSERT OR IGNORE INTO folders (id, env_id, parent_id, name, path) VALUES (?, ?, NULL, '/', '/')`,
		tc.NewID(), envID)
}

func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	email := strings.ToLower(strings.TrimSpace(b.Email))
	var userID, name, hash, status string
	err := s.DB.QueryRow(`SELECT id, name, password_hash, status FROM users WHERE email = ?`, email).
		Scan(&userID, &name, &hash, &status)
	ok, needsRehash := false, false
	if err == nil {
		ok, needsRehash, _ = tc.CheckPassword(b.Password, hash)
	}
	if err != nil || !ok {
		writeErr(w, apperr.BadCredentials)
		return
	}
	u := &auth.Actor{ID: userID, Email: email, Name: name, Kind: auth.KindUser}
	if needsRehash {
		// 旧 scrypt 哈希透明迁移至 Argon2id（issue #2）
		if newHash, err := tc.HashPassword(b.Password); err == nil {
			_, _ = s.DB.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, newHash, userID)
		}
	}
	// TOTP 2FA（M2 #4）：已开启 → 不下发 token，改发 5min challenge，二次验证后换 token
	var totpEnabled int
	_ = s.DB.QueryRow(`SELECT totp_enabled FROM users WHERE id = ?`, userID).Scan(&totpEnabled)
	if totpEnabled == 1 {
		challenge, err := tc.SignJWTClaims(map[string]any{"sub": userID, "typ": "totp"}, s.JWTSecret, 5*time.Minute)
		if err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		writeJSON(w, 200, map[string]any{"totp_required": true, "challenge": challenge})
		return
	}
	var orgID string
	if err := s.DB.QueryRow(`SELECT org_id FROM org_members WHERE user_id = ? LIMIT 1`, userID).Scan(&orgID); err == nil {
		auth.Audit(s.DB, orgID, u, "auth.login", "user/"+email, nil, ipOf(r))
	}
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"user": u, "tokens": tk})
}

func (s *Service) Refresh(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Refresh string `json:"refresh"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	var id, userID string
	var exp int64
	err := s.DB.QueryRow(`SELECT id, user_id, expires_at FROM refresh_tokens WHERE token_hash = ?`,
		tc.SHA256(b.Refresh)).Scan(&id, &userID, &exp)
	if err != nil || exp < time.Now().UnixMilli() {
		writeErr(w, apperr.InvalidRefresh)
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM refresh_tokens WHERE id = ?`, id) // 旋转
	var u auth.Actor
	u.Kind = auth.KindUser
	if err := s.DB.QueryRow(`SELECT id, email, name FROM users WHERE id = ?`, userID).
		Scan(&u.ID, &u.Email, &u.Name); err != nil {
		writeErr(w, apperr.InvalidRefresh)
		return
	}
	tk, err := auth.IssueTokens(s.DB, s.JWTSecret, &u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"tokens": tk})
}

// ---------- 组织/项目/环境 ----------

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	type orgRow struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
		Role  string `json:"role"`
	}
	rows, err := s.DB.Query(`SELECT o.id, o.name, o.slug, m.role FROM orgs o
		JOIN org_members m ON m.org_id = o.id WHERE m.user_id = ?`, u.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	orgs := []orgRow{}
	for rows.Next() {
		var o orgRow
		_ = rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Role)
		orgs = append(orgs, o)
	}
	writeJSON(w, 200, map[string]any{"user": u, "orgs": orgs, "totp_enabled": totpEnabledOf(s.DB, u.ID)})
}

func totpEnabledOf(db *sql.DB, userID string) bool {
	var n int
	_ = db.QueryRow(`SELECT totp_enabled FROM users WHERE id = ?`, userID).Scan(&n)
	return n == 1
}

func (s *Service) orgOf(w http.ResponseWriter, r *http.Request) (id, slug string, ok bool) {
	slug = chi.URLParam(r, "slug")
	err := s.DB.QueryRow(`SELECT id FROM orgs WHERE slug = ?`, slug).Scan(&id)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return "", slug, false
	}
	u := auth.From(r)
	if _, member := auth.Membership(s.DB, id, u.ID); !member {
		writeErr(w, apperr.Forbidden)
		return "", slug, false
	}
	return id, slug, true
}

func (s *Service) ListProjects(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, slug, created_at FROM projects WHERE org_id = ? ORDER BY created_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type proj struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Slug  string `json:"slug"`
		Created string `json:"created_at"`
	}
	out := []proj{}
	for rows.Next() {
		var p proj
		_ = rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Created)
		out = append(out, p)
	}
	writeJSON(w, 200, map[string]any{"projects": out})
}

func (s *Service) CreateProject(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if strings.TrimSpace(b.Name) == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	slug := slugify(b.Name)
	for {
		var x string
		if err := s.DB.QueryRow(`SELECT id FROM projects WHERE org_id = ? AND slug = ?`, orgID, slug).Scan(&x); err != nil {
			break
		}
		slug += "-" + tc.NewID()[:4]
	}
	pid := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, ?, ?)`,
		pid, orgID, b.Name, slug); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	for i, env := range []string{"dev", "staging", "prod"} {
		envID := tc.NewID()
		_, _ = s.DB.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`,
			envID, pid, env, env, i)
		ensureRootFolder(s.DB, envID)
	}
	auth.Audit(s.DB, orgID, auth.From(r), "project.create", "project/"+slug, nil, ipOf(r))
	writeJSON(w, 201, map[string]any{"id": pid, "name": b.Name, "slug": slug})
}

func (s *Service) projectOf(w http.ResponseWriter, r *http.Request) (secret.ProjectCtx, bool) {
	pid := chi.URLParam(r, "pid")
	var p secret.ProjectCtx
	err := s.DB.QueryRow(`SELECT id, org_id, slug FROM projects WHERE id = ?`, pid).Scan(&p.ID, &p.OrgID, &p.Slug)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return p, false
	}
	u := auth.From(r)
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "read", "") {
		writeErr(w, apperr.Forbidden)
		return p, false
	}
	return p, true
}

func (s *Service) ListEnvs(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectOf(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, slug, sort_order FROM environments WHERE project_id = ? ORDER BY sort_order`, p.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type env struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Sort   int    `json:"sort_order"`
	}
	out := []env{}
	for rows.Next() {
		var e env
		_ = rows.Scan(&e.ID, &e.Name, &e.Slug, &e.Sort)
		out = append(out, e)
	}
	writeJSON(w, 200, map[string]any{"environments": out})
}

func (s *Service) CreateEnv(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectOf(w, r)
	if !ok {
		return
	}
	var b struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if strings.TrimSpace(b.Name) == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	var maxSort int
	_ = s.DB.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) FROM environments WHERE project_id = ?`, p.ID).Scan(&maxSort)
	id := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order) VALUES (?, ?, ?, ?, ?)`,
		id, p.ID, b.Name, slugify(b.Name), maxSort+1); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	ensureRootFolder(s.DB, id)
	writeJSON(w, 201, map[string]any{"id": id, "name": b.Name})
}

// ---------- 审计查询 ----------

func (s *Service) Audit(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	f := parseAuditFilter(r)
	cond, args := buildAuditQuery(orgID, f)
	rows, err := s.DB.Query(`SELECT id, actor_name, action, resource, metadata, ip, created_at FROM audit_logs
		WHERE `+cond+` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, f.Limit)...)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type log struct {
		ID        string          `json:"id"`
		ActorName string          `json:"actor_name"`
		Action    string          `json:"action"`
		Resource  string          `json:"resource"`
		Metadata  json.RawMessage `json:"metadata"`
		IP        string          `json:"ip"`
		CreatedAt string          `json:"created_at"`
	}
	out := []log{}
	for rows.Next() {
		var l log
		var meta string
		_ = rows.Scan(&l.ID, &l.ActorName, &l.Action, &l.Resource, &meta, &l.IP, &l.CreatedAt)
		if meta == "" {
			meta = "{}"
		}
		l.Metadata = json.RawMessage(meta)
		out = append(out, l)
	}
	writeJSON(w, 200, map[string]any{"logs": out})
}
