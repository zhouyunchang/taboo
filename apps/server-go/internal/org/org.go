// 组织/项目 + 认证（注册/登录/refresh）+ 审计查询
package org

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/pathcode"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

type Service struct {
	// Exact local account lookup is only used after owner authorization in
	// AddMember; tenant handlers cannot query the global identity directory.
	LookupLocalUser func(string) (string, error)
	DB              *sql.DB
	ControlDB       *sql.DB // Unscoped connection used to create a new tenant.
	MasterKey       []byte
	JWTSecret       string
	DEKs            *tc.DEKCache
	DataDir         string // M5 #14：审计导出文件落盘目录（空 = 禁用异步导出）
	DisableRegister bool
	DisablePassword bool
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

// ---------- 路由 ----------

func (s *Service) PublicRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", s.Register)
		// 登录限流：5 次/分钟/IP（设计文档 §8）
		r.With(auth.LoginRateLimit(s.DB, time.Minute, 5)).Post("/login", s.Login)
		r.Post("/refresh", s.Refresh)
	})
}

func (s *Service) Routes(r chi.Router) {
	r.Get("/me", s.Me)
	r.Patch("/me/handle", s.UpdateHandle)
	r.Post("/me/handle/confirm", s.ConfirmHandle)
	r.Post("/orgs", s.CreateOrganization)
	r.Post("/invites/{token}/accept", s.AcceptInvite)
	r.Route("/orgs/{slug}", func(r chi.Router) {
		r.Get("/projects", s.ListProjects)
		r.Post("/projects", s.CreateProject)
		r.Get("/members", s.ListMembers)
		r.Post("/members", s.AddMember)
		r.Patch("/members/{userId}", s.PatchMember)
		r.Delete("/members/{userId}", s.RemoveMember)
		r.Put("/members/{userId}/grants", s.PutGrants)
		r.Get("/members/{userId}/actions", s.ListActionGrants)
		r.Put("/members/{userId}/actions", s.PutActionGrants)
		r.Get("/security", s.Security)
		r.Put("/security", s.PutSecurity)
		r.Post("/invites", s.CreateInvite)
		r.Get("/audit", s.Audit)
		r.Get("/audit/verify", s.AuditVerify)
		r.Get("/audit/export", s.AuditExport)
		r.Get("/audit/exports", s.ListAuditExports)
		r.Get("/audit/exports/{id}", s.AuditExportDownload)
		r.Post("/audit/exports/{id}/retry", s.RetryAuditExport)
	})
}

// ---------- 认证 ----------

type tokens struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

func (s *Service) Register(w http.ResponseWriter, r *http.Request) {
	if s.DisableRegister {
		writeErr(w, apperr.New(403, "REGISTER_DISABLED", "registration is disabled; use the configured identity source"))
		return
	}
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
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	created, cerr := CreateLocalUser(tx, s.MasterKey, email, b.Name, hash)
	if cerr != nil {
		writeErr(w, cerr)
		return
	}
	userID, orgID := created.UserID, created.OrgID
	u := &auth.Actor{ID: userID, Email: email, Name: b.Name, Kind: auth.KindUser}

	tk, err := auth.IssueTokens(tx, s.JWTSecret, u)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if !auth.CommitMutation(tx, w, r, orgID, u, "auth.register", "user/"+email, nil) {
		return
	}
	writeJSON(w, 201, map[string]any{"user": u, "tokens": tk})
}

// NewLocalUser 是 CreateLocalUser 的结果。
type NewLocalUser struct{ UserID, OrgID, Slug string }

