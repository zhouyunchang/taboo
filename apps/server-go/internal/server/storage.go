package server

import (
	"context"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func scopedHTTP(d *Deps) http.Handler {
	// SQLite remains a single-connection portable deployment. PostgreSQL's
	// authenticated handler graph is constructed against a restricted pool.
	portable := protectedRoutes(d)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := chi.NewRouteContext()
		route.RoutePath = strings.TrimPrefix(r.URL.Path, "/api/v1")
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		a := auth.From(r)
		if a == nil {
			writeErr(w, apperr.Unauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
		org := ""
		if len(parts) >= 2 {
			var err error
			switch parts[0] {
			case "orgs":
				err = d.DB.QueryRow(`SELECT id FROM orgs WHERE slug=?`, parts[1]).Scan(&org)
			case "projects":
				err = d.DB.QueryRow(`SELECT org_id FROM projects WHERE id=?`, parts[1]).Scan(&org)
			case "invites":
				err = d.DB.QueryRow(`SELECT org_id FROM org_invites WHERE token_hash=?`, tc.SHA256(parts[1])).Scan(&org)
			}
			if err != nil {
				writeErr(w, apperr.NotFound)
				return
			}
		}
		if a.OrganizationID != "" && org != "" && a.OrganizationID != org {
			writeErr(w, apperr.Forbidden)
			return
		}
		if org != "" && parts[0] != "invites" {
			if a.Kind == auth.KindUser {
				if _, ok := auth.Membership(d.DB, org, a.ID); !ok {
					writeErr(w, apperr.Forbidden)
					return
				}
			} else {
				var n int
				if err := d.DB.QueryRow(`SELECT COUNT(*) FROM machine_identities WHERE id=? AND org_id=? AND status='active'`, a.ID, org).Scan(&n); err != nil || n != 1 {
					writeErr(w, apperr.Forbidden)
					return
				}
			}
		}
		if org != "" && !auth.OrganizationLoginAllowed(d.DB, a, org) {
			writeErr(w, apperr.New(403, "EXTERNAL_LOGIN_REQUIRED", "external sign-in to this organization is required"))
			return
		}
		if !store.IsPostgres(d.DB) {
			portable.ServeHTTP(w, r)
			return
		}
		db, closeScope, err := store.Scoped(r.Context(), d.DB, org, a.ID)
		if err != nil {
			writeErr(w, apperr.New(503, "STORAGE_UNAVAILABLE", "tenant storage is unavailable"))
			return
		}
		defer closeScope()
		copy := *d
		copy.DB = db
		protectedRoutes(&copy).ServeHTTP(w, r)
	})
}
