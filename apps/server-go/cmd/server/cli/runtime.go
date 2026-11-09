package cli

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// Options are wired from main (embed FS, etc.).
type Options struct {
	WebFS fs.FS
}

type runtime struct {
	cfg       *config.Config
	masterKey []byte
	db        *sql.DB
}

func loadRuntime(needDB bool) (*runtime, error) {
	cfg := config.Load()
	masterKey, err := tc.ParseMasterKey(cfg.MasterKey)
	if err != nil {
		return nil, exitf(1, "[taboo] master key: %v", err)
	}
	rt := &runtime{cfg: cfg, masterKey: masterKey}
	if !needDB {
		return rt, nil
	}
	var db *sql.DB
	if cfg.DatabaseURL != "" {
		db, err = store.OpenPostgres(context.Background(), cfg.DatabaseURL, cfg.DatabaseRuntimeRole)
	} else {
		db, err = store.Open(cfg.DataDir)
	}
	if err != nil {
		return nil, exitf(1, "[taboo] open db: %v", err)
	}
	rt.db = db
	return rt, nil
}

func (rt *runtime) close() {
	if rt != nil && rt.db != nil {
		_ = rt.db.Close()
	}
}