// CreateLocalUser 在调用方事务内创建本地账号和仅本人可用的个人空间（DEK、默认项目、
// dev/staging/prod）。注册接口与运维 CLI（taboo-server user create）共用。组织要另外创建。
func CreateLocalUser(tx *sql.Tx, masterKey []byte, email, name, passwordHash string) (NewLocalUser, *apperr.Error) {
	userID := tc.NewID()
	if _, err := tx.Exec(`INSERT INTO users (id, email, name, password_hash, auth_source) VALUES (?, ?, ?, ?, 'local')`,
		userID, email, name, passwordHash); err != nil {
		return NewLocalUser{}, apperr.New(500, "INTERNAL", err.Error())
	}
	code, err := userHandle(tx, userID, email)
	if err != nil {
		if errors.Is(err, pathcode.ErrTaken) || errors.Is(err, pathcode.ErrInvalid) {
			return NewLocalUser{}, apperr.New(409, "CODE_TAKEN", "this code is already used")
		}
		return NewLocalUser{}, apperr.New(500, "INTERNAL", err.Error())
	}
	orgID, slug, aerr := provisionTenant(tx, masterKey, personalLabel(name, email), code, KindPersonal, userID)
	if aerr != nil {
		return NewLocalUser{}, aerr
	}
	return NewLocalUser{UserID: userID, OrgID: orgID, Slug: slug}, nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// ensureRootFolder 为环境补建根文件夹（幂等，已存在则忽略冲突）
func ensureRootFolder(db interface {
	Exec(string, ...any) (sql.Result, error)
}, envID string) error {
	_, err := db.Exec(`INSERT INTO folders (id, env_id, parent_id, name, path) VALUES (?, ?, NULL, '/', '/') ON CONFLICT(env_id,path) DO NOTHING`,
		tc.NewID(), envID)
	return err
}

func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	if s.DisablePassword {
		writeErr(w, apperr.New(403, "PASSWORD_DISABLED", "password login is disabled; use SSO"))
		return
	}
	var b struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	email := strings.ToLower(strings.TrimSpace(b.Email))
	var userID, name, hash, status string
	err := s.DB.QueryRow(`SELECT id, name, password_hash, status FROM users WHERE email = ? AND auth_source='local'`, email).
		Scan(&userID, &name, &hash, &status)
	ok, needsRehash := false, false
	if err == nil {
		ok, needsRehash, _ = tc.CheckPassword(b.Password, hash)
	}
	if err != nil || !ok {
		_ = auth.AuditReq(s.DB, r, "-", nil, "auth.login.failed", "user/"+email, map[string]any{"reason": "bad_credentials"})
		writeErr(w, apperr.BadCredentials)
		return
	}
	if status != "active" {
		// 停用账号：与密码错误同一响应，不泄露账号状态
		_ = auth.AuditReq(s.DB, r, "-", nil, "auth.login.failed", "user/"+email, map[string]any{"reason": "account_" + status})
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
		if err := auth.AuditReq(s.DB, r, orgID, u, "auth.login", "user/"+email, nil); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
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
	if err := s.DB.QueryRow(`SELECT id, email, name FROM users WHERE id = ? AND auth_source='local' AND status='active'`, userID).
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
		ID         string `json:"id"`
		Name       string `json:"name"`
		Slug       string `json:"slug"`
		Kind       string `json:"kind"`
		Role       string `json:"role"`
		Restricted bool   `json:"restricted"`
	}
	// Organizations come first so a user who also has an empty personal space
	// lands on the tenant that already holds shared keys.
	rows, err := s.DB.Query(`SELECT o.id, o.name, o.slug, o.kind, m.role, COALESCE(m.restricted,0) FROM orgs o
		JOIN org_members m ON m.org_id = o.id WHERE m.user_id = ?
		ORDER BY CASE WHEN o.kind = 'organization' THEN 0 ELSE 1 END, o.created_at, o.id`, u.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	orgs := []orgRow{}
	for rows.Next() {
		var o orgRow
		var rest int
		_ = rows.Scan(&o.ID, &o.Name, &o.Slug, &o.Kind, &o.Role, &rest)
		o.Restricted = rest == 1
		if u.OrganizationID != "" && u.OrganizationID != o.ID {
			continue
		}
		orgs = append(orgs, o)
	}
	rows.Close() // release the single SQLite connection before the next query
	visible := orgs[:0]
	for _, o := range orgs {
		if auth.OrganizationLoginAllowed(s.DB, u, o.ID) {
			visible = append(visible, o)
		}
	}
	orgs = visible
	handle, confirmed := handleOf(s.DB, u.ID)
	writeJSON(w, 200, map[string]any{"user": meUser{Actor: *u, Handle: handle, HandleConfirmed: confirmed}, "orgs": orgs, "totp_enabled": totpEnabledOf(s.DB, u.ID)})
}

type meUser struct {
	auth.Actor
	Handle          string `json:"handle"`
	HandleConfirmed bool   `json:"handle_confirmed"`
}

func handleOf(db *sql.DB, userID string) (string, bool) {
	var handle string
	var confirmed int
	if err := db.QueryRow(`SELECT COALESCE(handle,''), COALESCE(handle_confirmed,0) FROM users WHERE id=?`, userID).Scan(&handle, &confirmed); err != nil {
		return "", false
	}
	return handle, confirmed == 1
}

func (s *Service) UpdateHandle(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	if u == nil || u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	var b struct {
		Handle string `json:"handle"`
	}
	if jsonDecode(r, &b) != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	code := strings.ToLower(strings.TrimSpace(b.Handle))
	if !pathcode.Valid(code) {
		writeErr(w, apperr.New(400, "INVALID_CODE", "code must be 1-39 lowercase letters, digits, or inner hyphens"))
		return
	}
	tx, started := auth.BeginMutation(s.controlDB(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	var current string
	var confirmed int
	if err := tx.QueryRow(`SELECT COALESCE(handle,''), COALESCE(handle_confirmed,0) FROM users WHERE id=?`, u.ID).Scan(&current, &confirmed); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if confirmed == 1 {
		writeErr(w, apperr.New(409, "HANDLE_LOCKED", "the username is confirmed and can no longer change"))
		return
	}
	if current != code {
		if err := pathcode.Claim(tx, code, pathcode.KindUser, u.ID); err != nil {
			if errors.Is(err, pathcode.ErrTaken) {
				writeErr(w, apperr.New(409, "CODE_TAKEN", "this code is already used"))
				return
			}
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		if _, err := tx.Exec(`UPDATE users SET handle=? WHERE id=?`, code, u.ID); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		if _, err := tx.Exec(`UPDATE orgs SET slug=? WHERE kind='personal' AND owner_user_id=?`, code, u.ID); err != nil {
			if isUnique(err) {
				writeErr(w, apperr.New(409, "CODE_TAKEN", "this code is already used"))
				return
			}
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		if current != "" {
			if err := pathcode.Release(tx, current); err != nil {
				writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
				return
			}
		}
	}
	orgID := personalOrgID(tx, u.ID)
	if !auth.CommitMutation(tx, w, r, orgID, u, "user.handle", "user/"+u.ID, map[string]any{"handle": code}) {
		return
	}
	writeJSON(w, 200, map[string]any{"handle": code, "handle_confirmed": false})
}

func (s *Service) ConfirmHandle(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	if u == nil || u.Kind != auth.KindUser {
		writeErr(w, apperr.Forbidden)
		return
	}
	tx, started := auth.BeginMutation(s.controlDB(), w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	var handle string
	var confirmed int
	if err := tx.QueryRow(`SELECT COALESCE(handle,''), COALESCE(handle_confirmed,0) FROM users WHERE id=?`, u.ID).Scan(&handle, &confirmed); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if handle == "" || !pathcode.Valid(handle) {
		writeErr(w, apperr.New(400, "INVALID_CODE", "choose a username before confirming it"))
		return
	}
	if confirmed == 0 {
		if _, err := tx.Exec(`UPDATE users SET handle_confirmed=1 WHERE id=?`, u.ID); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
		if !auth.CommitMutation(tx, w, r, personalOrgID(tx, u.ID), u, "user.handle.confirm", "user/"+u.ID, map[string]any{"handle": handle}) {
			return
		}
	}
	writeJSON(w, 200, map[string]any{"handle": handle, "handle_confirmed": true})
}

func personalOrgID(q interface {
	QueryRow(string, ...any) *sql.Row
}, userID string) string {
	var id string
	_ = q.QueryRow(`SELECT id FROM orgs WHERE kind='personal' AND owner_user_id=?`, userID).Scan(&id)
	return id
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
	u := auth.From(r)
	role, restricted, _ := auth.MemberInfo(s.DB, orgID, u.ID)
	q := `SELECT id, name, slug, created_at, approval_required FROM projects WHERE org_id = ? ORDER BY created_at`
	args := []any{orgID}
	if restricted && !rbac.Privileged(role) {
		q = `SELECT p.id, p.name, p.slug, p.created_at, p.approval_required FROM projects p
			JOIN project_grants g ON g.project_id = p.id AND g.user_id = ?
			WHERE p.org_id = ? ORDER BY p.created_at`
		args = []any{u.ID, orgID}
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer rows.Close()
	type proj struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Created  string `json:"created_at"`
		Approval bool   `json:"approval_required"`
	}
	out := []proj{}
	for rows.Next() {
		var p proj
		var approval int
		_ = rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Created, &approval)
		p.Approval = approval == 1
		out = append(out, p)
	}
	writeJSON(w, 200, map[string]any{"projects": out})
}

func (s *Service) CreateProject(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.Admin, "") {
		return
	}
	var b struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	if strings.TrimSpace(b.Name) == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	slug := strings.ToLower(strings.TrimSpace(b.Code))
	if slug == "" {
		slug = slugify(b.Name)
	}
	if !pathcode.ValidToken(slug) {
		writeErr(w, apperr.New(400, "INVALID_CODE", "project code must be 1-39 lowercase letters, digits, or inner hyphens"))
		return
	}
	var existing string
	err := s.DB.QueryRow(`SELECT id FROM projects WHERE org_id = ? AND slug = ?`, orgID, slug).Scan(&existing)
	if err == nil {
		writeErr(w, apperr.New(409, "CODE_TAKEN", "this project code is already used"))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	pid := tc.NewID()
	if _, err := tx.Exec(`INSERT INTO projects (id, org_id, name, slug) VALUES (?, ?, ?, ?)`,
		pid, orgID, b.Name, slug); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	envID := tc.NewID()
	if _, err := tx.Exec(`INSERT INTO environments (id, project_id, name, slug, sort_order, protected, approval_required) VALUES (?, ?, 'dev', 'dev', 0, 0, 0)`,
		envID, pid); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "change was not committed"))
		return
	}
	if err := ensureRootFolder(tx, envID); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "change was not committed"))
		return
	}

	if !auth.CommitMutation(tx, w, r, orgID, auth.From(r), "project.create", "project/"+slug, nil) {
		return
	}
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
	if !auth.ProjectVisible(s.DB, u, p.OrgID, p.ID) {
		auth.Deny(s.DB, w, r, p.OrgID, "read", "project/"+p.Slug)
		return p, false
	}
	return p, true
}

