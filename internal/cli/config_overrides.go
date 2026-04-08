package cli

import (
	"fmt"
	"strconv"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/platform"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/spf13/cobra"
)

// newConfigOverridesCmd constructs `yap config overrides`, the
// mutation surface for injection.app_overrides. Slice mutation
// through dot-notation set is too error-prone — this subcommand
// keeps every write behind a validating helper.
func newConfigOverridesCmd(_ *config.Config, p platform.Platform) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overrides",
		Short: "manage injection.app_overrides entries",
		Long: `overrides manages the ordered injection.app_overrides slice
in your config file. Slice mutation through the generic
'config set' is too error-prone for this nested array, so every
write goes through a validating helper.

Sub-commands:

  yap config overrides list             dump the current overrides in order
  yap config overrides add <m> <s>      append match=<m> strategy=<s>
  yap config overrides remove <index>   delete by zero-based index
  yap config overrides clear            wipe every override`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newOverridesListCmd())
	cmd.AddCommand(newOverridesAddCmd(p))
	cmd.AddCommand(newOverridesRemoveCmd(p))
	cmd.AddCommand(newOverridesClearCmd(p))
	return cmd
}

func newOverridesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list all injection app_overrides in order",
		Long: `list prints every injection.app_overrides entry in the order
they appear in the config file. The leading number is the
zero-based index that 'overrides remove' takes. When no overrides
are configured the output is "(no app_overrides configured)" so
shell pipelines can detect the empty state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("config: overrides: list: load: %w", err)
			}
			if len(loaded.Injection.AppOverrides) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no app_overrides configured)")
				return nil
			}
			for i, ov := range loaded.Injection.AppOverrides {
				fmt.Fprintf(cmd.OutOrStdout(), "%d: match=%q strategy=%q\n", i, ov.Match, ov.Strategy)
			}
			return nil
		},
	}
}

func newOverridesAddCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "add <match> <strategy>",
		Short: "append an injection app_override",
		Long: `add appends a new {match, strategy} override to the end of
injection.app_overrides. <match> is a substring tested against the
focused window's class name; <strategy> is the inject strategy to
use when it matches.

The full updated config is re-validated before being written, so a
nonsensical strategy fails before the file is touched.

Example:

  yap config overrides add Code keystroke
  yap config overrides add chromium electron`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("config: overrides: add: load: %w", err)
			}
			loaded.Injection.AppOverrides = append(loaded.Injection.AppOverrides, pcfg.AppOverride{
				Match:    args[0],
				Strategy: args[1],
			})
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config: overrides: add: validate: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("config: overrides: add: save: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added override: match=%q strategy=%q\n", args[0], args[1])
			return nil
		},
	}
}

func newOverridesRemoveCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <index>",
		Short: "remove the injection app_override at <index>",
		Long: `remove deletes a single injection.app_overrides entry by
zero-based index. Indices reflect the current order shown by
'overrides list' — they are NOT stable across other mutations,
so list before each remove if you are scripting.

The remaining slice is re-validated before the file is rewritten.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("config: overrides: remove: index must be an integer: %w", err)
			}
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("config: overrides: remove: load: %w", err)
			}
			if idx < 0 || idx >= len(loaded.Injection.AppOverrides) {
				return fmt.Errorf("config: overrides: remove: index %d out of range (len=%d)", idx, len(loaded.Injection.AppOverrides))
			}
			removed := loaded.Injection.AppOverrides[idx]
			loaded.Injection.AppOverrides = append(
				loaded.Injection.AppOverrides[:idx],
				loaded.Injection.AppOverrides[idx+1:]...,
			)
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config: overrides: remove: validate: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("config: overrides: remove: save: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed override %d: match=%q strategy=%q\n", idx, removed.Match, removed.Strategy)
			return nil
		},
	}
}

func newOverridesClearCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "remove every injection app_override",
		Long: `clear empties the injection.app_overrides slice. Useful for
shell scripts that re-seed overrides from a fresh source — pair
this with a sequence of 'overrides add' calls.

The empty slice is re-validated before being written so a clear
that would invalidate any related setting still fails fast.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("config: overrides: clear: load: %w", err)
			}
			loaded.Injection.AppOverrides = nil
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config: overrides: clear: validate: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("config: overrides: clear: save: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cleared all injection app_overrides")
			return nil
		},
	}
}
