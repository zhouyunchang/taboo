// 认证中间件 + RBAC 求值（用户角色 / 机器身份 scope 双主体）+ 审计
package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

type ctxKey string

const UserKey ctxKey = "taboo.user"

type Actor struct {
	WorkloadBinding      string   `json:"-"`
	OAuthScopes          []string `json:"-"`
	ID                   string   `json:"id"`
	Email                string   `json:"email,omitempty"`
	Name                 string   `json:"name"`
	Kind                 string   `json:"kind"` // "user" | "identity"
	OrganizationID       string   `json:"-"`
	AuthenticationSource string   `json:"-"`
	BrowserOrigin        string   `json:"-"`
	SessionHash          string   `json:"-"`
	SessionProfile       string   `json:"-"`
	SensitiveAllowed     bool     `json:"-"`
	SessionExpires       int64    `json:"-"`
	ReauthenticationURL  string   `json:"-"`
	AuthValidUntil       int64    `json:"-"`
	RPLogoutEnabled      bool     `json:"-"`
}

const (
	KindUser     = "user"
	KindIdentity = "identity"
)

func Membership(db *sql.DB, orgID, userID string) (string, bool) {
	role, _, ok := MemberInfo(db, orgID, userID)
	return role, ok
}

func MemberInfo(db *sql.DB, orgID, userID string) (role string, restricted bool, ok bool) {
	var rest int
	err := db.QueryRow(`SELECT role, COALESCE(restricted, 0) FROM org_members WHERE org_id = ? AND user_id = ?`,
		orgID, userID).Scan(&role, &rest)
	return role, rest == 1, err == nil
}

func EnvProtected(db *sql.DB, projectID, envSlug string) bool {
	if envSlug == "" {
		return false
	}
	var p int
	err := db.QueryRow(`SELECT COALESCE(protected, 0) FROM environments WHERE project_id = ? AND slug = ?`,
		projectID, envSlug).Scan(&p)
	if err != nil {
		// 未找到环境时按 slug==prod 兜底，避免新建环境漏标时误开
		return envSlug == "prod"
	}
	return p == 1
}

func projectGrant(db *sql.DB, projectID, userID string) (role string, ok bool) {
	if projectID == "" {
		return "", false
	}
	err := db.QueryRow(`SELECT role FROM project_grants WHERE project_id = ? AND user_id = ?`,
		projectID, userID).Scan(&role)
	return role, err == nil
}

// Can 策略求值：action ∈ read | reveal | write | admin | members | audit.* | manage
func Can(db *sql.DB, actor *Actor, orgID, projectID, action, envSlug string) bool {
	if actor == nil {
		return false
	}
	if actor.OrganizationID != "" && actor.OrganizationID != orgID {
		return false
	}
	if actor.Kind == KindIdentity {
		if actor.WorkloadBinding != "" {
			return workloadCan(db, actor, orgID, projectID, action, envSlug)
		}
		return identityCan(db, actor.ID, orgID, projectID, action, envSlug)
	}
	if actor.BrowserOrigin != "" && rbac.Sensitive(action) && !actor.SensitiveAllowed {
		return false
	}
	orgRole, restricted, ok := MemberInfo(db, orgID, actor.ID)
	if !ok {
		return false
	}
	if jitAllows(db, actor, orgID, projectID, action, envSlug) {
		return true
	}
	grantRole, hasGrant := projectGrant(db, projectID, actor.ID)
	eff, ok := rbac.EffectiveRole(orgRole, restricted, grantRole, hasGrant)
	if !ok {
		return false
	}
	if actor.AuthenticationSource != "" && projectID != "" {
		switch action {
		case rbac.Read, rbac.Write, rbac.Reveal, rbac.Export, rbac.Rollback, rbac.Delete:
			// Explicit grants are the authority for secret actions of federated
			// people. A mapped developer/admin role cannot imply plaintext access.
			var n int
			query := `SELECT COUNT(*) FROM member_action_grants g JOIN environments e ON e.id=g.env_id AND e.project_id=g.project_id WHERE g.org_id=? AND g.user_id=? AND g.project_id=? AND g.action=?`
			args := []any{orgID, actor.ID, projectID, action}
			if envSlug != "" {
				query += ` AND e.slug=?`
				args = append(args, envSlug)
			}
			err := db.QueryRow(query, args...).Scan(&n)
			return err == nil && n > 0 && rbac.UserAllows(eff, rbac.Read, false)
		}
	}
	return rbac.UserAllows(eff, action, EnvProtected(db, projectID, envSlug))
}

