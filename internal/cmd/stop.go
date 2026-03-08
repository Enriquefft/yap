package cmd

import "github.com/spf13/cobra"

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running yap daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(Phase 3): send IPC stop
			return nil
		},
	}
}
