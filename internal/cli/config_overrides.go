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
		Short: "Manage injection.app_overrides entries",
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
		Short: "List all injection app_overrides in order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
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
		Short: "Append an injection app_override",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			loaded.Injection.AppOverrides = append(loaded.Injection.AppOverrides, pcfg.AppOverride{
				Match:    args[0],
				Strategy: args[1],
			})
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config would be invalid after add: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added override: match=%q strategy=%q\n", args[0], args[1])
			return nil
		},
	}
}

func newOverridesRemoveCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <index>",
		Short: "Remove the injection app_override at <index>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idx, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("index must be an integer: %w", err)
			}
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if idx < 0 || idx >= len(loaded.Injection.AppOverrides) {
				return fmt.Errorf("index %d out of range (len=%d)", idx, len(loaded.Injection.AppOverrides))
			}
			removed := loaded.Injection.AppOverrides[idx]
			loaded.Injection.AppOverrides = append(
				loaded.Injection.AppOverrides[:idx],
				loaded.Injection.AppOverrides[idx+1:]...,
			)
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config would be invalid after remove: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed override %d: match=%q strategy=%q\n", idx, removed.Match, removed.Strategy)
			return nil
		},
	}
}

func newOverridesClearCmd(p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Remove every injection app_override",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			loaded, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			loaded.Injection.AppOverrides = nil
			if err := loaded.Validate(p.HotkeyCfg); err != nil {
				return fmt.Errorf("config would be invalid after clear: %w", err)
			}
			if err := config.Save(loaded); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Cleared all injection app_overrides")
			return nil
		},
	}
}
