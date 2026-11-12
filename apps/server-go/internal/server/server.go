// HTTP 装配：Chi 路由 + 中间件 + 项目上下文注入
package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/apperr"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/auth"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/device"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/folder"
	ghsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/github"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/govern"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/httpx"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/identity"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/oidc"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/project"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/redact"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/scanreport"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/scim"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	totpsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/totp"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/workload"
)

// errRecorder captures the status and the head of a response body for the
// diagnostic log above. It changes no response behaviour.
type errRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (e *errRecorder) WriteHeader(code int) {
	if e.status == 0 {
		e.status = code
	}
	e.ResponseWriter.WriteHeader(code)
}

func (e *errRecorder) Write(b []byte) (int, error) {
	if e.status == 0 {
		e.status = http.StatusOK
	}
	if e.body.Len() < 512 {
		e.body.Write(b[:min(len(b), 512-e.body.Len())])
	}
	return e.ResponseWriter.Write(b)
}

func (e *errRecorder) Flush() {
	if f, ok := e.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type Deps struct {
	AccountDB       *sql.DB // Trusted local account operations, always bound to the authenticated user.
	LookupLocalUser func(string) (string, error)
	Workloads       *workload.Service
	PublicOrigin    string
	IssuerAllowlist []string
	SessionProfile  string
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
	Rotation        *rotation.Service
	GitHub          ghsvc.Config // Instance-level "Sign in with GitHub" (disabled unless configured).
}

func New(d *Deps) *chi.Mux {
	copy := *d
	d = &copy
	if d.AccountDB == nil {
		d.AccountDB = d.DB
	}
	if d.LookupLocalUser == nil && d.DB != nil {
		control := d.DB
		d.LookupLocalUser = func(email string) (string, error) {
			var id string
			err := control.QueryRow(`SELECT id FROM users WHERE email=? AND auth_source='local' AND status='active'`, email).Scan(&id)
			return id, err
		}
	}
	if d.Workloads == nil {
		d.Workloads = workload.New(d.DB, d.MasterKey, d.IssuerAllowlist)
	}
	if d.Dynamic == nil {
		d.Dynamic = dynamic.New(d.DB, d.MasterKey)
	}
	r := chi.NewRouter()
	// 诊断开关：TABOO_DEBUG_ERRORS=1 时把所有 5xx 的 apperr 与内部错误写进服务端日志。
	// 默认关闭；开启后只写日志，不改变响应内容（响应仍是 {code,message}）。
	debugErrors := os.Getenv("TABOO_DEBUG_ERRORS") == "1"
	if debugErrors {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := &errRecorder{ResponseWriter: w}
				next.ServeHTTP(rec, r)
				if rec.status >= 500 {
					// 只记录最终响应与请求标识；处理器内部的真实原因由各自的 slog.Error 记录。
					slog.Error("request failed", "request_id", httpx.RequestID(r), "method", r.Method,
						"path", r.URL.Path, "status", rec.status, "body", rec.body.String())
				}
			})
		})
	}
	r.Use(middleware.RequestID)
	// 只采信受信代理的 X-Real-IP；机器身份 IP 白名单依赖这里。
	r.Use(httpx.RealIP(httpx.ParseTrusted(os.Getenv("TABOO_TRUSTED_PROXIES"))))
	r.Use(redact.AccessLog)
	r.Use(middleware.Recoverer)
	r.Use(cors(d.CORS))
	r.Use(securityHeaders)

	orgs := &orgsvc.Service{
		DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, DEKs: d.DEKs, DataDir: d.DataDir,
		DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword,
	}
	identities := &identity.Service{DB: d.DB, JWTSecret: d.JWTSecret}
	totps := &totpsvc.Service{DB: d.AccountDB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret}
	loginRate := d.LoginRate
	if loginRate <= 0 {
		loginRate = 5
	}
	wctx := d.WorkerCtx
	if wctx == nil {
		wctx = context.Background()
	}
	orgs.StartExportWorker(wctx, 10*time.Second)
	govern.Start(wctx, d.DB)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			bi := buildinfo.Get()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": bi.Version, "commit": bi.Commit, "build_time": bi.BuildTime})
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
			ver, err := store.Version(d.DB)
			if err != nil || ver != store.ExpectedVersion(d.DB) {
				http.Error(w, `{"ok":false,"error":"schema"}`, http.StatusServiceUnavailable)
				return
			}
			bi := buildinfo.Get()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": bi.Version, "commit": bi.Commit, "build_time": bi.BuildTime})
		})
		orgs.PublicRoutes(r)
		// client_credentials 换 token（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/identities/token", identities.Token)
		// TOTP 2FA 第二步（公开，登录同窗口限流）
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/auth/totp/login", totps.Login2FA)
		// OIDC SSO 登录跳转 + callback（公开，登录同窗口限流；M5 #13）
		oidcSvc := &oidc.Service{
			PublicOrigin: d.PublicOrigin, SessionProfile: d.SessionProfile, IssuerAllowlist: d.IssuerAllowlist,
			DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret,
			DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword,
		}
		gh := &ghsvc.Service{Cfg: d.GitHub, DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, PublicOrigin: d.PublicOrigin}
		oidcSvc.GitHubEnabled = gh.Enabled()
		// Sign in with GitHub (OAuth App, instance-level; disabled unless TABOO_GITHUB_* set).
		r.Route("/auth/github", func(r chi.Router) {
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Get("/login", gh.Login)
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Get("/callback", gh.Callback)
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/session", gh.Session)
			r.With(auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Get("/link", gh.LinkStatus)
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate), auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Post("/link", gh.StartLink)
			r.With(auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Delete("/link", gh.Unlink)
		})
		r.Get("/auth/session", auth.BrowserSession(d.DB, oidcSvc.CheckSession))
		r.With(auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Get("/auth/organizations", oidcSvc.Organizations)
		r.Post("/auth/logout", oidcSvc.Logout)
		r.Get("/auth/sources", oidcSvc.ListSources)
		r.Route("/auth/oidc/{slug}", func(r chi.Router) {
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Get("/login", oidcSvc.Login)
			r.Get("/callback", oidcSvc.Callback)
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/backchannel-logout", oidcSvc.BackChannel)
			r.With(auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Get("/connections", oidcSvc.Connections)
			r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate), auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Post("/replace", oidcSvc.Replace)
		})
		cli := &device.Service{DB: d.DB, JWTSecret: d.JWTSecret, Origin: d.PublicOrigin}
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/auth/device", cli.Start)
		// Polling is paced per device with slow_down (5s); the address cap only
		// stops device_code guessing, so it sits above one poller's 12/min.
		r.With(auth.LoginRateLimit(d.DB, time.Minute, 30)).Post("/auth/device/token", cli.Token)
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate)).Post("/auth/cli/token", cli.Exchange)
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate), auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Post("/auth/device/decision", cli.Decision)
		r.With(auth.LoginRateLimit(d.DB, time.Minute, loginRate), auth.Middleware(d.DB, d.JWTSecret, oidcSvc.CheckSession)).Post("/auth/cli/authorize", cli.Authorize)
		r.Route("/scim/v2", (&scim.Service{DB: d.DB, MasterKey: d.MasterKey}).Routes)
		r.Group(func(r chi.Router) {
			r.Use(auth.MiddlewareWithExternal(d.DB, d.JWTSecret, d.Workloads.Authenticate, oidcSvc.CheckSession))
			r.Use(federatedScope(d.DB))
			handler := scopedHTTP(d)
			if err := chi.Walk(protectedRoutes(d), func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
				r.Method(method, path, handler)
				if path != "/" && strings.HasSuffix(path, "/") {
					r.Method(method, strings.TrimSuffix(path, "/"), handler)
				}
				return nil
			}); err != nil {
				panic(err)
			}
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
		w.Header().Set("Cache-Control", "no-store")
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,Idempotency-Key,X-Taboo-Trust,X-Taboo-Organization")
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
		w.Header().Set("Cache-Control", "no-store")
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
			if !auth.ProjectVisible(db, auth.From(r), p.OrgID, p.ID) {
				auth.Deny(db, w, r, p.OrgID, "read", "project/"+p.Slug)
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

// No worker or subscription is started while building request-scoped handlers.
func protectedRoutes(d *Deps) *chi.Mux {
	r := chi.NewRouter()
	orgs := &orgsvc.Service{DB: d.DB, ControlDB: d.AccountDB, LookupLocalUser: d.LookupLocalUser, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, DEKs: d.DEKs, DataDir: d.DataDir, DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword}
	folders := &folder.Service{DB: d.DB, ControlDB: d.AccountDB}
	secrets := &secretsvc.Service{DB: d.DB, MasterKey: d.MasterKey, DEKs: d.DEKs, Events: d.Events}
	rot := d.Rotation
	if rot == nil {
		rot = &rotation.Service{DB: d.DB}
	}
	if rot.Secrets == nil {
		rot.Secrets = secrets
	}
	identities := &identity.Service{DB: d.DB, JWTSecret: d.JWTSecret}
	totps := &totpsvc.Service{DB: d.AccountDB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret}
	syncs := &syncsvc.Service{DB: d.DB, MasterKey: d.MasterKey, DEKs: d.DEKs}
	webhooks := &whsvc.Service{DB: d.DB, MasterKey: d.MasterKey}
	dyn := d.Dynamic.WithDB(d.DB)
	oidcSvc := &oidc.Service{DB: d.DB, MasterKey: d.MasterKey, JWTSecret: d.JWTSecret, PublicOrigin: d.PublicOrigin, SessionProfile: d.SessionProfile, IssuerAllowlist: d.IssuerAllowlist, DisableRegister: d.DisableRegister, DisablePassword: d.DisablePassword}
	orgs.Routes(r)
	rot.OrgRoutes(r)
	r.Get("/access", secrets.Access)
	r.Put("/access", secrets.Access)
	r.Post("/access", secrets.Access)
	r.Delete("/access", secrets.Access)
	d.Workloads.WithDB(d.DB).Routes(r)
	totps.Routes(r)
	// 机器身份管理（组织级，需 owner）
	r.Route("/orgs/{slug}/identities", func(r chi.Router) {
		r.Get("/", identities.List)
		r.Post("/", identities.Create)
		r.Patch("/{id}", identities.Update)
		r.Post("/{id}/revoke", identities.Revoke)
	})
	// Secret Sync 目标与同步记录（M5 #11）
	syncs.Routes(r)
	// Webhooks 订阅与投递日志（M5 #12）
	webhooks.Routes(r)
	// OIDC SSO 配置（M5 #13）
	oidcSvc.Routes(r)
	r.Route("/orgs/{slug}/scan-reports", (&scanreport.Service{DB: d.DB, MasterKey: d.MasterKey}).Routes)
	gov := &govern.Service{DB: d.DB, Secrets: secrets}
	r.Route("/orgs/{slug}/approvals", gov.ApprovalRoutes)
	r.Route("/orgs/{slug}/access-requests", gov.AccessRoutes)
	r.Route("/orgs/{slug}/scim/tokens", (&scim.Service{DB: d.DB, MasterKey: d.MasterKey}).TokenRoutes)
	// 项目作用域：注入 ProjectCtx 后挂密钥与文件夹路由
	r.Route("/projects/{pid}", func(r chi.Router) {
		r.Use(projectCtx(d.DB))
		r.Patch("/", orgs.PatchProject)
		folders.Routes(r)
		secrets.Routes(r)
		rot.Routes(r)
		dyn.Routes(r)
	})
	return r
}
