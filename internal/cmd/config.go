package cmd

import "github.com/spf13/cobra"

func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Read or write yap configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	// Subcommands added in Phase 5
	return configCmd
}
