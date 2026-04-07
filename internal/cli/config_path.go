package cli

import (
	"fmt"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

func newConfigPathCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Long:  `Print the resolved path to the yap configuration file.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ConfigPath()
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}
