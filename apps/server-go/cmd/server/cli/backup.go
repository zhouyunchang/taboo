package cli

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/store"
)

func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup [dest]",
		Short: "Backup SQLite data directory (PostgreSQL: use pg_dump)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			initJSONLogger()
			rt, err := loadRuntime(false)
			if err != nil {
				return err
			}
			if rt.cfg.DatabaseURL != "" {
				return exitf(1, "PostgreSQL backup requires pg_dump plus the configured root key; see the recovery runbook")
			}
			dest := "backup"
			if len(args) > 0 {
				dest = args[0]
			}
			if err := store.Backup(rt.cfg.DataDir, dest); err != nil {
				return exitf(1, "[taboo] backup: %v", err)
			}
			log.Printf("[taboo] backup written to %s", dest)
			return nil
		},
	}
}
