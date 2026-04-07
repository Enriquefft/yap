package cli_test

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/spf13/cobra"
)

// TestCmdClosureInjection verifies that a subcommand factory built
// with the closure injection pattern receives a populated
// *config.Config after PersistentPreRunE fires.
func TestCmdClosureInjection(t *testing.T) {
	var injected config.Config

	root := &cobra.Command{Use: "root"}
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		injected = pcfg.DefaultConfig()
		injected.Transcription.APIKey = "injected-key"
		injected.General.Hotkey = "KEY_TEST"
		return nil
	}

	var seenCfg *config.Config
	sub := &cobra.Command{
		Use: "sub",
		RunE: func(cmd *cobra.Command, args []string) error {
			seenCfg = &injected
			return nil
		},
	}
	root.AddCommand(sub)

	root.SetArgs([]string{"sub"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if seenCfg == nil {
		t.Fatal("subcommand RunE did not set seenCfg")
	}
	if seenCfg.Transcription.APIKey != "injected-key" {
		t.Errorf("closure injection: APIKey got %q, want injected-key", seenCfg.Transcription.APIKey)
	}
	if seenCfg.General.Hotkey != "KEY_TEST" {
		t.Errorf("closure injection: Hotkey got %q, want KEY_TEST", seenCfg.General.Hotkey)
	}
}
