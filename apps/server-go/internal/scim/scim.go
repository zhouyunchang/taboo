// SCIM 2.0 provisioning for one organization. Tokens are bearer secrets shown
// once. Users are never joined to an existing account by email.
package scim

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

const (
	coreUser  = "urn:ietf:params:scim:schemas:core:2.0:User"
	coreGroup = "urn:ietf:params:scim:schemas:core:2.0:Group"
	extGroup  = "urn:ietf:params:scim:schemas:extension:taboo:2.0:Group"
	listResp  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	errSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	patchOp   = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

var filterEq = regexp.MustCompile(`^(userName|externalId|displayName) eq "((?:[^"\\]|\\.){0,200})"$`)

type Service struct {
	DB        *sql.DB
	MasterKey []byte
}

func (s *Service) Routes(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/scim+json")
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/ServiceProviderConfig", s.config)
	r.Get("/ResourceTypes", s.resourceTypes)
	r.Get("/Schemas", s.schemas)
	r.Get("/Users", s.auth(s.listUsers))
	r.Post("/Users", s.auth(s.createUser))
	r.Get("/Users/{id}", s.auth(s.getUser))
	r.Put("/Users/{id}", s.auth(s.putUser))
	r.Patch("/Users/{id}", s.auth(s.patchUser))
	r.Delete("/Users/{id}", s.auth(s.deleteUser))
	r.Get("/Groups", s.auth(s.listGroups))
	r.Post("/Groups", s.auth(s.createGroup))
	r.Get("/Groups/{id}", s.auth(s.getGroup))
	r.Put("/Groups/{id}", s.auth(s.putGroup))
	r.Patch("/Groups/{id}", s.auth(s.patchGroup))
	r.Delete("/Groups/{id}", s.auth(s.deleteGroup))
}

func (s *Service) auth(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r)
		// Shared counters: a per-address request ceiling, and a much lower
		// ceiling once an address has presented bad tokens.
		if ok, err := s.underLimit("scim-fail:"+ip, failLimit); err != nil {
			scimErr(w, 503, "try again")
			return
		} else if !ok {
			scimErr(w, 429, "too many failed attempts")
			return
		}
		if ok, err := auth.Hit(s.DB, "scim:"+ip, time.Minute, requestLimit); err != nil {
			scimErr(w, 503, "try again")
			return
		} else if !ok {
			scimErr(w, 429, "too many requests")
			return
		}
		raw := bearer(r)
		if !strings.HasPrefix(raw, "taboo_scim_") || len(raw) > 200 {
			s.fail(ip)
			scimErr(w, 401, "invalid token")
			return
		}
		var org string
		var exp, revoked int64
		err := s.DB.QueryRow(`SELECT org_id, expires_at, revoked_at FROM scim_tokens WHERE token_hash=?`, tc.SHA256(raw)).Scan(&org, &exp, &revoked)
		if err != nil && err != sql.ErrNoRows {
			scimErr(w, 503, "try again")
			return
		}
		if err != nil || revoked != 0 || (exp > 0 && exp <= time.Now().Unix()) {
			s.fail(ip)
			scimErr(w, 401, "invalid token")
			return
		}
		if personal, err := orgsvc.IsPersonal(s.DB, org); err != nil {
			scimErr(w, 503, "try again")
			return
		} else if personal {
			scimErr(w, 403, "personal spaces cannot be shared or federated")
			return
		}
		_, _ = s.DB.Exec(`UPDATE scim_tokens SET last_used_at=? WHERE token_hash=?`, time.Now().Unix(), tc.SHA256(raw))
		next(w, r, org)
	}
}

const (
	requestLimit = 600 // SCIM requests per address per minute
	failLimit    = 20  // bad tokens per address per minute
)

func (s *Service) fail(ip string) {
	_, _ = auth.Hit(s.DB, "scim-fail:"+ip, time.Minute, failLimit)
}

// underLimit reads the failure counter without adding to it.
func (s *Service) underLimit(bucket string, limit int) (bool, error) {
	start := time.Now().Unix() / 60 * 60
	var hits int
	err := s.DB.QueryRow(`SELECT hits FROM rate_counters WHERE bucket=? AND window_start=?`, bucket, start).Scan(&hits)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return hits < limit, nil
}

