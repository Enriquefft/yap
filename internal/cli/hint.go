package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/daemon"
	"github.com/Enriquefft/yap/internal/platform"
	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
	"github.com/spf13/cobra"
)

// newHintCmd builds the `yap hint` debug command. It runs the same
// provider walk the daemon uses at recording time and prints the
// resolved target, winning provider, and bundle summary. This is a
// debug tool — it has no side effects.
func newHintCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:   "hint",
		Short: "show the hint bundle for the currently focused window",
		Long: `hint runs the same provider walk the daemon uses at recording time
and prints the resolved target, winning provider, and bundle summary.
This is a debug tool — it has no side effects.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHint(cmd, cfg, p)
		},
	}
}

func runHint(cmd *cobra.Command, cfg *config.Config, p platform.Platform) error {
	out := cmd.OutOrStdout()

	if !cfg.Hint.Enabled {
		fmt.Fprintln(out, "hint: disabled in config (hint.enabled = false)")
		return nil
	}

	// Build injector to resolve the focused window target.
	inj, err := p.NewInjector(daemon.InjectionOptionsFromConfig(cfg.Injection))
	if err != nil {
		return fmt.Errorf("hint: build injector: %w", err)
	}

	// Resolve target.
	var target inject.Target
	resolver, ok := inj.(inject.StrategyResolver)
	if ok {
		timeoutMS := cfg.Hint.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 300
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		decision, err := resolver.Resolve(ctx)
		if err != nil {
			fmt.Fprintf(out, "hint: target resolution failed: %v\n\n", err)
		} else {
			target = decision.Target
		}
	} else {
		fmt.Fprintln(out, "hint: injector does not implement StrategyResolver")
		fmt.Fprintln(out, "      (target resolution unavailable on this platform)")
		fmt.Fprintln(out)
	}

	// Print target.
	writeHintTarget(out, target)

	// Resolve focused window's cwd and apply project overrides.
	rootPath := hint.ResolveTargetCwd(target)
	hintCfg := cfg.Hint
	if ov, err := hint.LoadProjectOverrides(rootPath); err == nil {
		if ov.VocabularyFiles != nil {
			hintCfg.VocabularyFiles = *ov.VocabularyFiles
		}
		if ov.Providers != nil {
			hintCfg.Providers = *ov.Providers
		}
	}

	// Read vocabulary files.
	vocab := hint.ReadVocabularyFiles(rootPath, hintCfg.VocabularyFiles)

	// Walk providers.
	var conversation, source string
	var providerErr string
	for _, name := range hintCfg.Providers {
		factory, err := hint.Get(name)
		if err != nil {
			fmt.Fprintf(out, "provider %q: unknown, skipping\n", name)
			continue
		}
		prov, err := factory(hint.Config{RootPath: rootPath})
		if err != nil {
			fmt.Fprintf(out, "provider %q: construction failed: %v\n", name, err)
			continue
		}
		if !prov.Supports(target) {
			fmt.Fprintf(out, "provider %q: does not support this target\n", name)
			continue
		}
		timeoutMS := cfg.Hint.TimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 300
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), time.Duration(timeoutMS)*time.Millisecond)
		b, err := prov.Fetch(ctx, target)
		cancel()
		if err != nil {
			providerErr = fmt.Sprintf("%s: %v", name, err)
			fmt.Fprintf(out, "provider %q: fetch failed: %v\n", name, err)
			continue
		}
		if b.Conversation != "" {
			conversation = b.Conversation
			source = name
			break
		}
		fmt.Fprintf(out, "provider %q: matched but returned empty conversation\n", name)
	}

	fmt.Fprintln(out)
	writeHintSummary(out, source, providerErr, vocab, conversation)
	return nil
}

// writeHintTarget renders the target block in the hint debug output.
func writeHintTarget(w io.Writer, t inject.Target) {
	fmt.Fprintln(w, "target:")
	fmt.Fprintf(w, "  display_server: %s\n", orNone(t.DisplayServer))
	fmt.Fprintf(w, "  app_class:      %s\n", orNone(t.AppClass))
	fmt.Fprintf(w, "  app_type:       %s\n", t.AppType.String())
	fmt.Fprintf(w, "  tmux:           %t\n", t.Tmux)
	fmt.Fprintln(w)
}

// writeHintSummary renders the bundle summary in the hint debug output.
func writeHintSummary(w io.Writer, source, providerErr string, vocab, conversation string) {
	fmt.Fprintf(w, "provider: %s\n", orNone(source))
	fmt.Fprintf(w, "vocabulary:    %d bytes\n", len(vocab))
	fmt.Fprintf(w, "conversation:  %d bytes\n", len(conversation))

	if providerErr != "" && source == "" {
		fmt.Fprintf(w, "provider_error: %s\n", providerErr)
	}

	if vocab != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "--- vocabulary (first 500 bytes) ---")
		fmt.Fprintln(w, truncatePreview(vocab, 500))
	}

	if conversation != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "--- conversation (first 500 bytes) ---")
		fmt.Fprintln(w, truncatePreview(conversation, 500))
	}
}

// truncatePreview returns the first n bytes of s, or s if shorter.
func truncatePreview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
