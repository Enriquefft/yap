package cmd

import "github.com/spf13/cobra"

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the yap daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(Phase 3): start daemon
			return nil
		},
	}
}
