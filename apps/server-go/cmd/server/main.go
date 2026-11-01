// taboo API Server 入口（Go 版）—— 单体优先：API + 前端静态文件同端口
package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	ghsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/github"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/oidc"
	orgsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/org"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/server"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

//go:embed all:web
var webFS embed.FS

var mime = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript",
	".css":   "text/css",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("taboo-server " + buildinfo.Get().String())
		return
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := config.Load()

	masterKey, err := tc.ParseMasterKey(cfg.MasterKey)
	if err != nil {
		log.Fatalf("[taboo] master key: %v", err)
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "backup" {
		if cfg.DatabaseURL != "" {
			log.Fatal("PostgreSQL backup requires pg_dump plus the configured root key; see the recovery runbook")
		}
		dest := "backup"
		if len(args) > 1 {
			dest = args[1]
		}
		if err := store.Backup(cfg.DataDir, dest); err != nil {
			log.Fatalf("[taboo] backup: %v", err)
		}
		log.Printf("[taboo] backup written to %s", dest)
		return
	}

	var db *sql.DB
	if cfg.DatabaseURL != "" {
		db, err = store.OpenPostgres(context.Background(), cfg.DatabaseURL, cfg.DatabaseRuntimeRole)
	} else {
		db, err = store.Open(cfg.DataDir)
	}
	if err != nil {
		log.Fatalf("[taboo] open db: %v", err)
	}

	if len(args) > 0 && args[0] == "user" {
		code := runUserCmd(db, masterKey, args[1:])
		_ = db.Close()
		os.Exit(code)
	}

	for _, arg := range args {
		if arg == "--check" || arg == "check" {
			runSelfCheck(db, masterKey)
			_ = db.Close()
			return
		}
	}

	if os.Getenv("TABOO_DYNAMIC_ENGINE") == "mock" {
		mock := dynamic.NewMockEngine()
		dynamic.Factory = func(string, string) (dynamic.Engine, error) { return mock, nil }
		log.Println("[taboo] dynamic engine: MOCK (no real database touched)")
	}

	oidc.BootstrapFromEnv(db, masterKey)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dynSvc := dynamic.New(db, masterKey)
	defer dynSvc.Close()
	deks := tc.NewDEKCache()
	bus := events.NewBus()
	syncs := &syncsvc.Service{DB: db, MasterKey: masterKey, DEKs: deks}
	syncs.Subscribe(bus)
	webhooks := &whsvc.Service{DB: db, MasterKey: masterKey}
	webhooks.Subscribe(bus)
	ob := &outbox.Service{DB: db, Bus: bus}

	ghCfg, err := ghsvc.ConfigFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("[taboo] %v", err)
	}
	if ghCfg.Enabled() {
		if cfg.PublicOrigin == "" {
			log.Fatal("[taboo] GitHub login requires TABOO_PUBLIC_ORIGIN (callback = <origin>/api/v1/auth/github/callback)")
		}
		log.Printf("[taboo] GitHub login enabled (callback %s/api/v1/auth/github/callback, autocreate=%v)", strings.TrimRight(cfg.PublicOrigin, "/"), ghCfg.AutoCreate)
	}
	r := server.New(&server.Deps{
		PublicOrigin:    cfg.PublicOrigin,
		SessionProfile:  cfg.SessionProfile,
		IssuerAllowlist: cfg.IssuerAllowlist,
		DB:              db,
		MasterKey:       masterKey,
		JWTSecret:       cfg.JWTSecret,
		CORS:            cfg.CORS,
		DEKs:            deks,
		LoginRate:       cfg.LoginRate,
		Dynamic:         dynSvc,
		DataDir:         cfg.DataDir,
		Events:          bus,
		Sync:            syncs,
		Webhooks:        webhooks,
		DisableRegister: cfg.DisableRegister,
		DisablePassword: cfg.DisablePassword,
		WorkerCtx:       ctx,
		GitHub:          ghCfg,
	})

	dynSvc.StartWorker(ctx, 20*time.Second)
	(&rotation.Service{DB: db}).StartWorker(ctx, 20*time.Second)
	syncs.StartWorker(ctx, 10*time.Second)
	webhooks.StartWorker(ctx, 5*time.Second)
	ob.StartWorker(ctx, 3*time.Second)
	audit.StartCheckpointWorker(ctx.Done(), db, masterKey, time.Minute)

	dist, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("[taboo] embed web: %v", err)
	}
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		up := path.Clean("/" + req.URL.Path)
		if up == "/" || up == "." {
			up = "/index.html"
		}
		b, err := fs.ReadFile(dist, strings.TrimPrefix(up, "/"))
		if err != nil {
			serveIndex(w, dist)
			return
		}
		m := mime[strings.ToLower(path.Ext(up))]
		if m == "" {
			m = "application/octet-stream"
		}
		w.Header().Set("Content-Type", m)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})

	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Printf("[taboo] version %s", buildinfo.Get().String())
		log.Printf("[taboo] server listening on http://localhost%s", addr)
		log.Printf("[taboo] data dir: %s", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	_ = db.Close()
}

