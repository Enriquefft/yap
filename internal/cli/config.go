package cli

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

	// Add subcommands
	configCmd.AddCommand(newConfigSetCmd(cfg))
	configCmd.AddCommand(newConfigGetCmd(cfg))
	configCmd.AddCommand(newConfigPathCmd(cfg))

	return configCmd
}
