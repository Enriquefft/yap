package cli

import (
	"fmt"

	"github.com/hybridz/yap/internal/config"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/spf13/cobra"
)

// newConfigGetCmd constructs the `yap config get <key>` subcommand.
//
// Keys are dot-notation paths into the nested schema defined in
// pkg/yap/config, e.g. `transcription.backend`, `general.hotkey`,
// `injection.app_overrides.0.match`.
//
// The rootCfg pointer is unused here — each invocation loads a fresh
// Config so env overrides and file edits land immediately.
func newConfigGetCmd(_ *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a configuration value by dot-notation path.

Examples:
  yap config get general.hotkey
  yap config get transcription.backend
  yap config get transform.enabled
  yap config get injection.electron_strategy
  yap config get injection.app_overrides.0.match

Section-level paths (e.g. "general") return a struct summary; drill
down to a leaf to get the exact value.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			value, err := pcfg.Get(&loaded, key)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), value)
			return nil
		},
	}
}
