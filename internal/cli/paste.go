package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/spf13/cobra"
)

// pasteOptions bundles the per-invocation flags for `yap paste`. The
// struct keeps the cobra wiring and runPaste independently testable.
type pasteOptions struct {
	readStdin bool
	dryRun    bool
}

// newPasteCmd builds the `yap paste [text]` command. It constructs
// only the platform Injector and calls Inject on the supplied text —
// no recorder, no transcriber, no transformer. This is the canonical
// debug command for the Phase 4 inject layer: anything wrong with the
// inject path can be reproduced and tested in isolation.
//
// The text payload is supplied as a positional argument or via stdin.
// When --dry-run is set, the command bypasses Inject entirely: it
// type-asserts the platform injector to inject.StrategyResolver,
// calls Resolve, and writes the resulting StrategyDecision to the
// cobra command's writer. Resolve is a pure query — nothing touches
// the clipboard, no keystrokes are synthesized, no text is written
// to any pty. This is the recommended way to diagnose "yap paste
// chose the wrong branch" reports.
func newPasteCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	var opts pasteOptions
	cmd := &cobra.Command{
		Use:   "paste [text]",
		Short: "inject text at the cursor (debug the inject layer)",
		Long: `paste delivers text to the active window via the configured
inject strategy. It bypasses recording and transcription entirely;
the only thing it exercises is the platform's text-injection layer.

Pass the text as a positional argument or via stdin (use --stdin to
force stdin reading). Useful for verifying that the inject layer
selects the right strategy for your terminal / Electron app /
browser without having to record audio first.

Use --dry-run to report which strategy the inject layer would fire
(plus its fall-throughs) WITHOUT actually injecting anything. The
text argument is still required so the command's input shape stays
consistent, but it is never delivered.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaste(cmd, cfg, p, args, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.readStdin, "stdin", false,
		"read text from stdin instead of an arg")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false,
		"report the strategy that would fire without injecting anything")
	return cmd
}

func runPaste(cmd *cobra.Command, cfg *config.Config, p platform.Platform, args []string, opts pasteOptions) error {
	text, err := readTextInput(args, opts.readStdin, os.Stdin, stdinIsTerminal)
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
	if opts.dryRun {
		return runPasteDryRun(cmd.Context(), inj, cmd.OutOrStdout())
	}
	if err := inj.Inject(cmd.Context(), text); err != nil {
		return fmt.Errorf("paste: inject: %w", err)
	}
	return nil
}

// runPasteDryRun is the --dry-run branch of `yap paste`. It
// type-asserts the injector to StrategyResolver, runs Resolve, and
// writes the decision to the supplied writer. No Inject call is made
// — this is the pure-query debug surface that answers "which strategy
// would paste fire?" without mutating any external state.
//
// A type-assertion failure surfaces as a clean user-facing error
// naming the optional interface, so a user on a platform that does
// not yet implement Resolve can immediately tell why --dry-run is
// unavailable.
func runPasteDryRun(ctx context.Context, inj inject.Injector, out io.Writer) error {
	resolver, ok := inj.(inject.StrategyResolver)
	if !ok {
		return fmt.Errorf("paste: --dry-run not supported by the current injector (platform does not implement StrategyResolver)")
	}
	decision, err := resolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("paste: resolve: %w", err)
	}
	writeStrategyDecision(out, decision)
	return nil
}
