package cli

import (
	"fmt"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newConfigPathCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "print the config file path",
		Long: `path prints the absolute path to the yap configuration file
that the current invocation would load.

The resolution order is documented in 'yap --help' but, in short:
$YAP_CONFIG wins, then $XDG_CONFIG_HOME/yap/config.toml, then the
system /etc/yap/config.toml fallback. The first existing file wins;
if none exist, the per-user XDG path is printed so operators can
seed it.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ConfigPath()
			if err != nil {
				return fmt.Errorf("config: path: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}
