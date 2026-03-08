package cmd

import "github.com/spf13/cobra"

func newToggleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "toggle",
		Short: "Toggle recording (start if idle, stop if recording)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO(Phase 3): send IPC toggle
			return nil
		},
	}
}