func identityCan(db *sql.DB, identityID, orgID, projectID, action, envSlug string) bool {
	var status string
	err := db.QueryRow(`SELECT status FROM machine_identities WHERE id = ? AND org_id = ?`, identityID, orgID).Scan(&status)
	if err != nil || status != "active" {
		return false
	}
	envCond, args := `AND e.slug = ?`, []any{identityID, projectID, envSlug}
	if envSlug == "" {
		envCond, args = ``, []any{identityID, projectID}
	}
	rows, err := db.Query(`SELECT s.permission FROM identity_scopes s
		JOIN environments e ON e.id = s.env_id
		WHERE s.identity_id = ? AND s.project_id = ? `+envCond, args...)
	if err != nil {
		return false
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			perms = append(perms, p)
		}
	}
	return rbac.IdentityAllows(action, perms)
}

func recOf(orgID string, actor *Actor, action, resource string, metadata map[string]any, ip, ua, rid string) audit.Rec {
	if metadata == nil {
		metadata = map[string]any{}
	}
	actorID, actorName, actorType := "system", "", KindUser
	if actor != nil {
		actorID, actorName, actorType = actor.ID, actor.Name, actor.Kind
		if actor.AuthenticationSource != "" {
			metadata["authentication_source"] = actor.AuthenticationSource
		}
		if actor.Kind == KindIdentity {
			actorName = "identity:" + actor.Name
		}
	}
	return audit.Rec{
		OrgID: orgID, ActorID: actorID, ActorName: actorName, ActorType: actorType,
		Action: action, Resource: resource, Metadata: metadata,
		IP: ip, UA: ua, RequestID: rid,
	}
}

// Audit 写入审计；敏感动作 fail-closed（返回 error）。
func Audit(db audit.DBTX, orgID string, actor *Actor, action, resource string, metadata map[string]any, ip string) error {
	rec := recOf(orgID, actor, action, resource, metadata, ip, "", "")
	rec.FailOpen = !failClosed(action)
	return audit.Record(db, rec)
}

func AuditReq(db audit.DBTX, r *http.Request, orgID string, actor *Actor, action, resource string, metadata map[string]any) error {
	rec := recOf(orgID, actor, action, resource, metadata, httpx.ClientIP(r), httpx.UserAgent(r), httpx.RequestID(r))
	rec.FailOpen = !failClosed(action)
	return audit.Record(db, rec)
}

func failClosed(action string) bool {
	if rbac.Sensitive(action) {
		return true
	}
	switch {
	case strings.HasPrefix(action, "secrets.") && action != "secrets.list":
		return true
	case strings.HasPrefix(action, "identity.") && action != "identity.list":
		return true
	case strings.HasPrefix(action, "scan_report."):
		return true
	case strings.HasPrefix(action, "admin."):
		return true
	case strings.HasPrefix(action, "auth.totp."), strings.HasPrefix(action, "members."), strings.HasPrefix(action, "oidc."), strings.HasPrefix(action, "workload."):
		return true
	case strings.HasPrefix(action, "approval."), strings.HasPrefix(action, "access."), strings.HasPrefix(action, "scim."), strings.HasPrefix(action, "auth.device"), strings.HasPrefix(action, "auth.cli"):
		return true
	case action == "auth.session.refresh", action == "auth.logout", action == "auth.oidc.login", action == "auth.login", action == "auth.register", action == "auth.backchannel_logout", action == "project.create", action == "env.create", action == "env.approval":
		return true
	}
	return false
}

// Require 权限关卡：失败写 authz.denied 并 403。
func Require(db *sql.DB, w http.ResponseWriter, r *http.Request, orgID, projectID, action, envSlug string) bool {
	u := From(r)
	if Can(db, u, orgID, projectID, action, envSlug) {
		return true
	}
	Deny(db, w, r, orgID, action, resourceOf(projectID, envSlug, action))
	return false
}

