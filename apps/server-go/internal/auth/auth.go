// 认证中间件 + RBAC 求值（用户角色 / 机器身份 scope 双主体）+ 审计 —— 设计文档 §6 / §8
//
// 策略四元组 (主体, 资源, 动作, 环境约束)：
//   用户：org_members 角色 viewer/developer/admin/owner（reveal 与 read 分离）
//   机器身份：identity_scopes 显式 (项目, 环境, read|write)，禁止通配（§6）
package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type ctxKey string

const UserKey ctxKey = "taboo.user"

// Actor 统一主体：用户或机器身份
type Actor struct {
	ID    string `json:"id"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name"`
	Kind  string `json:"kind"` // "user" | "identity"
}

const (
	KindUser     = "user"
	KindIdentity = "identity"
)

var roleRank = map[string]int{"viewer": 1, "developer": 2, "admin": 3, "owner": 4}

func Membership(db *sql.DB, orgID, userID string) (string, bool) {
	var role string
	err := db.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&role)
	return role, err == nil
}

// Can 策略求值：action ∈ read | reveal | write | manage
//  - 用户：角色决定（reveal 与 read 分离；developer 禁 prod 明文/写入）
//  - 机器身份：identity_scopes 命中 (projectID, envSlug)；read/reveal 需 read|write，write 需 write
func Can(db *sql.DB, actor *Actor, orgID, projectID, action, envSlug string) bool {
	if actor == nil {
		return false
	}
	if actor.Kind == KindIdentity {
		return identityCan(db, actor.ID, orgID, projectID, action, envSlug)
	}
	role, ok := Membership(db, orgID, actor.ID)
	if !ok {
		return false
	}
	rank := roleRank[role]
	switch action {
	case "read":
		return rank >= roleRank["viewer"]
	case "reveal", "write":
		if rank >= roleRank["admin"] {
			return true
		}
		return role == "developer" && envSlug != "prod"
	case "manage":
		return rank >= roleRank["owner"]
	}
	return false
}

func identityCan(db *sql.DB, identityID, orgID, projectID, action, envSlug string) bool {
	// identity 必须属于该组织且未吊销
	var status string
	err := db.QueryRow(`SELECT status FROM machine_identities WHERE id = ? AND org_id = ?`, identityID, orgID).Scan(&status)
	if err != nil || status != "active" {
		return false
	}
	switch action {
	case "read", "reveal":
		// envSlug 为空 = 项目级读（如环境列表）：项目内任一 scope 即可
		envCond, args := `AND e.slug = ?`, []any{identityID, projectID, envSlug}
		if envSlug == "" {
			envCond, args = ``, []any{identityID, projectID}
		}
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM identity_scopes s
			JOIN environments e ON e.id = s.env_id
			WHERE s.identity_id = ? AND s.project_id = ? `+envCond+` AND s.permission IN ('read','write')`,
			args...).Scan(&n)
		return n > 0
	case "write":
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM identity_scopes s
			JOIN environments e ON e.id = s.env_id
			WHERE s.identity_id = ? AND s.project_id = ? AND e.slug = ? AND s.permission = 'write'`,
			identityID, projectID, envSlug).Scan(&n)
		return n > 0
	}
	return false
}

// Audit append-only 写入（actor_type 区分 user / identity）
func Audit(db *sql.DB, orgID string, actor *Actor, action, resource string, metadata map[string]any, ip string) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	mb, _ := json.Marshal(metadata)
	actorID, actorName, actorType := "system", "", KindUser
	if actor != nil {
		actorID, actorName, actorType = actor.ID, actor.Name, actor.Kind
		if actor.Kind == KindIdentity {
			actorName = "identity:" + actor.Name // 审计主体标识（§7.3）
		}
	}
	_, _ = db.Exec(`INSERT INTO audit_logs (id, org_id, actor_id, actor_name, actor_type, action, resource, metadata, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.NewID(), orgID, actorID, actorName, actorType, action, resource, string(mb), ip)
}

type TokenPair struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh,omitempty"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

func IssueTokens(db *sql.DB, secret string, u *Actor) (*TokenPair, error) {
	access, err := tc.SignJWT(u.ID, u.Email, secret, 15*time.Minute)
	if err != nil {
		return nil, err
	}
	refresh := tc.NewID() + tc.NewID()
	_, err = db.Exec(`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		tc.NewID(), u.ID, tc.SHA256(refresh), time.Now().Add(30*24*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	return &TokenPair{Access: access, Refresh: refresh, TokenType: "Bearer", ExpiresIn: 900}, nil
}

// Middleware 校验 Bearer JWT（user / identity 双主体），注入 Actor 到 context
func Middleware(db *sql.DB, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearer(r)
			if token == "" {
				writeErr(w, apperr.Unauthorized)
				return
			}
			claims, err := tc.VerifyJWT(token, jwtSecret)
			if err != nil {
				writeErr(w, apperr.Unauthorized)
				return
			}
			sub, _ := claims["sub"].(string)
			typ, _ := claims["typ"].(string)
			var actor *Actor
			if typ == KindIdentity {
				actor, err = loadIdentity(db, sub)
			} else {
				actor, err = loadUser(db, sub)
			}
			if err != nil {
				writeErr(w, apperr.Unauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserKey, actor)))
		})
	}
}

func loadUser(db *sql.DB, id string) (*Actor, error) {
	var a Actor
	var status string
	err := db.QueryRow(`SELECT id, email, name, status FROM users WHERE id = ?`, id).
		Scan(&a.ID, &a.Email, &a.Name, &status)
	if err != nil || status != "active" {
		return nil, apperr.Unauthorized
	}
	a.Kind = KindUser
	return &a, nil
}

func loadIdentity(db *sql.DB, id string) (*Actor, error) {
	var a Actor
	var status string
	err := db.QueryRow(`SELECT id, name, status FROM machine_identities WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &status)
	if err != nil || status != "active" {
		return nil, apperr.Unauthorized // 吊销即 401，立即生效
	}
	a.Kind = KindIdentity
	return &a, nil
}

// CheckIdentitySecret 常时比较 secret（client_credentials 换 token 用）
func CheckIdentitySecret(got, wantHash string) bool {
	gotHash := tc.SHA256(got)
	return subtle.ConstantTimeCompare([]byte(gotHash), []byte(wantHash)) == 1
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

func From(r *http.Request) *Actor {
	a, _ := r.Context().Value(UserKey).(*Actor)
	return a
}

func writeErr(w http.ResponseWriter, e *apperr.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