func (s *Service) config(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, map[string]any{
		"schemas":               []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":                 map[string]any{"supported": true},
		"bulk":                  map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":                map[string]any{"supported": true, "maxResults": 100},
		"changePassword":        map[string]any{"supported": false},
		"sort":                  map[string]any{"supported": false},
		"etag":                  map[string]any{"supported": false},
		"authenticationSchemes": []any{map[string]any{"type": "oauthbearertoken", "name": "Bearer", "description": "Organization SCIM token"}},
	})
}

func (s *Service) resourceTypes(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, []any{
		map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User", "endpoint": "/Users", "schema": coreUser},
		map[string]any{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group", "endpoint": "/Groups", "schema": coreGroup},
	})
}

func (s *Service) schemas(w http.ResponseWriter, _ *http.Request) {
	write(w, 200, []any{
		map[string]any{"id": coreUser, "name": "User"},
		map[string]any{"id": coreGroup, "name": "Group"},
	})
}

type scimUser struct {
	UserName   string `json:"userName"`
	ExternalID string `json:"externalId"`
	Active     *bool  `json:"active"`
	Name       struct {
		Formatted string `json:"formatted"`
	} `json:"name"`
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request, org string) {
	var in scimUser
	if !decode(w, r, &in) || strings.TrimSpace(in.UserName) == "" || len(in.UserName) > 200 {
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	id := tc.NewID()
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	if !s.insertUser(tx, w, r, org, id, in, active) {
		return
	}
	if tx.Commit() != nil {
		scimErr(w, 503, "try again")
		return
	}
	write(w, 201, s.userBody(org, id))
}

func (s *Service) insertUser(tx *sql.Tx, w http.ResponseWriter, r *http.Request, org, id string, in scimUser, active bool) bool {
	name := strings.TrimSpace(in.Name.Formatted)
	if name == "" {
		name = in.UserName
	}
	if len(name) > 200 || len(in.ExternalID) > 200 {
		scimErr(w, 400, "value is too long")
		return false
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM scim_users WHERE org_id=? AND user_name=?`, org, in.UserName).Scan(&n); err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	if n > 0 {
		scimErr(w, 409, "userName already exists")
		return false
	}
	if in.ExternalID != "" {
		if err := tx.QueryRow(`SELECT COUNT(*) FROM scim_users WHERE org_id=? AND external_id=?`, org, in.ExternalID).Scan(&n); err != nil || n > 0 {
			scimErr(w, 409, "externalId already exists")
			return false
		}
	}
	userID := tc.NewID()
	email := scimEmail(org, userID)
	if _, err := tx.Exec(`INSERT INTO users(id,email,name,password_hash,auth_source,external_sub,status) VALUES(?,?,?,'!','scim',?,?)`,
		userID, email, name, org+":"+id, map[bool]string{true: "active", false: "disabled"}[active]); err != nil {
		slog.Error("scim user insert", "err", err)
		scimErr(w, 503, "try again")
		return false
	}
	flag := 0
	if active {
		flag = 1
	}
	if _, err := tx.Exec(`INSERT INTO scim_users(id,org_id,user_id,external_id,user_name,active) VALUES(?,?,?,?,?,?)`, id, org, userID, in.ExternalID, in.UserName, flag); err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	if active {
		if _, err := tx.Exec(`INSERT INTO org_members(org_id,user_id,role,scim_managed) VALUES(?,?,'viewer',1)`, org, userID); err != nil {
			scimErr(w, 503, "try again")
			return false
		}
	}
	if err := orgsvc.EnsurePersonalSpace(tx, s.MasterKey, userID, email, name); err != nil {
		slog.Error("scim personal space", "err", err)
		scimErr(w, 503, "try again")
		return false
	}
	if !auditSCIM(tx, r, org, "scim.user.write", "scim-user/"+id) {
		scimErr(w, 503, "try again")
		return false
	}
	return true
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request, org string) {
	attr, value, ok := eqFilter(w, r, "user")
	if !ok {
		return
	}
	start, count := page(r)
	q := `SELECT id FROM scim_users WHERE org_id=?`
	args := []any{org}
	switch attr {
	case "userName":
		q += ` AND user_name=?`
		args = append(args, value)
	case "externalId":
		q += ` AND external_id=?`
		args = append(args, value)
	}
	var total int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM (`+q+`) t`, args...).Scan(&total); err != nil {
		scimErr(w, 503, "try again")
		return
	}
	rows, err := s.DB.Query(q+` ORDER BY user_name LIMIT ? OFFSET ?`, append(args, count, start-1)...)
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			if body := s.userBody(org, id); body != nil {
				out = append(out, body)
			}
		}
	}
	write(w, 200, map[string]any{"schemas": []string{listResp}, "totalResults": total, "startIndex": start, "itemsPerPage": len(out), "Resources": out})
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request, org string) {
	body := s.userBody(org, chi.URLParam(r, "id"))
	if body == nil {
		scimErr(w, 404, "user not found")
		return
	}
	write(w, 200, body)
}

