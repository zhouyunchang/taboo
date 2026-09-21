// 动态密钥服务（M4 #9）—— 引擎配置 + lease 申请/续期/回收 + 后台回收 worker
//
//	POST   /projects/:pid/dynamic-engines                 建引擎配置（owner/admin）
//	GET    /projects/:pid/dynamic-engines                 引擎 + 活跃 lease 列表（不返连接串）
//	POST   /projects/:pid/dynamic-engines/:eid/lease      申请 lease（仅限机器身份，绑定 identity）
//	POST   /projects/:pid/dynamic-leases/:lid/renew       续期（lease 所属身份或 owner/admin）
//	POST   /projects/:pid/dynamic-leases/:lid/revoke      手动回收（lease 所属身份或 owner/admin）
//
// 安全：高权限连接串 Master Key 加密落库（自举）；lease 明文密码仅响应一次；身份吊销 → worker 联动回收。
package dynamic

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
)

const (
	DefaultTTL = time.Hour
	MaxTTL     = 24 * time.Hour
)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
	Engines   map[string]Engine // engine_id → 已建连接（内存缓存，进程内复用）
}

func New(db *sql.DB, masterKey []byte) *Service {
	return &Service{DB: db, MasterKey: masterKey, Engines: map[string]Engine{}}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) { writeJSON(w, e.Status, e) }

