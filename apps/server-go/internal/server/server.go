// HTTP 装配：Chi 路由 + 中间件 + 项目上下文注入
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
)

type Deps struct {
	DB        *sql.DB
	MasterKey []byte
	JWTSecret string
	CORS      string
	DEKs      *tc.DEKCache
}

func New(d *Deps) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors(d.CORS))
	r.Use(securityHeaders)

	orgs := &orgsvc.Service{DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, DEKs: d.DEKs}
	secrets := &secretsvc.Service{DB: d.DB, MasterKey: d.MasterKey, DEKs: d.DEKs}

	r.Route("/api/v1", func(r chi.Router) {
		orgs.PublicRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(d.DB, d.JWTSecret))
			orgs.Routes(r)
			// 项目作用域：注入 ProjectCtx 后挂环境管理 + 密钥路由
			r.Route("/projects/{pid}", func(r chi.Router) {
				r.Use(projectCtx(d.DB))
				r.Get("/environments", orgs.ListEnvs)
				r.Post("/environments", orgs.CreateEnv)
				secrets.Routes(r)
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(apperr.New(404, "NOT_FOUND", "no route: "+req.Method+" "+req.URL.Path))
	})
	return r
}

func cors(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// projectCtx 校验项目存在 + read 权限，注入 secret.ProjectCtx
func projectCtx(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.From(r)
			var p secretsvc.ProjectCtx
			err := db.QueryRow(`SELECT id, org_id, slug FROM projects WHERE id = ?`, chi.URLParam(r, "pid")).
				Scan(&p.ID, &p.OrgID, &p.Slug)
			if err != nil {
				writeErr(w, apperr.NotFound)
				return
			}
			if !auth.Can(db, u.ID, p.OrgID, "read", "") {
				writeErr(w, apperr.Forbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), secretsvc.ProjectKey, p)))
		})
	}
}

func writeErr(w http.ResponseWriter, e *apperr.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
