// taboo API Server 入口（Go 版）—— 单体优先：API + 前端静态文件同端口
package main

import (
	"context"
	"database/sql"
	"embed"
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
	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/oidc"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := config.Load()

	masterKey, err := tc.ParseMasterKey(cfg.MasterKey)
	if err != nil {
		log.Fatalf("[taboo] master key: %v", err)
	}

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "backup" {
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

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("[taboo] open db: %v", err)
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
		dynamic.Factory = func(string) (dynamic.Engine, error) { return mock, nil }
		log.Println("[taboo] dynamic engine: MOCK (no real database touched)")
	}

	oidc.BootstrapFromEnv(db, masterKey)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dynSvc := dynamic.New(db, masterKey)
	deks := tc.NewDEKCache()
	bus := events.NewBus()
	syncs := &syncsvc.Service{DB: db, MasterKey: masterKey, DEKs: deks}
	syncs.Subscribe(bus)
	webhooks := &whsvc.Service{DB: db, MasterKey: masterKey}
	webhooks.Subscribe(bus)
	ob := &outbox.Service{DB: db, Bus: bus}

	r := server.New(&server.Deps{
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
	})

	dynSvc.StartWorker(ctx, 20*time.Second)
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

	var v int
	check("read user_version", db.QueryRow(`PRAGMA user_version`).Scan(&v))
	if v != store.SchemaVersion {
		check("schema version", fmt.Errorf("user_version=%d, want %d", v, store.SchemaVersion))
	} else {
		log.Printf("[check] ok   schema version %d", v)
	}
	for _, t := range []string{
		"users", "orgs", "secrets", "secret_versions", "audit_logs", "webhooks", "sync_targets",
		"oidc_providers", "org_invites", "project_grants", "outbox_events", "audit_checkpoints",
	} {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t).Scan(&n)
		if err == nil && n == 0 {
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
