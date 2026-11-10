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
				if len(parts) >= 3 && parts[0] == "orgs" && parts[2] == "scan-reports" {
					var org string
					if err := db.QueryRow(`SELECT id FROM orgs WHERE slug=?`, parts[1]).Scan(&org); err != nil || org != a.OrganizationID {
						writeErr(w, apperr.Forbidden)
						return
					}
					next.ServeHTTP(w, r)
					return
				}
				if len(parts) < 3 || parts[0] != "projects" {
					writeErr(w, apperr.Forbidden)
					return
				}
				switch parts[2] {
				case "secrets", "folders", "export", "import":
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
				switch parts[2] {
				case "projects", "members", "security", "oidc", "oauth-trusts", "workloads", "audit", "identities", "sync", "webhooks", "invites", "scan-reports":
				default:
					writeErr(w, apperr.Forbidden)
					return
				}

				err = db.QueryRow(`SELECT id FROM orgs WHERE slug=?`, parts[1]).Scan(&org)
			case "projects":
				if parts[2] != "secrets" && parts[2] != "folders" && parts[2] != "export" && parts[2] != "import" && parts[2] != "dynamic-engines" && parts[2] != "dynamic-leases" {
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
