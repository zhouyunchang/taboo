package cli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	tc "github.com/zhouyunchang/taboo/apps/server-go/internal/crypto"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// migrateFlags are shared by verify / envelopes / import-sqlite.
// Credentials come from TABOO_MASTER_KEY or <data-dir>/master.key — never from argv.
type migrateFlags struct {
	dataDir   string
	sourceDir string
	apply     bool
}

func newMigrateCmd() *cobra.Command {
	f := &migrateFlags{}
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Offline migration / recovery (verify, envelopes, import-sqlite)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitf(2, "usage: taboo-server migrate verify|envelopes|import-sqlite [--data-dir DIR] [--source DIR] [--apply]")
		},
	}
	cmd.PersistentFlags().StringVar(&f.dataDir, "data-dir", "data", "SQLite data directory / master.key location")
	cmd.AddCommand(
		newMigrateVerifyCmd(f),
		newMigrateEnvelopesCmd(f),
		newMigrateImportSQLiteCmd(f),
	)
	return cmd
}

func newMigrateVerifyCmd(f *migrateFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Decrypt-check all envelopes (no ciphertext rewrite)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateEnvelopes(f, false)
		},
	}
}

func newMigrateEnvelopesCmd(f *migrateFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "envelopes",
		Short: "Report (or atomically upgrade) legacy envelopes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateEnvelopes(f, true)
		},
	}
	cmd.Flags().BoolVar(&f.apply, "apply", false, "persist envelope upgrades; requires all server instances stopped")
	return cmd
}

func newMigrateImportSQLiteCmd(f *migrateFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import-sqlite",
		Short: "Copy a stopped SQLite replica into an empty PostgreSQL target",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateImportSQLite(f)
		},
	}
	cmd.Flags().StringVar(&f.sourceDir, "source", "", "source SQLite directory (offline copy)")
	return cmd
}

func runMigrateEnvelopes(f *migrateFlags, envelopesCmd bool) error {
	key, db, err := openMigrateDB(f.dataDir)
	if err != nil {
		return migrateFail(err)
	}
	defer clear(key)
	defer db.Close()
	apply := envelopesCmd && f.apply
	n, err := store.UpgradeEnvelopes(db, key, apply)
	if err != nil {
		return migrateFail(err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"verified":         true,
		"legacy_envelopes": n,
		"applied":          apply,
	})
}

func runMigrateImportSQLite(f *migrateFlags) error {
	key, db, err := openMigrateDB(f.dataDir)
	if err != nil {
		return migrateFail(err)
	}
	defer clear(key)
	defer db.Close()
	if f.sourceDir == "" || !store.IsPostgres(db) {
		return migrateFail(fmt.Errorf("import-sqlite requires --source and a PostgreSQL target"))
	}
	if _, err := os.Stat(filepath.Join(f.sourceDir, "taboo.db")); err != nil {
		return migrateFail(fmt.Errorf("existing source SQLite database is required"))
	}
	src, err := store.Open(f.sourceDir)
	if err != nil {
		return migrateFail(err)
	}
	defer src.Close()
	if _, err = store.UpgradeEnvelopes(src, key, false); err != nil {
		return migrateFail(err)
	}
	counts, err := store.ImportSQLite(src, db)
	if err != nil {
		return migrateFail(err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"imported":                 counts,
		"browser_sessions_expired": true,
	})
}

// openMigrateDB mirrors the former taboo-migrate entrypoint: require an existing
// root key and (for SQLite) an existing database; never generate replacements.
func openMigrateDB(dataDir string) ([]byte, *sql.DB, error) {
	raw := os.Getenv("TABOO_MASTER_KEY")
	if raw == "" {
		b, err := os.ReadFile(filepath.Join(dataDir, "master.key"))
		if err != nil {
			return nil, nil, fmt.Errorf("original master key is required")
		}
		raw = string(b)
	}
	key, err := tc.ParseMasterKey(raw)
	if err != nil {
		return nil, nil, err
	}
	var db *sql.DB
	if dsn := os.Getenv("TABOO_DATABASE_URL"); dsn != "" {
		db, err = store.OpenPostgres(context.Background(), dsn, os.Getenv("TABOO_DATABASE_RUNTIME_ROLE"))
	} else {
		if _, err := os.Stat(filepath.Join(dataDir, "taboo.db")); err != nil {
			clear(key)
			return nil, nil, fmt.Errorf("existing SQLite database is required")
		}
		db, err = store.Open(dataDir)
	}
	if err != nil {
		clear(key)
		return nil, nil, err
	}
	return key, db, nil
}

func migrateFail(err error) error {
	msg := fmt.Sprintf("migration failed: %v", err)
	fmt.Fprintln(os.Stderr, msg)
	return &exitError{code: 1, msg: msg, printed: true}
}