func (s *Service) putUser(w http.ResponseWriter, r *http.Request, org string) {
	var in scimUser
	if !decode(w, r, &in) || strings.TrimSpace(in.UserName) == "" {
		return
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	if !s.updateUser(w, r, org, chi.URLParam(r, "id"), in, active) {
		return
	}
	write(w, 200, s.userBody(org, chi.URLParam(r, "id")))
}

func (s *Service) patchUser(w http.ResponseWriter, r *http.Request, org string) {
	id := chi.URLParam(r, "id")
	cur := s.loadUser(org, id)
	if cur == nil {
		scimErr(w, 404, "user not found")
		return
	}
	in := scimUser{UserName: cur.userName, ExternalID: cur.externalID}
	in.Name.Formatted = cur.name
	active := cur.active
	if !applyPatch(w, r, func(path string, value json.RawMessage) bool {
		switch strings.ToLower(path) {
		case "username":
			var v string
			if json.Unmarshal(value, &v) != nil {
				return false
			}
			in.UserName = v
		case "externalid":
			var v string
			if json.Unmarshal(value, &v) != nil {
				return false
			}
			in.ExternalID = v
		case "active":
			var v bool
			if json.Unmarshal(value, &v) != nil {
				return false
			}
			active = v
		case "name.formatted":
			var v string
			if json.Unmarshal(value, &v) != nil {
				return false
			}
			in.Name.Formatted = v
		default:
			return false
		}
		return true
	}) {
		return
	}
	if !s.updateUser(w, r, org, id, in, active) {
		return
	}
	write(w, 200, s.userBody(org, id))
}

func (s *Service) updateUser(w http.ResponseWriter, r *http.Request, org, id string, in scimUser, active bool) bool {
	cur := s.loadUser(org, id)
	if cur == nil {
		scimErr(w, 404, "user not found")
		return false
	}
	if len(in.UserName) > 200 || len(in.ExternalID) > 200 {
		scimErr(w, 400, "value is too long")
		return false
	}
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM scim_users WHERE org_id=? AND user_name=? AND id<>?`, org, in.UserName, id).Scan(&n); err != nil || n > 0 {
		scimErr(w, 409, "userName already exists")
		return false
	}
	if in.ExternalID != "" {
		if err = tx.QueryRow(`SELECT COUNT(*) FROM scim_users WHERE org_id=? AND external_id=? AND id<>?`, org, in.ExternalID, id).Scan(&n); err != nil || n > 0 {
			scimErr(w, 409, "externalId already exists")
			return false
		}
	}
	flag := 0
	if active {
		flag = 1
	}
	name := strings.TrimSpace(in.Name.Formatted)
	if name == "" {
		name = in.UserName
	}
	if _, err = tx.Exec(`UPDATE scim_users SET user_name=?, external_id=?, active=?, updated_at=datetime('now') WHERE id=? AND org_id=?`, in.UserName, in.ExternalID, flag, id, org); err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	if _, err = tx.Exec(`UPDATE users SET name=? WHERE id=?`, name, cur.userID); err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	if !s.ensurePersonal(tx, w, cur.userID) {
		return false
	}
	if active {
		if _, err = tx.Exec(`INSERT INTO org_members(org_id,user_id,role,scim_managed) VALUES(?,?,'viewer',1) ON CONFLICT(org_id,user_id) DO NOTHING`, org, cur.userID); err != nil {
			scimErr(w, 503, "try again")
			return false
		}
		if _, err = tx.Exec(`UPDATE users SET status='active' WHERE id=? AND auth_source='scim'`, cur.userID); err != nil {
			scimErr(w, 503, "try again")
			return false
		}
		if !s.syncRole(tx, org, cur.userID) {
			scimErr(w, 503, "try again")
			return false
		}
	} else if !s.deprovision(tx, org, cur.userID) {
		scimErr(w, 409, "cannot deprovision the last owner")
		return false
	}
	if !auditSCIM(tx, r, org, "scim.user.write", "scim-user/"+id) || tx.Commit() != nil {
		scimErr(w, 503, "try again")
		return false
	}
	return true
}

func (s *Service) deleteUser(w http.ResponseWriter, r *http.Request, org string) {
	id := chi.URLParam(r, "id")
	cur := s.loadUser(org, id)
	if cur == nil {
		scimErr(w, 404, "user not found")
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM scim_group_members WHERE org_id=? AND user_scim_id=?`, org, id); err != nil || !s.deprovision(tx, org, cur.userID) {
		scimErr(w, 409, "cannot deprovision the last owner")
		return
	}
	if _, err = tx.Exec(`UPDATE scim_users SET active=0, updated_at=datetime('now') WHERE id=? AND org_id=?`, id, org); err != nil {
		scimErr(w, 503, "try again")
		return
	}
	if !auditSCIM(tx, r, org, "scim.user.delete", "scim-user/"+id) || tx.Commit() != nil {
		scimErr(w, 503, "try again")
		return
	}
	w.WriteHeader(204)
}

