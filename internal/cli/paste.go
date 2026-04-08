package cli

import (
	"fmt"
	"os"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/internal/platform"
	"github.com/spf13/cobra"
)

// newPasteCmd builds the `yap paste [text]` command. It constructs
// only the platform Injector and calls Inject on the supplied text —
// no recorder, no transcriber, no transformer. This is the canonical
// debug command for the Phase 4 inject layer: anything wrong with the
// inject path can be reproduced and tested in isolation.
//
// The text payload is supplied as a positional argument or via stdin.
func newPasteCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	var readStdin bool
	cmd := &cobra.Command{
		Use:   "paste [text]",
		Short: "inject text at the cursor (debug the inject layer)",
		Long: `paste delivers text to the active window via the configured
inject strategy. It bypasses recording and transcription entirely;
the only thing it exercises is the platform's text-injection layer.

Pass the text as a positional argument or via stdin (use --stdin to
force stdin reading). Useful for verifying that the inject layer
selects the right strategy for your terminal / Electron app /
browser without having to record audio first.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaste(cmd, cfg, p, args, readStdin)
		},
	}
	cmd.Flags().BoolVar(&readStdin, "stdin", false,
		"read text from stdin instead of an arg")
	return cmd
}

func runPaste(cmd *cobra.Command, cfg *config.Config, p platform.Platform, args []string, readStdin bool) error {
	text, err := readTextInput(args, readStdin, os.Stdin, stdinIsTerminal)
	if err != nil {
		return fmt.Errorf("paste: %w", err)
	}
	if text == "" {
		return fmt.Errorf("paste: empty input")
	}
	inj, err := p.NewInjector(daemon.InjectionOptionsFromConfig(cfg.Injection))
	if err != nil {
		return fmt.Errorf("paste: build injector: %w", err)
	}
	if err := inj.Inject(cmd.Context(), text); err != nil {
		return fmt.Errorf("paste: inject: %w", err)
	}
	return nil
}