func (s *Service) Routes(r chi.Router) {
	r.Route("/dynamic-engines", func(r chi.Router) {
		r.Post("/", s.CreateEngine)
		r.Get("/", s.ListEngines)
		r.Post("/{eid}/lease", s.RequestLease)
	})
	r.Route("/dynamic-leases", func(r chi.Router) {
		r.Post("/{lid}/renew", s.RenewLease)
		r.Post("/{lid}/revoke", s.RevokeLease)
	})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// engineOf 取/建引擎连接（配置里的连接串解密后经 Factory 构造）
func (s *Service) engineOf(engineID, connEnc string) (Engine, error) {
	if e, ok := s.Engines[engineID]; ok {
		return e, nil
	}
	conn, err := tc.Decrypt(s.MasterKey, connEnc)
	if err != nil {
		return nil, err
	}
	e, err := Factory(conn)
	if err != nil {
		return nil, err
	}
	s.Engines[engineID] = e
	return e, nil
}

// mustRole owner/admin 关卡
func (s *Service) mustRole(w http.ResponseWriter, r *http.Request, p project.Ctx, action string) (*auth.Actor, bool) {
	u := auth.From(r)
	if u.Kind == auth.KindUser {
		role, ok := auth.Membership(s.DB, p.OrgID, u.ID)
		if !ok || (role != "owner" && role != "admin") {
			auth.Deny(s.DB, w, r, p.OrgID, "admin", "project/"+p.Slug+"/dynamic")
			return nil, false
		}
		return u, true
	}
	// 机器身份：不允许管理引擎配置
	writeErr(w, apperr.Forbidden)
	return nil, false
}

// leaseCaller 返回 (userActor 或 nil, identityID, ok)；lease 接口仅机器身份可申请，用户可管理
func (s *Service) leaseIdentity(w http.ResponseWriter, r *http.Request, p project.Ctx) (*auth.Actor, string, bool) {
	u := auth.From(r)
	if u.Kind == "identity" {
		// 吊销即断：实时校验身份状态
		var status string
		if err := s.DB.QueryRow(`SELECT status FROM machine_identities WHERE id = ?`, u.ID).Scan(&status); err != nil || status != "active" {
			writeErr(w, apperr.New(401, "IDENTITY_REVOKED", "machine identity is not active"))
			return nil, "", false
		}
		return u, u.ID, true
	}
	writeErr(w, apperr.New(403, "IDENTITY_REQUIRED", "lease endpoints require a machine identity token"))
	return nil, "", false
}

// CreateEngine POST /dynamic-engines {name, connection_string, database, default_ttl, max_ttl}
func (s *Service) CreateEngine(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	u, ok := s.mustRole(w, r, p, "dynamic.engine.create")
	if !ok {
		return
	}
	var b struct {
		Name           string `json:"name"`
		Connection     string `json:"connection_string"`
		Database       string `json:"database"`
		Type           string `json:"type"`
		DefaultTTLSecs int    `json:"default_ttl"`
		MaxTTLSecs     int    `json:"max_ttl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Name == "" || b.Connection == "" {
		writeErr(w, apperr.InvalidInput)
		return
	}
	typ := b.Type
	if typ == "" {
		typ = "postgres"
	}
	if typ != "postgres" {
		writeErr(w, apperr.New(400, "UNSUPPORTED", "only postgres engine is supported"))
		return
	}
	enc, err := tc.Encrypt(s.MasterKey, b.Connection)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	id := tc.NewID()
	if _, err := s.DB.Exec(`INSERT INTO dynamic_engines (id, org_id, project_id, name, type, conn_encrypted, database, default_ttl, max_ttl)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.OrgID, p.ID, b.Name, typ, enc, orDefault(b.Database, "postgres"),
		orDefaultInt(b.DefaultTTLSecs, int(DefaultTTL.Seconds())), orDefaultInt(b.MaxTTLSecs, int(MaxTTL.Seconds()))); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.engine.create", "project/"+p.Slug+"/engine/"+id,
		map[string]any{"name": b.Name, "type": typ})
	writeJSON(w, 201, map[string]any{"id": id, "name": b.Name, "type": typ})
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
func orDefaultInt(v, d int) int {
	if v <= 0 {
		return d
	}
	return v
}

// ListEngine GET /dynamic-engines —— 引擎元数据 + 活跃 lease（不含连接串/密码）
func (s *Service) ListEngines(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	u := auth.From(r)
	if !auth.Can(s.DB, u, p.OrgID, p.ID, "read", "") {
		writeErr(w, apperr.Forbidden)
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, type, database, default_ttl, max_ttl, created_at
		FROM dynamic_engines WHERE project_id = ? ORDER BY created_at`, p.ID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	type engineRow struct {
		id, name, typ, database, createdAt string
		defTTL, maxTTL                     int
	}
	var list []engineRow
	for rows.Next() {
		var e engineRow
		_ = rows.Scan(&e.id, &e.name, &e.typ, &e.database, &e.defTTL, &e.maxTTL, &e.createdAt)
		list = append(list, e)
	}
	rows.Close()
	// SQLite 单连接：必须先释放 engine 查询的 rows，再逐引擎查活跃 lease
	engines := []map[string]any{}
	for _, e := range list {
		engines = append(engines, map[string]any{
			"id": e.id, "name": e.name, "type": e.typ, "database": e.database,
			"default_ttl": e.defTTL, "max_ttl": e.maxTTL, "created_at": e.createdAt,
			"leases": s.activeLeases(e.id),
		})
	}
	writeJSON(w, 200, map[string]any{"engines": engines})
}

func (s *Service) activeLeases(engineID string) []map[string]any {
	rows, err := s.DB.Query(`SELECT id, username, status, expires_at, created_at
		FROM dynamic_leases WHERE engine_id = ? AND status = 'active' ORDER BY created_at DESC`, engineID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, username, status, createdAt string
		var exp int64
		_ = rows.Scan(&id, &username, &status, &exp, &createdAt)
		out = append(out, map[string]any{
			"id": id, "username": username, "status": status,
			"expires_at": exp, "seconds_left": max(0, exp-time.Now().Unix()), "created_at": createdAt,
		})
	}
	return out
}

// RequestLease POST /dynamic-engines/{eid}/lease {ttl?} —— 仅机器身份；绑定 identity
func (s *Service) RequestLease(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	u, identityID, ok := s.leaseIdentity(w, r, p)
	if !ok {
		return
	}
	// scope 关卡：身份须在本项目有任一环境 scope
	if !identityHasProjectScope(s.DB, identityID, p.ID) {
		writeErr(w, apperr.Forbidden)
		return
	}
	var connEnc, database string
	var defTTL, maxTTL int
	err := s.DB.QueryRow(`SELECT conn_encrypted, database, default_ttl, max_ttl FROM dynamic_engines
		WHERE id = ? AND project_id = ?`, chi.URLParam(r, "eid"), p.ID).
		Scan(&connEnc, &database, &defTTL, &maxTTL)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var b struct {
		TTLSecs int `json:"ttl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	ttl := time.Duration(orDefaultInt(b.TTLSecs, defTTL)) * time.Second
	if ttl <= 0 || ttl > time.Duration(maxTTL)*time.Second {
		writeErr(w, apperr.New(400, "INVALID_TTL", "ttl out of range"))
		return
	}

	engine, err := s.engineOf(chi.URLParam(r, "eid"), connEnc)
	if err != nil {
		_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.lease.failed", "project/"+p.Slug+"/engine/"+chi.URLParam(r, "eid"),
			map[string]any{"reason": err.Error()})
		writeErr(w, apperr.New(502, "ENGINE_UNREACHABLE", err.Error()))
		return
	}
	username := "taboo_" + randomHex(6)
	password := randomHex(16)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := engine.Grant(ctx, username, password, ttl); err != nil {
		_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.lease.failed", "project/"+p.Slug+"/engine/"+chi.URLParam(r, "eid"),
			map[string]any{"reason": err.Error()})
		writeErr(w, apperr.New(502, "GRANT_FAILED", err.Error()))
		return
	}
	pwEnc, _ := tc.Encrypt(s.MasterKey, password)
	lid := tc.NewID()
	expires := time.Now().Add(ttl).Unix()
	if _, err := s.DB.Exec(`INSERT INTO dynamic_leases (id, engine_id, identity_id, username, password_encrypted, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, lid, chi.URLParam(r, "eid"), identityID, username, pwEnc, expires); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.lease.create", "project/"+p.Slug+"/lease/"+lid,
		map[string]any{"username": username, "ttl": ttl.Seconds()})
	// 明文密码仅此一次下发
	writeJSON(w, 201, map[string]any{
		"id": lid, "username": username, "password": password,
		"database": database, "host_hint": "(见引擎配置)", "expires_at": expires,
		"seconds_left": int(ttl.Seconds()),
	})
}

func identityHasProjectScope(db *sql.DB, identityID, projectID string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM identity_scopes s
		JOIN environments e ON e.id = s.env_id
		WHERE s.identity_id = ? AND e.project_id = ?`, identityID, projectID).Scan(&n)
	return n > 0
}

