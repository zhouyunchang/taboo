// HTTP 装配：Chi 路由 + 中间件 + 项目上下文注入
package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/identity"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
	totpsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
)

type Deps struct {
	DB        *sql.DB
	MasterKey []byte
	JWTSecret string
	CORS      string
	DEKs      *tc.DEKCache
	LoginRate int // 登录类接口每 IP 每窗口限流（0 → 默认 5）
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
	folders := &folder.Service{DB: d.DB}
	identities := &identity.Service{DB: d.DB, JWTSecret: d.JWTSecret}
	totps := &totpsvc.Service{DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret}
	loginRate := d.LoginRate
	if loginRate <= 0 {
		loginRate = 5
	}

	r.Route("/api/v1", func(r chi.Router) {
		orgs.PublicRoutes(r)
		// client_credentials 换 token（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(time.Minute, loginRate)).Post("/identities/token", identities.Token)
		// TOTP 2FA 第二步（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(time.Minute, loginRate)).Post("/auth/totp/login", totps.Login2FA)
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(d.DB, d.JWTSecret))
			orgs.Routes(r)
			totps.Routes(r)
			// 机器身份管理（组织级，需 owner）
			r.Route("/orgs/{slug}/identities", func(r chi.Router) {
				r.Get("/", identities.List)
				r.Post("/", identities.Create)
				r.Post("/{id}/revoke", identities.Revoke)
			})
			// 项目作用域：注入 ProjectCtx 后挂环境管理 + 密钥 + 文件夹路由
			r.Route("/projects/{pid}", func(r chi.Router) {
				r.Use(projectCtx(d.DB))
				r.Get("/environments", orgs.ListEnvs)
				r.Post("/environments", orgs.CreateEnv)
				folders.Routes(r)
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

// projectCtx 校验项目存在 + read 权限，注入 project.Ctx
func projectCtx(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.From(r)
			var p project.Ctx
			err := db.QueryRow(`SELECT id, org_id, slug FROM projects WHERE id = ?`, chi.URLParam(r, "pid")).
				Scan(&p.ID, &p.OrgID, &p.Slug)
			if err != nil {
				writeErr(w, apperr.NotFound)
				return
			}
			if !auth.Can(db, u, p.OrgID, p.ID, "read", "") {
				writeErr(w, apperr.Forbidden)
				return
			}
			next.ServeHTTP(w, r.WithContext(project.With(r.Context(), p)))
		})
	}
}

func writeErr(w http.ResponseWriter, e *apperr.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(e)
}
