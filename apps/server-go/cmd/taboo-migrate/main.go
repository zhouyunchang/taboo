// Offline migration/recovery utility. Credentials are read from environment or
// a key file; no credential is accepted in command-line flags or printed.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: taboo-migrate verify|envelopes|import-sqlite [-data-dir DIR] [-source DIR] [-apply]")
	}
	command := os.Args[1]
	if command != "verify" && command != "envelopes" && command != "import-sqlite" {
		return fmt.Errorf("unknown migration command")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	data := flags.String("data-dir", "data", "SQLite data directory / master.key location")
	sourceDir := flags.String("source", "", "source SQLite directory (offline copy)")
	apply := flags.Bool("apply", false, "persist envelope upgrades; requires all server instances stopped")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	raw := os.Getenv("TABOO_MASTER_KEY")
	if raw == "" {
		b, err := os.ReadFile(filepath.Join(*data, "master.key"))
		if err != nil {
			return fmt.Errorf("original master key is required")
		}
		raw = string(b)
	}
	key, err := tc.ParseMasterKey(raw)
	if err != nil {
		return err
	}
	defer clear(key)
	var db *sql.DB
	if dsn := os.Getenv("TABOO_DATABASE_URL"); dsn != "" {
		db, err = store.OpenPostgres(context.Background(), dsn, os.Getenv("TABOO_DATABASE_RUNTIME_ROLE"))
	} else {
		if _, err := os.Stat(filepath.Join(*data, "taboo.db")); err != nil {
			return fmt.Errorf("existing SQLite database is required")
		}
		db, err = store.Open(*data)
	}
	if err != nil {
		return err
	}
	defer db.Close()
	switch command {
	case "verify", "envelopes":
		n, err := store.UpgradeEnvelopes(db, key, command == "envelopes" && *apply)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"verified": true, "legacy_envelopes": n, "applied": command == "envelopes" && *apply})
	case "import-sqlite":
		if *sourceDir == "" || !store.IsPostgres(db) {
			return fmt.Errorf("import-sqlite requires -source and a PostgreSQL target")
		}
		if _, err := os.Stat(filepath.Join(*sourceDir, "taboo.db")); err != nil {
			return fmt.Errorf("existing source SQLite database is required")
		}
		src, err := store.Open(*sourceDir)
		if err != nil {
			return err
		}
		defer src.Close()
		if _, err = store.UpgradeEnvelopes(src, key, false); err != nil {
			return err
		}
		counts, err := store.ImportSQLite(src, db)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"imported": counts, "browser_sessions_expired": true})
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}
