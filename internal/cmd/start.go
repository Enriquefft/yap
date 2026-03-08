package cmd

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newStartCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the yap daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			// cfg is populated by PersistentPreRunE before this runs.
			// TODO(Phase 3): start daemon using cfg
			_ = cfg
			return nil
		},
	}
}
