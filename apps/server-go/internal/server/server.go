// HTTP 装配：Chi 路由 + 中间件 + 项目上下文注入
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/identity"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/oidc"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	totpsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

type Deps struct {
	DB              *sql.DB
	MasterKey       []byte
	JWTSecret       string
	CORS            string
	DEKs            *tc.DEKCache
	LoginRate       int // 登录类接口每 IP 每窗口限流（0 → 默认 5）
	Dynamic         *dynamic.Service
	DataDir         string           // 数据目录（审计导出文件落盘；空 = 禁用异步导出）
	Events          *events.Bus      // M5：密钥变更事件总线（nil → 自动创建）
	Sync            *syncsvc.Service // M5 #11：nil → 自动创建（Subscribe 到 Events）
	Webhooks        *whsvc.Service   // M5 #12：nil → 自动创建（Subscribe 到 Events）
	DisableRegister bool
	DisablePassword bool
	WorkerCtx       context.Context
}

func New(d *Deps) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors(d.CORS))
	r.Use(securityHeaders)

	orgs := &orgsvc.Service{
		DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, DEKs: d.DEKs, DataDir: d.DataDir,
		DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword,
	}
	bus := d.Events
	if bus == nil {
		bus = events.NewBus()
	}
	secrets := &secretsvc.Service{DB: d.DB, MasterKey: d.MasterKey, DEKs: d.DEKs, Events: bus}
	folders := &folder.Service{DB: d.DB}
	identities := &identity.Service{DB: d.DB, JWTSecret: d.JWTSecret}
	totps := &totpsvc.Service{DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret}
	syncs := d.Sync
	if syncs == nil {
		syncs = &syncsvc.Service{DB: d.DB, MasterKey: d.MasterKey, DEKs: d.DEKs}
		syncs.Subscribe(bus)
	}
	webhooks := d.Webhooks
	if webhooks == nil {
		webhooks = &whsvc.Service{DB: d.DB, MasterKey: d.MasterKey}
		webhooks.Subscribe(bus)
	}
	dyn := d.Dynamic
	if dyn == nil {
		dyn = dynamic.New(d.DB, d.MasterKey)
	}
	loginRate := d.LoginRate
	if loginRate <= 0 {
		loginRate = 5
	}
	wctx := d.WorkerCtx
	if wctx == nil {
		wctx = context.Background()
	}
	orgs.StartExportWorker(wctx, 10*time.Second)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
			if d.DB == nil {
				http.Error(w, `{"ok":false}`, http.StatusServiceUnavailable)
				return
			}
			var one int
			if err := d.DB.QueryRow(`SELECT 1`).Scan(&one); err != nil {
				http.Error(w, `{"ok":false,"error":"db"}`, http.StatusServiceUnavailable)
				return
			}
			var ver int
			if err := d.DB.QueryRow(`PRAGMA user_version`).Scan(&ver); err != nil || ver != store.SchemaVersion {
				http.Error(w, `{"ok":false,"error":"schema"}`, http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		orgs.PublicRoutes(r)
		// client_credentials 换 token（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(time.Minute, loginRate)).Post("/identities/token", identities.Token)
		// TOTP 2FA 第二步（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(time.Minute, loginRate)).Post("/auth/totp/login", totps.Login2FA)
		// OIDC SSO 登录跳转 + callback（公开，登录同窗口限流；M5 #13）
		oidcSvc := &oidc.Service{
			DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret,
			DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword,
		}
		r.Get("/auth/sources", oidcSvc.ListSources)
		r.Route("/auth/oidc/{slug}", func(r chi.Router) {
			r.With(auth.LoginRateLimit(time.Minute, loginRate)).Get("/login", oidcSvc.Login)
			r.Get("/callback", oidcSvc.Callback)
		})
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
			// Secret Sync 目标与同步记录（M5 #11）
			syncs.Routes(r)
			// Webhooks 订阅与投递日志（M5 #12）
			webhooks.Routes(r)
			// OIDC SSO 配置（M5 #13）
			oidcSvc.Routes(r)
			// 项目作用域：注入 ProjectCtx 后挂环境管理 + 密钥 + 文件夹路由
			r.Route("/projects/{pid}", func(r chi.Router) {
				r.Use(projectCtx(d.DB))
				r.Get("/environments", orgs.ListEnvs)
				r.Post("/environments", orgs.CreateEnv)
				folders.Routes(r)
				secrets.Routes(r)
				dyn.Routes(r)
			})
		})
	})

	// API 文档（Scalar，引用 CDN；契约本体为 api/openapi.yaml）
	r.Get("/api/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>taboo API 文档</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head><body>
<script id="api-reference" data-url="/api/openapi.yaml"></script>
<script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
</body></html>`))
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
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
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
			var p project.Ctx
			err := db.QueryRow(`SELECT id, org_id, slug FROM projects WHERE id = ?`, chi.URLParam(r, "pid")).
				Scan(&p.ID, &p.OrgID, &p.Slug)
			if err != nil {
				writeErr(w, apperr.NotFound)
				return
			}
			if !auth.Require(db, w, r, p.OrgID, p.ID, "read", "") {
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
