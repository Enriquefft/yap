package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/assets"
	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/internal/engine"
	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/spf13/cobra"
)

// recordOptions bundles the per-invocation flag overrides for `yap
// record`. Keeping them in a struct lets the cobra command and the
// underlying runRecord helper stay testable independently.
type recordOptions struct {
	forceTransform bool
	out            string
	device         string
	maxDur         int
}

// newRecordCmd builds the `yap record` cobra command. The command
// runs a single record → transcribe → transform → inject pipeline
// without going through the daemon. It is the canonical "no-hotkey,
// no-daemon" debug command and the recommended way to script yap
// from another process.
func newRecordCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	var opts recordOptions
	cmd := &cobra.Command{
		Use:   "record",
		Short: "one-shot record, transcribe, then inject",
		Long: `record runs a single recording cycle without the daemon.

Recording stops on SIGINT, SIGTERM, the configured max_duration
timeout, or SIGUSR1 (which 'yap toggle' sends to a running record
process to end the recording cleanly so the captured audio still
flows through transcribe and inject).

Use --transform to enable LLM cleanup for this invocation only.
Use --out=text to print the transcription to stdout instead of
injecting it at the cursor.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecord(cmd.Context(), cfg, p, opts, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&opts.forceTransform, "transform", false,
		"force-enable transform for this invocation")
	cmd.Flags().StringVar(&opts.out, "out", "",
		`output mode: "" (inject at cursor) or "text" (print to stdout)`)
	cmd.Flags().StringVar(&opts.device, "device", "",
		"audio capture device (overrides general.audio_device)")
	cmd.Flags().IntVar(&opts.maxDur, "max-duration", 0,
		"maximum recording length in seconds (overrides general.max_duration)")
	return cmd
}

// runRecord executes a single record-transcribe-inject cycle. The
// returned error is non-nil only on real failures: SIGINT, SIGTERM,
// SIGUSR1, and the timeout expiry are all "the user wanted to stop"
// and exit cleanly.
func runRecord(parent context.Context, cfg *config.Config, p platform.Platform, opts recordOptions, stdout io.Writer) error {
	if opts.out != "" && opts.out != "text" {
		return fmt.Errorf("record: invalid --out value %q (expected \"\" or \"text\")", opts.out)
	}

	// Resolve effective config from flag overrides. We copy the
	// caller's Config so flag overrides do not bleed into other
	// commands inside the same root invocation.
	eff := *cfg
	if opts.device != "" {
		eff.General.AudioDevice = opts.device
	}
	if opts.maxDur > 0 {
		eff.General.MaxDuration = opts.maxDur
	}
	if opts.forceTransform {
		eff.Transform.Enabled = true
	}

	rec, err := p.NewRecorder(eff.General.AudioDevice)
	if err != nil {
		return fmt.Errorf("record: init audio: %w", err)
	}
	defer rec.Close()

	transcriber, err := daemon.NewTranscriber(eff.Transcription)
	if err != nil {
		return fmt.Errorf("record: build transcriber: %w", err)
	}
	defer closeIfCloser(transcriber)

	// Phase 8: wrap the configured transform backend in a fallback
	// decorator so `yap record --transform` degrades gracefully when
	// the backend is unreachable. The platform notifier is the same
	// one the daemon uses, so the user sees the same toast.
	transformer, err := daemon.NewTransformerWithFallback(eff.Transform, p.Notifier)
	if err != nil {
		return fmt.Errorf("record: build transformer: %w", err)
	}
	defer closeIfCloser(transformer)

	// The injector depends on output mode. text mode prints to the
	// caller-supplied stdout instead of touching the focused window.
	var injector inject.Injector
	if opts.out == "text" {
		injector = newStdoutInjector(stdout)
	} else {
		injector, err = p.NewInjector(daemon.InjectionOptionsFromConfig(eff.Injection))
		if err != nil {
			return fmt.Errorf("record: build injector: %w", err)
		}
	}

	eng, err := engine.New(rec, p.Chime, transcriber, transformer, injector, slog.Default())
	if err != nil {
		return fmt.Errorf("record: engine init: %w", err)
	}

	// Track the record process via its own PID file so `yap stop`
	// and `yap toggle` can target it without going through the
	// daemon's IPC socket.
	if err := writeRecordPID(); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	defer removeRecordPID()

	// Two contexts:
	//
	//   - ctx: shared by every pipeline stage. Cancelled on
	//     SIGINT/SIGTERM so a hard stop tears down the whole pipeline.
	//
	//   - recCtx: only the recorder uses this. Cancelling it ends
	//     the recording but lets transcribe + inject finish on the
	//     captured audio. SIGUSR1 (sent by `yap toggle`) cancels
	//     only recCtx, the same way the daemon's hotkey-release
	//     handler does.
	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	timeout := eff.General.MaxDuration
	if timeout <= 0 {
		timeout = 60
	}
	recCtx, recCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer recCancel()

	sigUsr := make(chan os.Signal, 1)
	signal.Notify(sigUsr, syscall.SIGUSR1)
	defer signal.Stop(sigUsr)
	go func() {
		select {
		case <-sigUsr:
			recCancel()
		case <-recCtx.Done():
		}
	}()

	runErr := eng.Run(ctx, engine.RunOptions{
		RecordCtx:      recCtx,
		StartChime:     assets.StartChime,
		StopChime:      assets.StopChime,
		WarningChime:   assets.WarningChime,
		TimeoutSec:     timeout,
		StreamPartials: eff.General.StreamPartials,
	})
	if runErr == nil {
		return nil
	}
	// SIGINT/SIGTERM cancel ctx (and therefore recCtx); SIGUSR1
	// cancels only recCtx; the timeout fires recCtx.DeadlineExceeded.
	// All four are "user asked to stop, not a failure".
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return nil
	}
	return runErr
}

// stdoutInjector is the inject.Injector implementation used by
// `yap record --out=text`. It writes the inbound transcript chunks
// to the supplied writer instead of touching the focused window.
//
// The Inject path writes the literal text plus a trailing newline so
// shell pipelines see one line per record invocation. InjectStream
// writes every chunk as it arrives but defers the trailing newline
// until the channel closes; if any chunk carries an error the stream
// terminates with that error.
type stdoutInjector struct {
	w io.Writer
}

func newStdoutInjector(w io.Writer) inject.Injector {
	if w == nil {
		w = os.Stdout
	}
	return &stdoutInjector{w: w}
}

func (s *stdoutInjector) Inject(ctx context.Context, text string) error {
	_, err := fmt.Fprintln(s.w, text)
	return err
}

func (s *stdoutInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	wrote := false
	for {
		select {
		case <-ctx.Done():
			if wrote {
				_, _ = fmt.Fprintln(s.w)
			}
			return ctx.Err()
		case chunk, ok := <-in:
			if !ok {
				if wrote {
					_, _ = fmt.Fprintln(s.w)
				}
				return nil
			}
			if chunk.Err != nil {
				return chunk.Err
			}
			if chunk.Text != "" {
				if _, err := fmt.Fprint(s.w, chunk.Text); err != nil {
					return err
				}
				wrote = true
			}
		}
	}
}

// Compile-time assertion that stdoutInjector satisfies inject.Injector.
var _ inject.Injector = (*stdoutInjector)(nil)