type loadedUser struct {
	userID, userName, externalID, name string
	active                             bool
}

func (s *Service) loadUser(org, id string) *loadedUser {
	var u loadedUser
	var active int
	err := s.DB.QueryRow(`SELECT u.user_id, u.user_name, u.external_id, users.name, u.active FROM scim_users u JOIN users ON users.id=u.user_id WHERE u.org_id=? AND u.id=?`, org, id).
		Scan(&u.userID, &u.userName, &u.externalID, &u.name, &active)
	if err != nil {
		return nil
	}
	u.active = active == 1
	return &u
}

func (s *Service) userBody(org, id string) map[string]any {
	u := s.loadUser(org, id)
	if u == nil {
		return nil
	}
	body := map[string]any{
		"schemas": []string{coreUser}, "id": id, "userName": u.userName, "active": u.active,
		"name": map[string]any{"formatted": u.name},
	}
	if u.externalID != "" {
		body["externalId"] = u.externalID
	}
	return body
}

type scimGroup struct {
	DisplayName string `json:"displayName"`
	ExternalID  string `json:"externalId"`
	Members     []struct {
		Value string `json:"value"`
	} `json:"members"`
	Extension map[string]any `json:"-"`
}

func (s *Service) createGroup(w http.ResponseWriter, r *http.Request, org string) {
	in, role, ok := s.readGroup(w, r)
	if !ok {
		return
	}
	id := tc.NewID()
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	if !s.saveGroup(tx, w, r, org, id, in, role, true) || tx.Commit() != nil {
		return
	}
	write(w, 201, s.groupBody(org, id))
}

func (s *Service) listGroups(w http.ResponseWriter, r *http.Request, org string) {
	attr, value, ok := eqFilter(w, r, "group")
	if !ok {
		return
	}
	start, count := page(r)
	q := `SELECT id FROM scim_groups WHERE org_id=?`
	args := []any{org}
	switch attr {
	case "displayName":
		q += ` AND display_name=?`
		args = append(args, value)
	case "externalId":
		q += ` AND external_id=?`
		args = append(args, value)
	}
	var total int
	if s.DB.QueryRow(`SELECT COUNT(*) FROM (`+q+`) t`, args...).Scan(&total) != nil {
		scimErr(w, 503, "try again")
		return
	}
	rows, err := s.DB.Query(q+` ORDER BY display_name LIMIT ? OFFSET ?`, append(args, count, start-1)...)
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			if body := s.groupBody(org, id); body != nil {
				out = append(out, body)
			}
		}
	}
	write(w, 200, map[string]any{"schemas": []string{listResp}, "totalResults": total, "startIndex": start, "itemsPerPage": len(out), "Resources": out})
}

