package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/spf13/cobra"
)

// newTransformCmd builds the `yap transform [text]` command. The
// command runs a single block of text through the configured
// transform backend and prints the result. Useful for verifying
// transform configuration without going through the recording or
// transcription pipeline.
//
// The text payload is supplied as a positional argument or via stdin.
// Flags:
//
//   - --backend forces a specific transform backend for one
//     invocation, overriding cfg.Transform.Backend AND implicitly
//     enabling the transform stage even when cfg.Transform.Enabled
//     is false.
//   - --system-prompt overrides cfg.Transform.SystemPrompt for one
//     invocation, useful for prompt-iteration debugging.
//   - --stdin reads the payload from stdin.
func newTransformCmd(cfg *config.Config) *cobra.Command {
	var (
		backend   string
		prompt    string
		readStdin bool
	)
	cmd := &cobra.Command{
		Use:   "transform [text]",
		Short: "run text through the configured transform backend",
		Long: `transform runs text through the configured transform backend
and prints the rewritten output.

Pass the text as a positional argument or via stdin (use --stdin to
force stdin reading). When --backend is supplied the transform stage
is enabled for this invocation regardless of the on-disk
transform.enabled value, so you can experiment with backends without
flipping the global setting.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransform(cmd, cfg, args, backend, prompt, readStdin)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "",
		"force a transform backend (default: configured)")
	cmd.Flags().StringVar(&prompt, "system-prompt", "",
		"override the system prompt for this invocation")
	cmd.Flags().BoolVar(&readStdin, "stdin", false,
		"read text from stdin instead of an arg")
	return cmd
}

func runTransform(cmd *cobra.Command, cfg *config.Config, args []string, backend, prompt string, readStdin bool) error {
	text, err := readTextInput(args, readStdin, os.Stdin, stdinIsTerminal)
	if err != nil {
		return fmt.Errorf("transform: %w", err)
	}
	if text == "" {
		return fmt.Errorf("transform: empty input")
	}

	// Build a temporary copy of the transform config so flag
	// overrides do not bleed into the persistent in-memory config.
	tc := cfg.Transform
	if backend != "" {
		tc.Backend = backend
		tc.Enabled = true
	}
	if prompt != "" {
		tc.SystemPrompt = prompt
	}

	tr, err := daemon.NewTransformer(tc)
	if err != nil {
		return fmt.Errorf("transform: build transformer: %w", err)
	}
	defer closeIfCloser(tr, "transformer")

	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: text, IsFinal: true}
	close(in)

	out, err := tr.Transform(cmd.Context(), in)
	if err != nil {
		return fmt.Errorf("transform: run: %w", err)
	}

	w := cmd.OutOrStdout()
	wroteAny := false
	for chunk := range out {
		if chunk.Err != nil {
			return fmt.Errorf("transform: chunk: %w", chunk.Err)
		}
		if chunk.Text != "" {
			if _, err := io.WriteString(w, chunk.Text); err != nil {
				return fmt.Errorf("transform: write: %w", err)
			}
			wroteAny = true
		}
	}
	if wroteAny {
		if _, err := io.WriteString(w, "\n"); err != nil {
			return fmt.Errorf("transform: write: %w", err)
		}
	}
	return nil
}
