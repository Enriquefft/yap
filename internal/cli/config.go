package cli

import (
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/platform"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Read or write yap configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cfg
			return cmd.Help()
		},
	}

	configCmd.AddCommand(newConfigSetCmd(cfg, p))
	configCmd.AddCommand(newConfigGetCmd(cfg))
	configCmd.AddCommand(newConfigPathCmd(cfg))
	configCmd.AddCommand(newConfigOverridesCmd(cfg, p))

	return configCmd
}
