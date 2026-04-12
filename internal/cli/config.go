package cli

import (
	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/platform"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "read or write yap configuration",
		Long: `config inspects and mutates the yap configuration file.

Sub-commands:

  yap config get <key>          read a value by dot-notation path
  yap config set <key> <value>  write a value by dot-notation path
  yap config path               print the resolved config file path
  yap config overrides ...      manage injection.app_overrides

Every write goes through the same validator the daemon runs at
startup, so a 'yap config set' that would produce an invalid config
fails before the file is touched.`,
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