func serveIndex(w http.ResponseWriter, dist fs.FS) {
	b, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("taboo (禁制) API is running. Web UI not embedded yet — run `make web` first.\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}

func runSelfCheck(db *sql.DB, masterKey []byte) {
	ok := true
	check := func(name string, err error) {
		if err != nil {
			log.Printf("[check] FAIL %s: %v", name, err)
			ok = false
		} else {
			log.Printf("[check] ok   %s", name)
		}
	}

	ct, err := tc.Encrypt(masterKey, "taboo-self-check")
	check("encrypt", err)
	pt, err := tc.Decrypt(masterKey, ct)
	if err == nil && pt != "taboo-self-check" {
		err = errors.New("roundtrip mismatch")
	}
	check("decrypt roundtrip", err)

	hash, err := tc.HashPassword("check-password")
	check("argon2id hash", err)
	valid, _, err := tc.CheckPassword("check-password", hash)
	if err == nil && !valid {
		err = errors.New("password verify mismatch")
	}
	check("argon2id verify", err)

	v, err := store.Version(db)
	check("read schema version", err)
	if v != store.SchemaVersion {
		check("schema version", fmt.Errorf("user_version=%d, want %d", v, store.SchemaVersion))
	} else {
		log.Printf("[check] ok   schema version %d", v)
	}
	for _, t := range []string{
		"users", "orgs", "secrets", "secret_versions", "audit_logs", "webhooks", "sync_targets",
		"oidc_providers", "org_invites", "project_grants", "outbox_events", "audit_checkpoints", "scan_reports", "rotation_policies",
		"approval_requests", "access_requests", "scim_tokens", "cluster_leases",
	} {
		exists, err := store.HasTable(db, t)
		if err == nil && !exists {
			err = fmt.Errorf("missing table: %s", t)
		}
		check("table "+t, err)
	}

	if !ok {
		log.Printf("[check] FAILED")
		os.Exit(1)
	}
	log.Printf("[check] all checks passed")
}

// runUserCmd：本地账号运维（注册关闭时建号、停用/启用测试或离职账号）。
//
//	taboo-server user status  EMAIL
//	taboo-server user disable EMAIL
//	taboo-server user enable  EMAIL
//	taboo-server user create  EMAIL [--name NAME] [--disabled]   # 密码从 stdin 第一行读取
//
// 输出单行 JSON，便于脚本处理；从不回显密码。
func runUserCmd(db *sql.DB, masterKey []byte, args []string) int {
	usage := func() int {
		fmt.Fprintln(os.Stderr, "usage: taboo-server user status|disable|enable EMAIL | user create EMAIL [--name NAME] [--disabled] (password on stdin)")
		return 2
	}
	if len(args) < 2 {
		return usage()
	}
	cmd, email := args[0], args[1]
	out := func(v map[string]any) int {
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
		return 0
	}
	fail := func(err error) int {
		fmt.Fprintf(os.Stderr, "[taboo] user %s: %v\n", cmd, err)
		if errors.Is(err, orgsvc.ErrUserNotFound) {
			return 3
		}
		return 1
	}
	switch cmd {
	case "status":
		st, err := orgsvc.AdminUserStatus(db, email)
		if err != nil {
			return fail(err)
		}
		return out(map[string]any{"email": email, "status": st})
	case "disable", "enable":
		status := map[string]string{"disable": "disabled", "enable": "active"}[cmd]
		changed, err := orgsvc.AdminSetUserStatus(db, email, status)
		if err != nil {
			return fail(err)
		}
		return out(map[string]any{"email": email, "status": status, "changed": changed})
	case "create":
		name, status := "", "active"
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--name":
				if i+1 >= len(args) {
					return usage()
				}
				name = args[i+1]
				i++
			case "--disabled":
				status = "disabled"
			default:
				return usage()
			}
		}
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return fail(errors.New("password must be provided on stdin"))
		}
		u, err := orgsvc.AdminCreateUser(db, masterKey, email, name, strings.TrimRight(line, "\r\n"), status)
		if err != nil {
			return fail(err)
		}
		return out(map[string]any{"email": email, "status": status, "user_id": u.UserID, "org_slug": u.Slug})
	default:
		return usage()
	}
}
