package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("taboo-server " + buildinfo.Get().String())
			return nil
		},
	}
}
