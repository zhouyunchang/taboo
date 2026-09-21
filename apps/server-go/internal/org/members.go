package org

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rbac"
)

type memberOut struct {
	UserID     string       `json:"user_id"`
	Email      string       `json:"email"`
	Name       string       `json:"name"`
	Role       string       `json:"role"`
	Restricted bool         `json:"restricted"`
	JoinedAt   string       `json:"joined_at"`
	Grants     []grantOut   `json:"grants"`
}

type grantOut struct {
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`
	ProjectName string `json:"project_name"`
	Role        string `json:"role"`
}

func validOrgRole(role string) bool {
	switch role {
	case "owner", "admin", "developer", "viewer":
		return true
	}
	return false
}

func (s *Service) requireMembers(w http.ResponseWriter, r *http.Request, orgID string) bool {
	return auth.Require(s.DB, w, r, orgID, "", rbac.Members, "")
}

func (s *Service) ownerCount(orgID string) int {
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM org_members WHERE org_id = ? AND role = 'owner'`, orgID).Scan(&n)
	return n
}

func (s *Service) ListMembers(w http.ResponseWriter, r *http.Request) {
	orgID, _, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	rows, err := s.DB.Query(`SELECT m.user_id, u.email, u.name, m.role, COALESCE(m.restricted,0), m.joined_at
		FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = ? ORDER BY m.joined_at`, orgID)
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	out := []memberOut{}
	for rows.Next() {
		var m memberOut
		var rest int
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &rest, &m.JoinedAt); err != nil {
			continue
		}
		m.Restricted = rest == 1
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		out[i].Grants = s.grantsOf(orgID, out[i].UserID)
	}
	writeJSON(w, 200, map[string]any{"members": out})
}

func (s *Service) grantsOf(orgID, userID string) []grantOut {
	rows, err := s.DB.Query(`SELECT g.project_id, p.slug, p.name, g.role
		FROM project_grants g JOIN projects p ON p.id = g.project_id
		WHERE g.org_id = ? AND g.user_id = ? ORDER BY p.slug`, orgID, userID)
	if err != nil {
		return []grantOut{}
	}
	defer rows.Close()
	out := []grantOut{}
	for rows.Next() {
		var g grantOut
		if rows.Scan(&g.ProjectID, &g.ProjectSlug, &g.ProjectName, &g.Role) == nil {
			out = append(out, g)
		}
	}
	return out
}

func (s *Service) AddMember(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	var b struct {
		Email      string `json:"email"`
		Role       string `json:"role"`
		Restricted bool   `json:"restricted"`
	}
	if err := jsonDecode(r, &b); err != nil || !emailRe.MatchString(strings.ToLower(strings.TrimSpace(b.Email))) {
		writeErr(w, apperr.InvalidInput)
		return
	}
	if !validOrgRole(b.Role) {
		b.Role = "viewer"
	}
	email := strings.ToLower(strings.TrimSpace(b.Email))
	var userID string
	if err := s.DB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID); err != nil {
		writeErr(w, apperr.New(404, "NOT_FOUND", "user not registered; send an invite instead"))
		return
	}
	rest := 0
	if b.Restricted {
		rest = 1
	}
	if _, err := s.DB.Exec(`INSERT INTO org_members (org_id, user_id, role, restricted) VALUES (?, ?, ?, ?)
		ON CONFLICT(org_id, user_id) DO UPDATE SET role = excluded.role, restricted = excluded.restricted`,
		orgID, userID, b.Role, rest); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "members.add", "org/"+slug+"/members/"+email,
		map[string]any{"role": b.Role, "restricted": b.Restricted}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 201, map[string]any{"user_id": userID, "email": email, "role": b.Role})
}

func (s *Service) PatchMember(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	userID := chi.URLParam(r, "userId")
	var current string
	if err := s.DB.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&current); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var b struct {
		Role       *string `json:"role"`
		Restricted *bool   `json:"restricted"`
	}
	if err := jsonDecode(r, &b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	role := current
	if b.Role != nil {
		if !validOrgRole(*b.Role) {
			writeErr(w, apperr.InvalidInput)
			return
		}
		role = *b.Role
	}
	if current == "owner" && role != "owner" && s.ownerCount(orgID) <= 1 {
		writeErr(w, apperr.New(400, "LAST_OWNER", "cannot demote the last owner"))
		return
	}
	sets := []string{"role = ?"}
	args := []any{role}
	if b.Restricted != nil {
		v := 0
		if *b.Restricted {
			v = 1
		}
		sets = append(sets, "restricted = ?")
		args = append(args, v)
	}
	args = append(args, orgID, userID)
	if _, err := s.DB.Exec(`UPDATE org_members SET `+strings.Join(sets, ", ")+` WHERE org_id = ? AND user_id = ?`, args...); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "members.update", "org/"+slug+"/members/"+userID,
		map[string]any{"role": role}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"user_id": userID, "role": role})
}

