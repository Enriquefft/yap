// Package exec provides an output handler that pipes transcribed text
// to an external command via stdin. It implements [inject.Injector] so
// the engine can use it as a drop-in replacement for the default
// injection pipeline.
//
// The handler runs the command directly (no shell) with the transcript
// on stdin. The command is resolved via PATH at delivery time — a
// missing binary surfaces as a clear error, not a silent no-op.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// Handler pipes transcript text to an external command via stdin.
// It satisfies [inject.Injector] so the engine can swap it in place
// of the default injection pipeline via [engine.RunOptions.OutputOverride].
type Handler struct {
	command string
	logger  *slog.Logger
}

// New creates an exec handler for the given command name (a single
// binary resolved via PATH — no arguments, no shell). The command is
// looked up at delivery time. logger may be nil (replaced with a
// discard logger).
func New(command string, logger *slog.Logger) (*Handler, error) {
	if command == "" {
		return nil, errors.New("exec: command is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{command: command, logger: logger}, nil
}

// Inject pipes text to the command's stdin and waits for it to exit.
// The command receives the full transcript as a single stdin payload.
func (h *Handler) Inject(ctx context.Context, text string) error {
	path, err := exec.LookPath(h.command)
	if err != nil {
		return fmt.Errorf("exec: command %q not found: %w", h.command, err)
	}

	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout = io.Discard

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	h.logger.InfoContext(ctx, "exec: delivering transcript",
		"command", h.command,
		"bytes", len(text),
	)

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return fmt.Errorf("exec: %s: %w\nstderr: %s", h.command, err, stderrStr)
		}
		return fmt.Errorf("exec: %s: %w", h.command, err)
	}

	h.logger.InfoContext(ctx, "exec: delivery complete", "command", h.command)
	return nil
}

// flushTimeout is the deadline for delivering a partial transcript
// when the pipeline context is cancelled or a chunk error arrives.
const flushTimeout = 10 * time.Second

// InjectStream collects all transcript chunks, then pipes the
// accumulated text to the command via [Inject]. Partial chunks are
// concatenated — the command always receives the full transcript.
func (h *Handler) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			// Flush whatever we have with a fresh context so the
			// command still gets the partial transcript.
			if sb.Len() > 0 {
				flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
				err := h.Inject(flushCtx, sb.String())
				cancel()
				return err
			}
			return ctx.Err()
		case chunk, ok := <-in:
			if !ok {
				if sb.Len() == 0 {
					h.logger.WarnContext(ctx, "exec: empty transcript, skipping command")
					return nil
				}
				// Channel closed normally. If ctx was also cancelled
				// concurrently (select picked this branch randomly),
				// use a fresh context so the command isn't killed
				// immediately.
				deliverCtx := ctx
				if ctx.Err() != nil {
					var cancel context.CancelFunc
					deliverCtx, cancel = context.WithTimeout(context.Background(), flushTimeout)
					defer cancel()
				}
				return h.Inject(deliverCtx, sb.String())
			}
			if chunk.Err != nil {
				// Flush accumulated text before surfacing the error —
				// partial transcripts are still valuable.
				if sb.Len() > 0 {
					flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
					flushErr := h.Inject(flushCtx, sb.String())
					cancel()
					if flushErr != nil {
						h.logger.WarnContext(ctx, "exec: flush before error failed", "error", flushErr)
					}
				}
				return fmt.Errorf("exec: transcription error: %w", chunk.Err)
			}
			sb.WriteString(chunk.Text)
		}
	}
}