// leaseGuard 公共关卡：lease 存在且属于本项目；返回行数据 + 是否管理权（owner/admin 或 lease 所属身份）
func (s *Service) leaseGuard(w http.ResponseWriter, r *http.Request, p project.Ctx) (map[string]any, bool) {
	u := auth.From(r)
	var lid, eid, identityID, username, status string
	var exp int64
	err := s.DB.QueryRow(`SELECT l.id, l.engine_id, l.identity_id, l.username, l.status, l.expires_at
		FROM dynamic_leases l JOIN dynamic_engines e ON e.id = l.engine_id
		WHERE l.id = ? AND e.project_id = ?`, chi.URLParam(r, "lid"), p.ID).
		Scan(&lid, &eid, &identityID, &username, &status, &exp)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return nil, false
	}
	admin := false
	if u.Kind == auth.KindUser {
		if role, ok := auth.Membership(s.DB, p.OrgID, u.ID); ok && (role == "owner" || role == "admin") {
			admin = true
		}
	} else if u.Kind == "identity" && u.ID == identityID {
		admin = true
	}
	if !admin {
		writeErr(w, apperr.Forbidden)
		return nil, false
	}
	return map[string]any{"id": lid, "engine_id": eid, "identity_id": identityID,
		"username": username, "status": status, "expires_at": exp}, true
}

