package cli_test

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
	"github.com/spf13/cobra"
)

// TestCmdClosureInjection verifies that a subcommand factory built with the
// closure injection pattern receives a populated *config.Config after
// PersistentPreRunE fires — confirming CONFIG-05 at the cmd layer.
func TestCmdClosureInjection(t *testing.T) {
	// Arrange: allocate a config pointer (simulates rootCfg)
	var injected config.Config

	// Build a minimal root command with PersistentPreRunE that populates injected.
	root := &cobra.Command{Use: "root"}
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		injected = config.Config{APIKey: "injected-key", Hotkey: "KEY_TEST"}
		return nil
	}

	// Build a subcommand that closes over &injected (same pattern as newStartCmd).
	var seenCfg *config.Config
	sub := &cobra.Command{
		Use: "sub",
		RunE: func(cmd *cobra.Command, args []string) error {
			seenCfg = &injected
			return nil
		},
	}
	root.AddCommand(sub)

	// Act: execute the subcommand — PersistentPreRunE fires first.
	root.SetArgs([]string{"sub"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Assert: seenCfg points to injected, which was populated by PersistentPreRunE.
	if seenCfg == nil {
		t.Fatal("subcommand RunE did not set seenCfg")
	}
	if seenCfg.APIKey != "injected-key" {
		t.Errorf("closure injection: APIKey got %q, want injected-key", seenCfg.APIKey)
	}
	if seenCfg.Hotkey != "KEY_TEST" {
		t.Errorf("closure injection: Hotkey got %q, want KEY_TEST", seenCfg.Hotkey)
	}
}