func Deny(db *sql.DB, w http.ResponseWriter, r *http.Request, orgID, action, resource string) {
	_ = AuditReq(db, r, orgID, From(r), "authz.denied", resource, map[string]any{"action": action})
	writeErr(w, apperr.Forbidden)
}

func resourceOf(projectID, envSlug, action string) string {
	if projectID != "" && envSlug != "" {
		return "project/" + projectID + "/env/" + envSlug
	}
	if projectID != "" {
		return "project/" + projectID
	}
	return "action/" + action
}

type TokenPair struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh,omitempty"`
	TokenType string `json:"tokenType"`
	ExpiresIn int    `json:"expiresIn"`
}

func IssueTokens(db audit.DBTX, secret string, u *Actor) (*TokenPair, error) {
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

type ExternalAuthenticator func(*http.Request) (*Actor, error)

func Middleware(db *sql.DB, jwtSecret string, policies ...BrowserPolicy) func(http.Handler) http.Handler {
	return MiddlewareWithExternal(db, jwtSecret, nil, policies...)
}
func MiddlewareWithExternal(db *sql.DB, jwtSecret string, external ExternalAuthenticator, policies ...BrowserPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Taboo-Trust") != "" {
				if external == nil {
					writeErr(w, apperr.Unauthorized)
					return
				}
				actor, err := external(r)
				if err != nil {
					if e, ok := err.(*apperr.Error); ok {
						writeErr(w, e)
					} else {
						writeErr(w, apperr.Unauthorized)
					}
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserKey, actor)))
				return
			}
			if _, err := r.Cookie(BrowserCookie); err == nil && r.Header.Get("Authorization") == "" {
				a, err := BrowserActor(db, r)
				if err != nil {
					if e, ok := err.(*apperr.Error); ok {
						writeErr(w, e)
					} else {
						writeErr(w, apperr.Unauthorized)
					}
					return
				}
				for _, allowed := range policies {
					if err := allowed(r, a); err != nil {
						if e, ok := err.(*apperr.Error); ok {
							writeErr(w, e)
						} else {
							writeErr(w, apperr.Unauthorized)
						}
						return
					}
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserKey, a)))
				return
			}
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
				if err == nil && !IdentityIPAllowed(db, actor.ID, httpx.ClientIP(r)) {
					writeErr(w, apperr.New(403, "IP_NOT_ALLOWED", "client address is not allowed for this machine identity"))
					return
				}
			} else if typ == KindUser {
				actor, err = loadUser(db, sub)
				var source string
				if err == nil {
					err = db.QueryRow(`SELECT auth_source FROM users WHERE id=?`, sub).Scan(&source)
					if source != "local" {
						err = apperr.Unauthorized
					}
				}
			} else {
				writeErr(w, apperr.Unauthorized)
				return
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
	err := db.QueryRow(`SELECT id, name, status FROM machine_identities WHERE id = ? AND auth_type='client_credentials'`, id).
		Scan(&a.ID, &a.Name, &status)
	if err != nil || status != "active" {
		return nil, apperr.Unauthorized
	}
	a.Kind = KindIdentity
	return &a, nil
}

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

func workloadCan(db *sql.DB, a *Actor, org, project, action, env string) bool {
	query := `SELECT g.required_scope FROM workload_grants g JOIN workload_bindings b ON b.id=g.binding_id AND b.org_id=g.org_id JOIN oauth_trusts t ON t.id=b.trust_id AND t.org_id=b.org_id JOIN machine_identities m ON m.id=b.id AND m.org_id=b.org_id JOIN environments e ON e.id=g.env_id AND e.project_id=g.project_id WHERE b.id=? AND b.org_id=? AND b.enabled=1 AND t.enabled=1 AND m.status='active' AND m.auth_type='external_oauth' AND g.project_id=? AND g.action=?`
	args := []any{a.WorkloadBinding, org, project, rbac.Normalize(action)}
	if env != "" {
		query += ` AND e.slug=?`
		args = append(args, env)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var scope string
		if rows.Scan(&scope) != nil {
			return false
		}
		for _, granted := range a.OAuthScopes {
			if scope == granted {
				return true
			}
		}
	}
	return false
}
