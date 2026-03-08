package cmd

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newToggleCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "toggle",
		Short: "Toggle recording (start if idle, stop if recording)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// cfg is populated by PersistentPreRunE before this runs.
			// TODO(Phase 3): send IPC toggle using cfg
			_ = cfg
			return nil
		},
	}
}