// RenewLease POST /dynamic-leases/{lid}/renew {ttl?} —— 延长 VALID UNTIL（不超 max_ttl）
func (s *Service) RenewLease(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	u := auth.From(r)
	lease, ok := s.leaseGuard(w, r, p)
	if !ok {
		return
	}
	if lease["status"] != "active" || lease["expires_at"].(int64) < time.Now().Unix() {
		writeErr(w, apperr.New(409, "LEASE_INACTIVE", "lease is not active"))
		return
	}
	var connEnc, database string
	var defTTL, maxTTL int
	if err := s.DB.QueryRow(`SELECT conn_encrypted, database, default_ttl, max_ttl FROM dynamic_engines WHERE id = ?`,
		lease["engine_id"]).Scan(&connEnc, &database, &defTTL, &maxTTL); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var b struct {
		TTLSecs int `json:"ttl"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	ttl := time.Duration(orDefaultInt(b.TTLSecs, defTTL)) * time.Second
	newExp := time.Now().Add(ttl)
	// 续期上限：自当前起不超 max_ttl
	if newExp.After(time.Now().Add(time.Duration(maxTTL) * time.Second)) {
		writeErr(w, apperr.New(400, "INVALID_TTL", "renewal exceeds max_ttl"))
		return
	}
	pwEnc, username := "", lease["username"].(string)
	_ = s.DB.QueryRow(`SELECT password_encrypted FROM dynamic_leases WHERE id = ?`, lease["id"]).Scan(&pwEnc)
	password, err := tc.Decrypt(s.MasterKey, pwEnc)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	engine, err := s.engineOf(lease["engine_id"].(string), connEnc)
	if err != nil {
		writeErr(w, apperr.New(502, "ENGINE_UNREACHABLE", err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := engine.Grant(ctx, username, password, time.Until(newExp)); err != nil {
		writeErr(w, apperr.New(502, "RENEW_FAILED", err.Error()))
		return
	}
	if _, err := s.DB.Exec(`UPDATE dynamic_leases SET expires_at = ? WHERE id = ?`, newExp.Unix(), lease["id"]); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.lease.renew", "project/"+p.Slug+"/lease/"+lease["id"].(string),
		map[string]any{"username": username, "expires_at": newExp.Unix()})
	writeJSON(w, 200, map[string]any{"id": lease["id"], "expires_at": newExp.Unix()})
}

// RevokeLease POST /dynamic-leases/{lid}/revoke —— 手动回收（立即 DROP USER）
func (s *Service) RevokeLease(w http.ResponseWriter, r *http.Request) {
	p := project.Of(r)
	u := auth.From(r)
	lease, ok := s.leaseGuard(w, r, p)
	if !ok {
		return
	}
	if lease["status"] != "active" {
		writeJSON(w, 200, map[string]any{"id": lease["id"], "status": lease["status"]})
		return
	}
	if err := s.revokeNow(r.Context(), lease["engine_id"].(string), lease["id"].(string), lease["username"].(string)); err != nil {
		writeErr(w, apperr.New(502, "REVOKE_FAILED", err.Error()))
		return
	}
	_ = auth.AuditReq(s.DB, r, p.OrgID, u, "dynamic.lease.revoke", "project/"+p.Slug+"/lease/"+lease["id"].(string),
		map[string]any{"username": lease["username"]})
	writeJSON(w, 200, map[string]any{"id": lease["id"], "status": "revoked"})
}

// revokeNow 回收 lease：引擎 DROP USER + 状态落库（幂等）
func (s *Service) revokeNow(ctx context.Context, engineID, leaseID, username string) error {
	var connEnc string
	if err := s.DB.QueryRow(`SELECT conn_encrypted FROM dynamic_engines WHERE id = ?`, engineID).Scan(&connEnc); err != nil {
		return err
	}
	engine, err := s.engineOf(engineID, connEnc)
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := engine.Revoke(cctx, username); err != nil {
		var n int
		_ = s.DB.QueryRow(`SELECT COALESCE(reclaim_fails,0) FROM dynamic_leases WHERE id = ?`, leaseID).Scan(&n)
		n++
		status := "active"
		if n >= 5 {
			status = "failed"
			var orgID string
			_ = s.DB.QueryRow(`SELECT org_id FROM dynamic_engines WHERE id = ?`, engineID).Scan(&orgID)
			_ = auth.Audit(s.DB, orgID, &auth.Actor{ID: "system", Name: "system", Kind: auth.KindUser},
				"dynamic.lease.reclaim_failed", "lease/"+leaseID, map[string]any{"username": username, "fails": n}, "")
		}
		_, _ = s.DB.Exec(`UPDATE dynamic_leases SET reclaim_fails = ?, status = CASE WHEN ? = 'failed' THEN 'failed' ELSE status END WHERE id = ?`, n, status, leaseID)
		return err
	}
	_, err = s.DB.Exec(`UPDATE dynamic_leases SET status = 'revoked', revoked_at = datetime('now') WHERE id = ?`, leaseID)
	return err
}

// ---------- 后台 worker ----------

// StartWorker 到期回收 + 身份吊销联动；ctx 取消即停
func (s *Service) StartWorker(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *Service) sweep(ctx context.Context) {
	now := time.Now().Unix()
	// 1. 到期 lease 回收
	rows, err := s.DB.Query(`SELECT l.id, l.engine_id, l.username, e.org_id
		FROM dynamic_leases l JOIN dynamic_engines e ON e.id = l.engine_id
		WHERE l.status = 'active' AND l.expires_at <= ?`, now)
	if err == nil {
		var ids, eids, users, orgs []string
		for rows.Next() {
			var id, eid, user, org string
			_ = rows.Scan(&id, &eid, &user, &org)
			ids, eids, users, orgs = append(ids, id), append(eids, eid), append(users, user), append(orgs, org)
		}
		rows.Close()
		for i := range ids {
			if err := s.revokeNow(ctx, eids[i], ids[i], users[i]); err == nil {
				auth.Audit(s.DB, orgs[i], &auth.Actor{ID: "system", Name: "system", Kind: auth.KindUser},
					"dynamic.lease.expire", "lease/"+ids[i], map[string]any{"username": users[i]}, "")
			}
		}
	}
	// 2. 身份吊销联动：活跃 lease 的 identity 已 revoked → 回收
	rows2, err := s.DB.Query(`SELECT l.id, l.engine_id, l.username, e.org_id
		FROM dynamic_leases l
		JOIN dynamic_engines e ON e.id = l.engine_id
		JOIN machine_identities mi ON mi.id = l.identity_id
		WHERE l.status = 'active' AND mi.status = 'revoked'`)
	if err == nil {
		var ids, eids, users, orgs []string
		for rows2.Next() {
			var id, eid, user, org string
			_ = rows2.Scan(&id, &eid, &user, &org)
			ids, eids, users, orgs = append(ids, id), append(eids, eid), append(users, user), append(orgs, org)
		}
		rows2.Close()
		for i := range ids {
			if err := s.revokeNow(ctx, eids[i], ids[i], users[i]); err == nil {
				auth.Audit(s.DB, orgs[i], &auth.Actor{ID: "system", Name: "system", Kind: auth.KindUser},
					"dynamic.lease.revoke", "lease/"+ids[i], map[string]any{"username": users[i], "reason": "identity_revoked"}, "")
			}
		}
	}
}
