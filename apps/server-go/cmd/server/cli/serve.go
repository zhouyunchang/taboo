package cli

import (
	"context"
	"errors"
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
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	ghsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/github"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/oidc"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/server"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

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

func runServe() error {
	initJSONLogger()
	rt, err := loadRuntime(true)
	if err != nil {
		fatalErr(err)
	}
	defer rt.close()

	if os.Getenv("TABOO_DYNAMIC_ENGINE") == "mock" {
		mock := dynamic.NewMockEngine()
		dynamic.Factory = func(string, string) (dynamic.Engine, error) { return mock, nil }
		log.Println("[taboo] dynamic engine: MOCK (no real database touched)")
	}

	oidc.BootstrapFromEnv(rt.db, rt.masterKey)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dynSvc := dynamic.New(rt.db, rt.masterKey)
	defer dynSvc.Close()
	deks := tc.NewDEKCache()
	bus := events.NewBus()
	syncs := &syncsvc.Service{DB: rt.db, MasterKey: rt.masterKey, DEKs: deks}
	syncs.Subscribe(bus)
	webhooks := &whsvc.Service{DB: rt.db, MasterKey: rt.masterKey}
	webhooks.Subscribe(bus)
	ob := &outbox.Service{DB: rt.db, Bus: bus}

	ghCfg, err := ghsvc.ConfigFromEnv(os.Getenv)
	if err != nil {
		log.Fatalf("[taboo] %v", err)
	}
	if ghCfg.Enabled() {
		if rt.cfg.PublicOrigin == "" {
			log.Fatal("[taboo] GitHub login requires TABOO_PUBLIC_ORIGIN (callback = <origin>/api/v1/auth/github/callback)")
		}
		log.Printf("[taboo] GitHub login enabled (callback %s/api/v1/auth/github/callback, autocreate=%v)", strings.TrimRight(rt.cfg.PublicOrigin, "/"), ghCfg.AutoCreate)
	}
	r := server.New(&server.Deps{
		PublicOrigin:    rt.cfg.PublicOrigin,
		SessionProfile:  rt.cfg.SessionProfile,
		IssuerAllowlist: rt.cfg.IssuerAllowlist,
		DB:              rt.db,
		MasterKey:       rt.masterKey,
		JWTSecret:       rt.cfg.JWTSecret,
		CORS:            rt.cfg.CORS,
		DEKs:            deks,
		LoginRate:       rt.cfg.LoginRate,
		Dynamic:         dynSvc,
		DataDir:         rt.cfg.DataDir,
		Events:          bus,
		Sync:            syncs,
		Webhooks:        webhooks,
		DisableRegister: rt.cfg.DisableRegister,
		DisablePassword: rt.cfg.DisablePassword,
		WorkerCtx:       ctx,
		GitHub:          ghCfg,
	})

	dynSvc.StartWorker(ctx, 20*time.Second)
	(&rotation.Service{DB: rt.db}).StartWorker(ctx, 20*time.Second)
	syncs.StartWorker(ctx, 10*time.Second)
	webhooks.StartWorker(ctx, 5*time.Second)
	ob.StartWorker(ctx, 3*time.Second)
	audit.StartCheckpointWorker(ctx.Done(), rt.db, rt.masterKey, time.Minute)

	dist, err := fs.Sub(opts.WebFS, "web")
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

	addr := ":" + rt.cfg.Port
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		log.Printf("[taboo] version %s", buildinfo.Get().String())
		log.Printf("[taboo] server listening on http://localhost%s", addr)
		log.Printf("[taboo] data dir: %s", rt.cfg.DataDir)
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
	return nil
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
