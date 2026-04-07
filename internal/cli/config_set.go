package cli

import (
	"fmt"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/platform"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/spf13/cobra"
)

// newConfigSetCmd constructs the `yap config set <key> <value>`
// subcommand.
//
// Keys are dot-notation paths into the nested schema. The command
// loads, mutates, validates, and saves. Validation failures abort
// before the file is touched.
//
// Struct-level mutations (e.g. appending to injection.app_overrides)
// are delegated to `yap config overrides`.
func newConfigSetCmd(_ *config.Config, p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value by dot-notation path.

Examples:
  yap config set general.hotkey KEY_SPACE
  yap config set general.max_duration 120
  yap config set transcription.backend groq
  yap config set transform.enabled true
  yap config set injection.electron_strategy keystroke

Mutations to injection.app_overrides are handled by
  yap config overrides add|remove|clear`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			if err := pcfg.Set(&loaded, key, value); err != nil {
				return err
			}

			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config would be invalid after set: %w", err)
			}

			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set %s to %s\n", key, value)
			return nil
		},
	}
}
