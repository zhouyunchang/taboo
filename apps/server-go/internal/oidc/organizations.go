package oidc

import (
	"net/http"
	"net/url"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
)

// Organizations exposes only the authenticated human's own memberships and
// enabled connections for the already-proved issuer. It never changes scope:
// selecting a connection starts a fresh organization-bound authorization flow.
func (s *Service) Organizations(w http.ResponseWriter, r *http.Request) {
	a := auth.From(r)
	if a == nil || a.Kind != auth.KindUser || a.BrowserOrigin == "" {
		writeErr(w, apperr.Forbidden)
		return
	}
	rows, err := s.DB.Query(`SELECT o.id,o.name,o.slug,p.id,p.name,p.issuer FROM org_members m JOIN orgs o ON o.id=m.org_id JOIN oidc_providers p ON p.org_id=o.id JOIN external_identities e ON e.user_id=m.user_id AND e.issuer=p.issuer WHERE m.user_id=? AND p.enabled=1 ORDER BY o.name,p.name,p.id`, a.ID)
	if err != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "organization choices unavailable"))
		return
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var id, name, slug, pid, pname, issuer string
		if rows.Scan(&id, &name, &slug, &pid, &pname, &issuer) != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "organization choices unavailable"))
			return
		}
		if !s.allowedIssuer(issuer) {
			continue
		}
		result = append(result, map[string]any{"id": id, "name": name, "connection": pname, "current": id == a.OrganizationID, "login_url": "/api/v1/auth/oidc/" + url.PathEscape(slug) + "/login?pid=" + url.QueryEscape(pid)})
	}
	if rows.Err() != nil {
		writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "organization choices unavailable"))
		return
	}
	writeJSON(w, 200, map[string]any{"organizations": result})
}