func (s *Service) getGroup(w http.ResponseWriter, r *http.Request, org string) {
	body := s.groupBody(org, chi.URLParam(r, "id"))
	if body == nil {
		scimErr(w, 404, "group not found")
		return
	}
	write(w, 200, body)
}

func (s *Service) putGroup(w http.ResponseWriter, r *http.Request, org string) {
	in, role, ok := s.readGroup(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	if !s.saveGroup(tx, w, r, org, id, in, role, false) || tx.Commit() != nil {
		return
	}
	write(w, 200, s.groupBody(org, id))
}

func (s *Service) patchGroup(w http.ResponseWriter, r *http.Request, org string) {
	id := chi.URLParam(r, "id")
	cur := s.groupBody(org, id)
	if cur == nil {
		scimErr(w, 404, "group not found")
		return
	}
	in := scimGroup{DisplayName: cur["displayName"].(string)}
	if ext, ok := cur["externalId"].(string); ok {
		in.ExternalID = ext
	}
	role, _ := cur["role"].(string)
	for _, m := range cur["members"].([]any) {
		if row, ok := m.(map[string]any); ok {
			if v, ok := row["value"].(string); ok {
				in.Members = append(in.Members, struct {
					Value string `json:"value"`
				}{Value: v})
			}
		}
	}
	if !applyPatch(w, r, func(path string, value json.RawMessage) bool {
		switch strings.ToLower(path) {
		case "displayname":
			return json.Unmarshal(value, &in.DisplayName) == nil
		case "externalid":
			return json.Unmarshal(value, &in.ExternalID) == nil
		case "members":
			return json.Unmarshal(value, &in.Members) == nil
		case extGroup + ":role", "role":
			var v string
			if json.Unmarshal(value, &v) != nil || !validRole(v) {
				return false
			}
			role = v
			return true
		default:
			return false
		}
	}) {
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	if !s.saveGroup(tx, w, r, org, id, in, role, false) || tx.Commit() != nil {
		return
	}
	write(w, 200, s.groupBody(org, id))
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request, org string) {
	id := chi.URLParam(r, "id")
	tx, err := s.DB.Begin()
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	defer tx.Rollback()
	members := s.memberUsers(tx, org, id)
	res, err := tx.Exec(`DELETE FROM scim_groups WHERE id=? AND org_id=?`, id, org)
	if err != nil {
		scimErr(w, 503, "try again")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		scimErr(w, 404, "group not found")
		return
	}
	if _, err = tx.Exec(`DELETE FROM scim_group_members WHERE group_id=?`, id); err != nil {
		scimErr(w, 503, "try again")
		return
	}
	for _, uid := range members {
		if !s.syncRole(tx, org, uid) {
			scimErr(w, 503, "try again")
			return
		}
	}
	if !auditSCIM(tx, r, org, "scim.group.delete", "scim-group/"+id) || tx.Commit() != nil {
		scimErr(w, 503, "try again")
		return
	}
	w.WriteHeader(204)
}

func (s *Service) readGroup(w http.ResponseWriter, r *http.Request) (scimGroup, string, bool) {
	var raw map[string]json.RawMessage
	if !decode(w, r, &raw) {
		return scimGroup{}, "", false
	}
	var in scimGroup
	buf, _ := json.Marshal(raw)
	if json.Unmarshal(buf, &in) != nil || strings.TrimSpace(in.DisplayName) == "" || len(in.DisplayName) > 200 || len(in.ExternalID) > 200 || len(in.Members) > 500 {
		scimErr(w, 400, "invalid group")
		return scimGroup{}, "", false
	}
	role := ""
	if ext, ok := raw[extGroup]; ok {
		var box struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(ext, &box) != nil || !validRole(box.Role) {
			scimErr(w, 400, "invalid group role")
			return scimGroup{}, "", false
		}
		role = box.Role
	}
	return in, role, true
}

func (s *Service) saveGroup(tx *sql.Tx, w http.ResponseWriter, r *http.Request, org, id string, in scimGroup, role string, create bool) bool {
	if !validRole(role) {
		scimErr(w, 400, "invalid group role")
		return false
	}
	var n int
	if tx.QueryRow(`SELECT COUNT(*) FROM scim_groups WHERE org_id=? AND display_name=? AND id<>?`, org, in.DisplayName, id).Scan(&n) != nil || n > 0 {
		scimErr(w, 409, "displayName already exists")
		return false
	}
	if in.ExternalID != "" && (tx.QueryRow(`SELECT COUNT(*) FROM scim_groups WHERE org_id=? AND external_id=? AND id<>?`, org, in.ExternalID, id).Scan(&n) != nil || n > 0) {
		scimErr(w, 409, "externalId already exists")
		return false
	}
	previous := s.memberUsers(tx, org, id)
	if create {
		if _, err := tx.Exec(`INSERT INTO scim_groups(id,org_id,display_name,external_id,role) VALUES(?,?,?,?,?)`, id, org, in.DisplayName, in.ExternalID, role); err != nil {
			scimErr(w, 409, "group already exists")
			return false
		}
	} else {
		res, err := tx.Exec(`UPDATE scim_groups SET display_name=?, external_id=?, role=?, updated_at=datetime('now') WHERE id=? AND org_id=?`, in.DisplayName, in.ExternalID, role, id, org)
		if err != nil {
			scimErr(w, 503, "try again")
			return false
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			scimErr(w, 404, "group not found")
			return false
		}
		if _, err = tx.Exec(`DELETE FROM scim_group_members WHERE group_id=?`, id); err != nil {
			scimErr(w, 503, "try again")
			return false
		}
	}
	seen := map[string]bool{}
	for _, m := range in.Members {
		if m.Value == "" || seen[m.Value] {
			continue
		}
		seen[m.Value] = true
		var userID string
		if tx.QueryRow(`SELECT user_id FROM scim_users WHERE org_id=? AND id=?`, org, m.Value).Scan(&userID) != nil {
			scimErr(w, 400, "member is not a user in this organization")
			return false
		}
		if _, err := tx.Exec(`INSERT INTO scim_group_members(org_id,group_id,user_scim_id) VALUES(?,?,?)`, org, id, m.Value); err != nil {
			scimErr(w, 503, "try again")
			return false
		}
		previous = append(previous, userID)
	}
	fresh := s.memberUsers(tx, org, id)
	touched := append(previous, fresh...)
	done := map[string]bool{}
	for _, uid := range touched {
		if uid == "" || done[uid] {
			continue
		}
		done[uid] = true
		if !s.syncRole(tx, org, uid) {
			scimErr(w, 503, "try again")
			return false
		}
	}
	if !auditSCIM(tx, r, org, "scim.group.write", "scim-group/"+id) {
		scimErr(w, 503, "try again")
		return false
	}
	return true
}

func (s *Service) groupBody(org, id string) map[string]any {
	var name, external, role string
	err := s.DB.QueryRow(`SELECT display_name, external_id, role FROM scim_groups WHERE org_id=? AND id=?`, org, id).Scan(&name, &external, &role)
	if err != nil {
		return nil
	}
	rows, err := s.DB.Query(`SELECT user_scim_id FROM scim_group_members WHERE org_id=? AND group_id=? ORDER BY user_scim_id`, org, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	members := []any{}
	for rows.Next() {
		var uid string
		if rows.Scan(&uid) == nil {
			members = append(members, map[string]any{"value": uid})
		}
	}
	body := map[string]any{"schemas": []string{coreGroup, extGroup}, "id": id, "displayName": name, "members": members, "role": role}
	if external != "" {
		body["externalId"] = external
	}
	if role != "" {
		body[extGroup] = map[string]any{"role": role}
	}
	return body
}

func (s *Service) memberUsers(tx *sql.Tx, org, group string) []string {
	rows, err := tx.Query(`SELECT u.user_id FROM scim_group_members m JOIN scim_users u ON u.id=m.user_scim_id AND u.org_id=m.org_id WHERE m.org_id=? AND m.group_id=?`, org, group)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) syncRole(tx *sql.Tx, org, userID string) bool {
	var role string
	var managed int
	if tx.QueryRow(`SELECT role, COALESCE(scim_managed,0) FROM org_members WHERE org_id=? AND user_id=?`, org, userID).Scan(&role, &managed) != nil {
		return true
	}
	if role == "owner" || managed == 0 {
		return true
	}
	rows, err := tx.Query(`SELECT g.role FROM scim_groups g JOIN scim_group_members m ON m.group_id=g.id AND m.org_id=g.org_id JOIN scim_users u ON u.id=m.user_scim_id AND u.org_id=g.org_id WHERE g.org_id=? AND u.user_id=?`, org, userID)
	if err != nil {
		return false
	}
	defer rows.Close()
	next := "viewer"
	for rows.Next() {
		var g string
		if rows.Scan(&g) == nil && rbac.Rank(g) > rbac.Rank(next) {
			next = g
		}
	}
	_, err = tx.Exec(`UPDATE org_members SET role=? WHERE org_id=? AND user_id=? AND role<>'owner' AND COALESCE(scim_managed,0)=1`, next, org, userID)
	return err == nil
}

func (s *Service) deprovision(tx *sql.Tx, org, userID string) bool {
	var role string
	var owners int
	if tx.QueryRow(`SELECT role FROM org_members WHERE org_id=? AND user_id=?`, org, userID).Scan(&role) != nil {
		return true
	}
	if role == "owner" {
		if tx.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id=? AND role='owner'`, org).Scan(&owners) != nil || owners <= 1 {
			return false
		}
	}
	if _, err := tx.Exec(`DELETE FROM org_members WHERE org_id=? AND user_id=?`, org, userID); err != nil {
		return false
	}
	var left int
	if tx.QueryRow(`SELECT COUNT(*) FROM org_members WHERE user_id=?`, userID).Scan(&left) != nil {
		return false
	}
	if left == 0 {
		_, err := tx.Exec(`UPDATE users SET status='disabled' WHERE id=? AND auth_source='scim'`, userID)
		return err == nil
	}
	return true
}

// TokenRoutes are the authenticated management routes.
func (s *Service) TokenRoutes(r chi.Router) {
	r.Get("/", s.listTokens)
	r.Post("/", s.createToken)
	r.Delete("/{id}", s.revokeToken)
}

func (s *Service) listTokens(w http.ResponseWriter, r *http.Request) {
	org, ok := s.orgAdmin(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.Query(`SELECT id, name, created_at, last_used_at, expires_at, revoked_at FROM scim_tokens WHERE org_id=? ORDER BY created_at`, org)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var id, name, created string
		var last, exp, revoked int64
		if rows.Scan(&id, &name, &created, &last, &exp, &revoked) == nil {
			out = append(out, map[string]any{"id": id, "name": name, "created_at": created, "last_used_at": last, "expires_at": exp, "revoked": revoked != 0})
		}
	}
	writeJSON(w, 200, map[string]any{"tokens": out})
}

func (s *Service) createToken(w http.ResponseWriter, r *http.Request) {
	org, ok := s.orgAdmin(w, r)
	if !ok {
		return
	}
	var b struct {
		Name       string `json:"name"`
		TTLSeconds int64  `json:"ttl_seconds"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil || strings.TrimSpace(b.Name) == "" || len(b.Name) > 80 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if b.TTLSeconds < 0 || b.TTLSeconds > 365*24*3600 {
		writeErr(w, apperr.InvalidInput)
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	token := "taboo_scim_" + base64.RawURLEncoding.EncodeToString(buf)
	id := tc.NewID()
	exp := int64(0)
	if b.TTLSeconds > 0 {
		exp = time.Now().Unix() + b.TTLSeconds
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO scim_tokens(id,org_id,name,token_hash,created_by,expires_at) VALUES(?,?,?,?,?,?)`, id, org, strings.TrimSpace(b.Name), tc.SHA256(token), auth.From(r).ID, exp); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	if !auditSCIM(tx, r, org, "scim.token.create", "scim-token/"+id) || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "name": strings.TrimSpace(b.Name), "token": token, "expires_at": exp})
}

func (s *Service) revokeToken(w http.ResponseWriter, r *http.Request) {
	org, ok := s.orgAdmin(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE scim_tokens SET revoked_at=? WHERE id=? AND org_id=? AND revoked_at=0`, time.Now().Unix(), id, org)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErr(w, apperr.NotFound)
		return
	}
	if !auditSCIM(tx, r, org, "scim.token.revoke", "scim-token/"+id) || tx.Commit() != nil {
		writeErr(w, apperr.New(503, "AUDIT_UNAVAILABLE", "try again"))
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "revoked": true})
}

func (s *Service) orgAdmin(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := chi.URLParam(r, "slug")
	var org string
	if s.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, slug).Scan(&org) != nil {
		writeErr(w, apperr.NotFound)
		return "", false
	}
	if personal, err := orgsvc.IsPersonal(s.DB, org); err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "try again"))
		return "", false
	} else if personal {
		writeErr(w, apperr.New(409, "PERSONAL_NAMESPACE", "personal spaces cannot be shared or federated"))
		return "", false
	}
	actor := auth.From(r)
	if actor == nil || actor.Kind != auth.KindUser {
		auth.Deny(s.DB, w, r, org, "admin", "scim")
		return "", false
	}
	role, _, ok := auth.MemberInfo(s.DB, org, actor.ID)
	if !ok || !rbac.Privileged(role) {
		auth.Deny(s.DB, w, r, org, "admin", "scim")
		return "", false
	}
	return org, true
}

