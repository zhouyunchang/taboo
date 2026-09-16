// taboo API Server 入口（Go 版，M1）—— 单体优先：API + 前端静态文件同端口
package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/dynamic"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/events"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/server"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
	syncsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/sync"
	whsvc "github.com/zhouyunchang/taboo/apps/server-go/internal/webhooks"
)

// web 目录由 Makefile 从 apps/web/dist 拷贝（构建期 embed）。
// 占位 index.html 保证 embed 在未构建前端时也可编译。
//
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
	cfg := config.Load()

	masterKey, err := tc.ParseMasterKey(cfg.MasterKey)
	if err != nil {
		log.Fatalf("[taboo] master key: %v", err)
	}

	db, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("[taboo] open db: %v", err)
	}
	defer db.Close()

	// 启动自检 --check：加密往返验证 + DB 迁移状态（设计文档 §8）
	for _, arg := range os.Args[1:] {
		if arg == "--check" || arg == "check" {
			runSelfCheck(db, masterKey)
			return
		}
	}

	// 动态密钥引擎 mock 开关（冒烟/CI：TABOO_DYNAMIC_ENGINE=mock）
	if os.Getenv("TABOO_DYNAMIC_ENGINE") == "mock" {
		mock := dynamic.NewMockEngine()
		dynamic.Factory = func(string) (dynamic.Engine, error) { return mock, nil }
		log.Println("[taboo] dynamic engine: MOCK (no real database touched)")
	}

	dynSvc := dynamic.New(db, masterKey)

	// Secret Sync（M5 #11）：事件订阅 + 后台 worker 同一实例
	deks := tc.NewDEKCache()
	bus := events.NewBus()
	syncs := &syncsvc.Service{DB: db, MasterKey: masterKey, DEKs: deks}
	syncs.Subscribe(bus)
	webhooks := &whsvc.Service{DB: db, MasterKey: masterKey}
	webhooks.Subscribe(bus)

	r := server.New(&server.Deps{
		DB:        db,
		MasterKey: masterKey,
		JWTSecret: cfg.JWTSecret,
		CORS:      cfg.CORS,
		DEKs:      deks,
		LoginRate: cfg.LoginRate,
		Dynamic:   dynSvc,
		DataDir:   cfg.DataDir,
		Events:    bus,
		Sync:      syncs,
		Webhooks:  webhooks,
	})

	// 动态密钥后台 worker：到期回收 + 身份吊销联动（M4 #9）
	dynSvc.StartWorker(context.Background(), 20*time.Second)
	// Secret Sync 后台 worker：消费 pending 同步任务（M5 #11）
	syncs.StartWorker(context.Background(), 10*time.Second)
	// Webhook 投递 worker：至少一次 + 指数退避（M5 #12）
	webhooks.StartWorker(context.Background(), 5*time.Second)

	// 静态前端（embed）；SPA fallback 到 index.html
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
			serveIndex(w, dist) // SPA fallback
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
	log.Printf("[taboo] server listening on http://localhost%s", addr)
	log.Printf("[taboo] data dir: %s", cfg.DataDir)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatal(err)
	}
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

// runSelfCheck 启动自检（--check）：全部通过 exit 0，任一失败 exit 1（设计文档 §8）
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

	// 1. 加密往返：Master Key 加解密自校验
	ct, err := tc.Encrypt(masterKey, "taboo-self-check")
	check("encrypt", err)
	pt, err := tc.Decrypt(masterKey, ct)
	if err == nil && pt != "taboo-self-check" {
		err = errors.New("roundtrip mismatch")
	}
	check("decrypt roundtrip", err)

	// 2. 密码哈希：Argon2id 校验路径
	hash, err := tc.HashPassword("check-password")
	check("argon2id hash", err)
	valid, _, err := tc.CheckPassword("check-password", hash)
	if err == nil && !valid {
		err = errors.New("password verify mismatch")
	}
	check("argon2id verify", err)

	// 3. DB 迁移状态：schema 版本号与关键表存在性
	var v int
	check("read user_version", db.QueryRow(`PRAGMA user_version`).Scan(&v))
	if v != store.SchemaVersion {
		check("schema version", fmt.Errorf("user_version=%d, want %d", v, store.SchemaVersion))
	} else {
		log.Printf("[check] ok   schema version %d", v)
	}
	for _, t := range []string{"users", "orgs", "secrets", "secret_versions", "audit_logs", "webhooks", "sync_targets", "oidc_providers"} {
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
