package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"
)

var (
	opts      Options
	flagCheck bool
)

// Execute runs the taboo-server command tree.
func Execute(o Options) {
	opts = o
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		reportExecuteErr(err)
		os.Exit(exitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "taboo-server",
		Short:         "taboo API server (API + embedded web UI)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagCheck {
				return runCheck()
			}
			return runServe()
		},
	}

	root.Version = buildinfo.Get().String()
	root.SetVersionTemplate("taboo-server {{.Version}}\n")
	root.CompletionOptions.DisableDefaultCmd = true
	root.Flags().BoolVar(&flagCheck, "check", false, "run self-check and exit")

	root.AddCommand(
		newVersionCmd(),
		newCheckCmd(),
		newBackupCmd(),
		newUserCmd(),
		newMigrateCmd(),
		newSchemaCmd(),
	)
	return root
}

func initJSONLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