// scimEmail is always a synthetic address. The SCIM userName is kept in
// scim_users only: a provisioning token must not be able to claim a global
// email that a local, GitHub or invited account could later need.
func (s *Service) ensurePersonal(tx *sql.Tx, w http.ResponseWriter, userID string) bool {
	var email, name string
	if err := tx.QueryRow(`SELECT email, name FROM users WHERE id=?`, userID).Scan(&email, &name); err != nil {
		scimErr(w, 503, "try again")
		return false
	}
	if err := orgsvc.EnsurePersonalSpace(tx, s.MasterKey, userID, email, name); err != nil {
		slog.Error("scim personal space", "err", err)
		scimErr(w, 503, "try again")
		return false
	}
	return true
}

func scimEmail(org, userID string) string {
	return "scim-" + tc.SHA256(org + "\x00" + userID)[:32] + "@external.invalid"
}

func validRole(role string) bool {
	switch role {
	case "", "viewer", "developer", "admin":
		return true
	}
	return false
}

func eqFilter(w http.ResponseWriter, r *http.Request, kind string) (string, string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("filter"))
	if raw == "" {
		return "", "", true
	}
	m := filterEq.FindStringSubmatch(raw)
	if m == nil {
		scimErr(w, 400, "unsupported filter")
		return "", "", false
	}
	attr := m[1]
	if kind == "user" && attr == "displayName" || kind == "group" && attr == "userName" {
		scimErr(w, 400, "unsupported filter")
		return "", "", false
	}
	value := strings.ReplaceAll(m[2], `\\`, `\`)
	value = strings.ReplaceAll(value, `\"`, `"`)
	return attr, value, true
}

func page(r *http.Request) (int, int) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	if start < 1 {
		start = 1
	}
	if count < 1 || count > 100 {
		count = 100
	}
	return start, count
}

func applyPatch(w http.ResponseWriter, r *http.Request, fn func(string, json.RawMessage) bool) bool {
	var body struct {
		Operations []struct {
			Op    string          `json:"op"`
			Path  string          `json:"path"`
			Value json.RawMessage `json:"value"`
		} `json:"Operations"`
	}
	if !decode(w, r, &body) || len(body.Operations) == 0 || len(body.Operations) > 20 {
		return false
	}
	for _, op := range body.Operations {
		if !strings.EqualFold(op.Op, "replace") || !fn(op.Path, op.Value) {
			scimErr(w, 400, "unsupported patch")
			return false
		}
	}
	return true
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest); err != nil {
		scimErr(w, 400, "invalid JSON")
		return false
	}
	return true
}

func auditSCIM(tx *sql.Tx, r *http.Request, org, action, resource string) bool {
	actor := &auth.Actor{ID: "scim", Kind: "system", Name: "scim"}
	if a := auth.From(r); a != nil && a.ID != "" {
		actor = a
	}
	if err := auth.AuditReq(tx, r, org, actor, action, resource, nil); err != nil {
		return false
	}
	return true
}

func scimErr(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"schemas": []string{errSchema}, "status": strconv.Itoa(status), "detail": detail})
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, e *apperr.Error) {
	writeJSON(w, e.Status, e)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
