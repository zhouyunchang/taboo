package server

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
)

// Restrict a BFF session to the organization proved by its login flow, before
// entering any legacy handler (including handlers using Membership directly).
// New unclassified routes are denied until their scope has been reviewed.
func federatedScope(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a := auth.From(r)
			if a == nil || a.AuthenticationSource == "" {
				next.ServeHTTP(w, r)
				return
			}
			if a.WorkloadBinding != "" {
				parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
				if len(parts) < 3 || parts[0] != "projects" {
					writeErr(w, apperr.Forbidden)
					return
				}
				switch parts[2] {
				case "secrets", "environments", "folders", "export":
				default:
					writeErr(w, apperr.Forbidden)
					return
				}
				var org string
				if err := db.QueryRow(`SELECT org_id FROM projects WHERE id=?`, parts[1]).Scan(&org); err != nil || org != a.OrganizationID {
					writeErr(w, apperr.Forbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path
			if path == "/api/v1/me" || path == "/api/v1/auth/session" || path == "/api/v1/auth/logout" {
				next.ServeHTTP(w, r)
				return
			}
			parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
			if len(parts) < 3 || parts[1] == "" {
				writeErr(w, apperr.Forbidden)
				return
			}
			var org string
			var err error
			switch parts[0] {
			case "orgs":
				// Automated export/delivery and identity governance require separate
				// migration gates; the first BFF slice exposes project and audit reads.
				if parts[2] != "projects" && parts[2] != "members" && parts[2] != "security" && parts[2] != "oidc" && parts[2] != "oauth-trusts" && parts[2] != "workloads" && (parts[2] != "audit" || len(parts) != 3) {
					writeErr(w, apperr.Forbidden)
					return
				}
				err = db.QueryRow(`SELECT id FROM orgs WHERE slug=?`, parts[1]).Scan(&org)
			case "projects":
				if parts[2] != "secrets" && parts[2] != "environments" && parts[2] != "folders" && parts[2] != "export" {
					writeErr(w, apperr.Forbidden)
					return
				}
				err = db.QueryRow(`SELECT org_id FROM projects WHERE id=?`, parts[1]).Scan(&org)
			default:
				writeErr(w, apperr.Forbidden)
				return
			}
			if err != nil || org != a.OrganizationID {
				writeErr(w, apperr.Forbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (d *Deps) allowedBrowser(a *auth.Actor) bool {
	if d.SessionProfile != "BOUNDED" || d.PublicOrigin == "" {
		return false
	}
	if a.BrowserOrigin != strings.TrimSuffix(d.PublicOrigin, "/") {
		return false
	}
	var issuer string
	if err := d.DB.QueryRow(`SELECT issuer FROM oidc_providers WHERE id=? AND org_id=?`, a.AuthenticationSource, a.OrganizationID).Scan(&issuer); err != nil {
		return false
	}
	for _, allowed := range d.IssuerAllowlist {
		if issuer == allowed {
			return true
		}
	}
	return false
}
