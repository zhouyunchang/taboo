// 认证中间件 + RBAC 求值 + 审计 —— 对齐设计文档 §6 / §8
package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
)

type ctxKey string

const UserKey ctxKey = "taboo.user"

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

var roleRank = map[string]int{"viewer": 1, "developer": 2, "admin": 3, "owner": 4}

func Membership(db *sql.DB, orgID, userID string) (string, bool) {
	var role string
	err := db.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&role)
	return role, err == nil
}

// Can 策略求值（四元组 MVP 化）：action ∈ read | reveal | write | manage
// reveal（取明文）与 read（看元数据）分离；developer 禁写/禁看 prod 明文。
func Can(db *sql.DB, userID, orgID, action, envSlug string) bool {
	role, ok := Membership(db, orgID, userID)
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

// Audit append-only 写入（应用层强制只 INSERT）
func Audit(db *sql.DB, orgID string, actor *User, action, resource string, metadata map[string]any, ip string) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	mb, _ := json.Marshal(metadata)
	actorID, actorName := "system", ""
	if actor != nil {
		actorID, actorName = actor.ID, actor.Name
	}
	_, _ = db.Exec(`INSERT INTO audit_logs (id, org_id, actor_id, actor_name, action, resource, metadata, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.NewID(), orgID, actorID, actorName, action, resource, string(mb), ip)
}

type TokenPair struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

func IssueTokens(db *sql.DB, secret string, u *User) (*TokenPair, error) {
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

// Middleware 校验 Bearer JWT，注入 User 到 context
func Middleware(db *sql.DB, jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearer(r)
			if token == "" {
				writeErr(w, apperr.Unauthorized)
				return
			}
			sub, err := tc.VerifyJWT(token, jwtSecret)
			if err != nil {
				writeErr(w, apperr.Unauthorized)
				return
			}
			var u User
			var status string
			err = db.QueryRow(`SELECT id, email, name, status FROM users WHERE id = ?`, sub).
				Scan(&u.ID, &u.Email, &u.Name, &status)
			if err != nil || status != "active" {
				writeErr(w, apperr.Unauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserKey, &u)))
		})
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

func From(r *http.Request) *User {
	u, _ := r.Context().Value(UserKey).(*User)
	return u
}

func writeErr(w http.ResponseWriter, e *apperr.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
