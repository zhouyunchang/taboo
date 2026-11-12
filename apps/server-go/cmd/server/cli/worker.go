package cli

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/audit"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/outbox"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/rotation"
	secretsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/secret"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

// runWorker is the background-only role. It deliberately does not build the
// HTTP app or its route-scoped services; it owns the polling loops and writes
// the worker heartbeat that the API health endpoint reads.
func runWorker() error {
	initJSONLogger()
	if os.Getenv("TABOO_DATABASE_URL") == "" {
		fatalErr(errors.New("SQLite requires TABOO_ROLE=all"))
	}
	rt, err := loadRuntime(true)
	if err != nil {
		fatalErr(err)
	}
	defer rt.close()
	if !store.IsPostgres(rt.db) {
		fatalErr(errors.New("SQLite requires TABOO_ROLE=all"))
	}
	if os.Getenv("TABOO_DYNAMIC_ENGINE") == "mock" {
		mock := dynamic.NewMockEngine()
		dynamic.Factory = func(string, string) (dynamic.Engine, error) { return mock, nil }
		log.Println("[taboo] dynamic engine: MOCK (no real database touched)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deks := tc.NewDEKCache()
	bus := events.NewBus()
	syncs := &syncsvc.Service{DB: rt.db, MasterKey: rt.masterKey, DEKs: deks}
	syncs.Subscribe(bus)
	webhooks := &whsvc.Service{DB: rt.db, MasterKey: rt.masterKey}
	webhooks.Subscribe(bus)
	ob := &outbox.Service{DB: rt.db, Bus: bus}
	dynSvc := dynamic.New(rt.db, rt.masterKey)
	defer dynSvc.Close()
	rotSecrets := &secretsvc.Service{DB: rt.db, MasterKey: rt.masterKey, DEKs: deks, Events: bus}
	rotSvc := &rotation.Service{DB: rt.db, Secrets: rotSecrets, Role: "rotation"}

	rotSvc.StartWorker(ctx, 15*time.Second)
	syncs.StartWorker(ctx, 10*time.Second)
	webhooks.StartWorker(ctx, 5*time.Second)
	ob.StartWorker(ctx, 3*time.Second)
	dynSvc.StartWorker(ctx, 20*time.Second)
	audit.StartCheckpointWorker(ctx.Done(), rt.db, rt.masterKey, time.Minute)

	var srv *http.Server
	if addr := os.Getenv("TABOO_WORKER_ADDR"); addr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`{"ok":true,"role":"worker"}`))
		})
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			var one int
			if err := rt.db.QueryRowContext(r.Context(), `SELECT 1`).Scan(&one); err != nil {
				http.Error(w, `{"ok":false,"error":"db"}`, http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		srv = &http.Server{Addr: addr, Handler: mux}
		go func() {
			log.Printf("[taboo] worker health listening on %s", addr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("worker health server", "err", err)
			}
		}()
	}
	log.Printf("[taboo] worker role started (rotation + webhooks + outbox + sync + dynamic + audit)")
	<-ctx.Done()
	if srv != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}
	return nil
}
