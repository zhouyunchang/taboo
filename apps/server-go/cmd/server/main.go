// taboo API Server 入口（Go 版，M1）—— 单体优先：API + 前端静态文件同端口
package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/server"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
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

	r := server.New(&server.Deps{
		DB:        db,
		MasterKey: masterKey,
		JWTSecret: cfg.JWTSecret,
		CORS:      cfg.CORS,
		DEKs:      tc.NewDEKCache(),
	})

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
