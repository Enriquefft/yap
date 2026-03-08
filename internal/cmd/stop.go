package cmd

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running yap daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// cfg is populated by PersistentPreRunE before this runs.
			// TODO(Phase 3): send IPC stop using cfg
			_ = cfg
			return nil
		},
	}
}
