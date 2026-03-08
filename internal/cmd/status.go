package cmd

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status (running/idle/not-running)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(Phase 3): send IPC status
			return nil
		},
	}
}