func (s *Service) RemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	userID := chi.URLParam(r, "userId")
	var role string
	if err := s.DB.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&role); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if role == "owner" && s.ownerCount(orgID) <= 1 {
		writeErr(w, apperr.New(400, "LAST_OWNER", "cannot remove the last owner"))
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM project_grants WHERE org_id = ? AND user_id = ?`, orgID, userID)
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "members.remove", "org/"+slug+"/members/"+userID, nil); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"user_id": userID, "deleted": true})
}

func (s *Service) PutGrants(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	userID := chi.URLParam(r, "userId")
	var exists string
	if err := s.DB.QueryRow(`SELECT user_id FROM org_members WHERE org_id = ? AND user_id = ?`, orgID, userID).Scan(&exists); err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	var b struct {
		Grants []struct {
			ProjectID string `json:"project_id"`
			Role      string `json:"role"`
		} `json:"grants"`
	}
	if err := jsonDecode(r, &b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	tx, err := s.DB.Begin()
	if err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM project_grants WHERE org_id = ? AND user_id = ?`, orgID, userID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	for _, g := range b.Grants {
		if !validOrgRole(g.Role) || g.ProjectID == "" {
			writeErr(w, apperr.InvalidInput)
			return
		}
		var orgCheck string
		if err := tx.QueryRow(`SELECT org_id FROM projects WHERE id = ?`, g.ProjectID).Scan(&orgCheck); err != nil || orgCheck != orgID {
			writeErr(w, apperr.New(400, "INVALID", "project not in this org"))
			return
		}
		if _, err := tx.Exec(`INSERT INTO project_grants (org_id, project_id, user_id, role) VALUES (?, ?, ?, ?)`,
			orgID, g.ProjectID, userID, g.Role); err != nil {
			writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
			return
		}
	}
	if err := auth.AuditReq(tx, r, orgID, auth.From(r), "members.grants", "org/"+slug+"/members/"+userID+"/grants",
		map[string]any{"count": len(b.Grants)}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	writeJSON(w, 200, map[string]any{"user_id": userID, "grants": s.grantsOf(orgID, userID)})
}

func (s *Service) CreateInvite(w http.ResponseWriter, r *http.Request) {
	orgID, slug, ok := s.orgOf(w, r)
	if !ok {
		return
	}
	if !s.requireMembers(w, r, orgID) {
		return
	}
	var b struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := jsonDecode(r, &b); err != nil {
		writeErr(w, apperr.InvalidInput)
		return
	}
	email := strings.ToLower(strings.TrimSpace(b.Email))
	if !emailRe.MatchString(email) {
		writeErr(w, apperr.InvalidEmail)
		return
	}
	if !validOrgRole(b.Role) {
		b.Role = "viewer"
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)
	id := tc.NewID()
	exp := time.Now().Add(7 * 24 * time.Hour).Unix()
	if _, err := s.DB.Exec(`INSERT INTO org_invites (id, org_id, email, role, token_hash, expires_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, orgID, email, b.Role, tc.SHA256(token), exp, auth.From(r).ID); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	if err := auth.AuditReq(s.DB, r, orgID, auth.From(r), "members.invite", "org/"+slug+"/invites/"+email,
		map[string]any{"role": b.Role}); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = "http://" + r.Host
	}
	writeJSON(w, 201, map[string]any{
		"id": id, "email": email, "role": b.Role,
		"expires_at": exp,
		"url":        origin + "/invite/" + token,
		"token":      token,
	})
}

func (s *Service) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	u := auth.From(r)
	if u == nil || u.Kind != auth.KindUser {
		writeErr(w, apperr.Unauthorized)
		return
	}
	token := chi.URLParam(r, "token")
	var id, orgID, email, role string
	var exp int64
	err := s.DB.QueryRow(`SELECT id, org_id, email, role, expires_at FROM org_invites WHERE token_hash = ?`,
		tc.SHA256(token)).Scan(&id, &orgID, &email, &role, &exp)
	if err != nil {
		writeErr(w, apperr.NotFound)
		return
	}
	if exp < time.Now().Unix() {
		_, _ = s.DB.Exec(`DELETE FROM org_invites WHERE id = ?`, id)
		writeErr(w, apperr.New(400, "EXPIRED", "invite expired"))
		return
	}
	if !strings.EqualFold(u.Email, email) {
		writeErr(w, apperr.New(403, "FORBIDDEN", "invite email does not match current user"))
		return
	}
	if _, err := s.DB.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(org_id, user_id) DO NOTHING`, orgID, u.ID, role); err != nil {
		writeErr(w, apperr.New(500, "INTERNAL", err.Error()))
		return
	}
	_, _ = s.DB.Exec(`DELETE FROM org_invites WHERE id = ?`, id)
	_ = auth.AuditReq(s.DB, r, orgID, u, "members.accept", "org/invite/"+email, map[string]any{"role": role})
	var slug string
	_ = s.DB.QueryRow(`SELECT slug FROM orgs WHERE id = ?`, orgID).Scan(&slug)
	writeJSON(w, 200, map[string]any{"org_id": orgID, "slug": slug, "role": role})
}

func jsonDecode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
