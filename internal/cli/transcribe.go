package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/spf13/cobra"
)

// newTranscribeCmd builds the `yap transcribe <file.wav>` command. It
// constructs only a Transcriber (no recorder, no transformer, no
// injector) and runs the file through it. Useful for verifying
// transcription backend configuration without going anywhere near the
// hotkey or the active window.
//
// Pass "-" as the path to read from stdin.
func newTranscribeCmd(cfg *config.Config) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "transcribe <file.wav>",
		Short: "transcribe a WAV file using the configured backend",
		Long: `transcribe runs an existing WAV file through the configured
transcription backend and prints the result.

Pass "-" as the path to read WAV bytes from stdin. Use --json to
emit one JSON object per chunk; useful for streaming backends and
for shell pipelines that want to keep partial structure.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTranscribe(cmd, cfg, args[0], jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"emit one JSON object per transcription chunk")
	return cmd
}

func runTranscribe(cmd *cobra.Command, cfg *config.Config, path string, jsonOut bool) error {
	f, err := openInputFile(path)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}
	defer f.Close()

	tx, err := daemon.NewTranscriber(cfg.Transcription)
	if err != nil {
		return fmt.Errorf("transcribe: build transcriber: %w", err)
	}
	defer closeIfCloser(tx)

	chunks, err := tx.Transcribe(cmd.Context(), f)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	out := cmd.OutOrStdout()
	enc := json.NewEncoder(out)
	wroteAny := false
	for chunk := range chunks {
		if chunk.Err != nil {
			return fmt.Errorf("transcribe: %w", chunk.Err)
		}
		if jsonOut {
			if err := enc.Encode(chunk); err != nil {
				return fmt.Errorf("transcribe: encode: %w", err)
			}
			wroteAny = true
			continue
		}
		if chunk.Text != "" {
			if _, err := io.WriteString(out, chunk.Text); err != nil {
				return fmt.Errorf("transcribe: write: %w", err)
			}
			wroteAny = true
		}
	}
	if !jsonOut && wroteAny {
		// Always finish text mode with a trailing newline so shell
		// pipelines see one record per invocation. JSON mode already
		// emits newlines via json.Encoder.
		if _, err := io.WriteString(out, "\n"); err != nil {
			return fmt.Errorf("transcribe: write: %w", err)
		}
	}
	return nil
}