// soleEnvID 是项目里唯一保留的环境。对外接口不再暴露环境，授权仍落到这一行。
func soleEnvID(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, orgID, projectID string) (string, error) {
	var id string
	err := q.QueryRow(`SELECT e.id FROM environments e JOIN projects p ON p.id = e.project_id WHERE p.org_id = ? AND p.id = ? ORDER BY e.sort_order LIMIT 1`, orgID, projectID).Scan(&id)
	return id, err
}

func (s *Service) PatchProject(w http.ResponseWriter, r *http.Request) {
	p, ok := s.projectOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, p.OrgID, p.ID, rbac.Admin, "") {
		return
	}
	var b struct {
		ApprovalRequired *bool `json:"approval_required"`
	}
	if json.NewDecoder(r.Body).Decode(&b) != nil || b.ApprovalRequired == nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	flag := 0
	if *b.ApprovalRequired {
		flag = 1
	}
	tx, started := auth.BeginMutation(s.DB, w, r)
	if !started {
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE projects SET approval_required=? WHERE id=?`, flag, p.ID); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if _, err := tx.Exec(`UPDATE environments SET approval_required=? WHERE project_id=?`, flag, p.ID); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if !auth.CommitMutation(tx, w, r, p.OrgID, auth.From(r), "project.approval", "project/"+p.Slug, map[string]any{"approval_required": flag == 1}) {
		return
	}
	writeJSON(w, 200, map[string]any{"id": p.ID, "approval_required": flag == 1})
}

// ---------- 审计查询 ----------

func (s *Service) AuditVerify(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditRead, "") {
		return
	}
	res, err := audit.Verify(s.DB, orgID, s.MasterKey)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, res)
}

func (s *Service) Audit(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !auth.Require(s.DB, w, r, orgID, "", rbac.AuditRead, "") {
		return
	}
	_ = auth.AuditReq(s.DB, r, orgID, auth.From(r), "audit.read", "org/audit", nil)
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
