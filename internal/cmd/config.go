package cmd

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *config.Config) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Read or write yap configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			// cfg is populated by PersistentPreRunE before this runs.
			// TODO(Phase 5): config wizard using cfg
			_ = cfg
			return cmd.Help()
		},
	}
	// Subcommands added in Phase 5
	return configCmd
}
