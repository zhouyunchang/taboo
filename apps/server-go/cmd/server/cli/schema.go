package cli

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/config"
	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

// newSchemaCmd 报告结构迁移状态，便于部署前检查（只读，不执行迁移）。
func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Show applied and pending schema migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			initJSONLogger()
			cfg := config.Load()
			db, closer, err := openSchemaDB(context.Background(), cfg)
			if err != nil {
				return exitf(1, "[schema] open db: %v", err)
			}
			defer closer()
			applied, err := store.Version(db)
			if err != nil {
				return exitf(1, "[schema] %v", err)
			}
			want := store.ExpectedVersion(db)
			pending, err := store.PendingMigrations(db)
			if err != nil {
				return exitf(1, "[schema] %v", err)
			}
			dialect := "sqlite"
			if store.IsPostgres(db) {
				dialect = "postgres"
			}
			fmt.Printf("dialect   %s\n", dialect)
			fmt.Printf("applied   %d\n", applied)
			fmt.Printf("expected  %d\n", want)
			if len(pending) == 0 {
				fmt.Println("state    up to date")
				return nil
			}
			fmt.Printf("pending   %d\n", len(pending))
			for _, name := range pending {
				fmt.Printf("  - %s\n", name)
			}
			return nil
		},
	}
}

// openSchemaDB 只打开连接，不触发迁移（auto-migrate 关闭时 store.Open 只校验版本，
// 这里需要的是原始连接，因此直接按配置打开）。
func openSchemaDB(ctx context.Context, cfg *config.Config) (*sql.DB, func(), error) {
	if cfg.DatabaseURL != "" {
		db, err := store.OpenPostgres(ctx, cfg.DatabaseURL, cfg.DatabaseRuntimeRole)
		return db, func() { db.Close() }, err
	}
	db, err := store.Open(cfg.DataDir)
	return db, func() { db.Close() }, err
}
