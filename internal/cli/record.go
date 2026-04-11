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
	resolve        bool
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
injecting it at the cursor.

Use --resolve to run the full record+transcribe pipeline but skip
injection entirely. Instead of delivering the transcribed text, the
command reports the StrategyDecision the inject layer would have
acted on. Useful for debugging "which strategy would record's text
have gone through?" without mutating any external state. --resolve
wins over --out=text: if both are set, the decision is printed and
the transcription is discarded.`,
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
	cmd.Flags().BoolVar(&opts.resolve, "resolve", false,
		"run the full pipeline but report the StrategyDecision instead of injecting")
	return cmd
}

// runRecord executes a single record-transcribe-inject cycle. The
// returned error is non-nil only on real failures: SIGINT, SIGTERM,
// SIGUSR1, and the timeout expiry are all "the user wanted to stop"
// and exit cleanly.
func runRecord(parent context.Context, cfg *config.Config, p platform.Platform, opts recordOptions, stdout io.Writer) error {
	if opts.out != "" && opts.out != "text" {
		return fmt.Errorf("record: validate: invalid --out value %q (expected \"\" or \"text\")", opts.out)
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
		return fmt.Errorf("record: audio init: %w", err)
	}
	defer rec.Close()

	transcriber, err := daemon.NewTranscriber(eff.Transcription)
	if err != nil {
		return fmt.Errorf("record: build transcriber: %w", err)
	}
	defer closeIfCloser(transcriber, "transcriber")

	// Phase 8: wrap the configured transform backend in a fallback
	// decorator so `yap record --transform` degrades gracefully when
	// the backend is unreachable. The platform notifier is the same
	// one the daemon uses, so the user sees the same toast. The
	// streamPartials flag gates the fallback wrapping: when the
	// user opted into partial injection, the buffered fallback
	// decorator would defeat that promise, so wrapping is skipped
	// (see pkg/yap/transform/fallback/doc.go).
	transformer, err := daemon.NewTransformerWithFallback(
		eff.Transform,
		p.Notifier,
		eff.General.StreamPartials,
	)
	if err != nil {
		return fmt.Errorf("record: build transformer: %w", err)
	}
	defer closeIfCloser(transformer, "transformer")

	// The injector depends on output mode. --resolve wins over
	// --out=text: when both are set, the full pipeline runs for
	// side effects (audio capture, transcribe) but injection is
	// replaced with a StrategyDecision render. text mode prints to
	// the caller-supplied stdout instead of touching the focused
	// window.
	var injector inject.Injector
	switch {
	case opts.resolve:
		// Build the real platform injector so we can type-assert it
		// to StrategyResolver. Failing the assertion here (before
		// audio starts) surfaces a clean error without wasting the
		// user's microphone time.
		realInj, err := p.NewInjector(daemon.InjectionOptionsFromConfig(eff.Injection))
		if err != nil {
			return fmt.Errorf("record: build injector: %w", err)
		}
		resolver, ok := realInj.(inject.StrategyResolver)
		if !ok {
			return fmt.Errorf("record: --resolve not supported by the current injector (platform does not implement StrategyResolver)")
		}
		injector = newResolveInjector(resolver, stdout)
	case opts.out == "text":
		injector = newStdoutInjector(stdout)
	default:
		injector, err = p.NewInjector(daemon.InjectionOptionsFromConfig(eff.Injection))
		if err != nil {
			return fmt.Errorf("record: build injector: %w", err)
		}
	}

	eng, err := engine.New(rec, p.Chime, transcriber, transformer, injector, slog.Default())
	if err != nil {
		return fmt.Errorf("record: engine init: %w", err)
	}

	// Track the record process via its own flock-protected PID
	// file so `yap stop` and `yap toggle` can target it without
	// going through the daemon's IPC socket. The kernel releases
	// the flock automatically on exit, so a crash can never leave
	// a stale pidfile blocking the next `yap record` invocation.
	pidHandle, err := acquireRecordPID()
	if err != nil {
		return fmt.Errorf("record: pid: %w", err)
	}
	defer func() {
		if cerr := pidHandle.Close(); cerr != nil {
			slog.Default().Warn("record pid close error", "err", cerr)
		}
	}()

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

// resolveInjector is the inject.Injector implementation used by
// `yap record --resolve`. It drains the transcription stream (so the
// full pipeline runs end-to-end and any audio/transcribe bugs still
// surface in the debug output) then calls Resolve on the underlying
// StrategyResolver instead of delivering the text. The resulting
// StrategyDecision is written to the supplied writer.
//
// Design: rather than special-casing "skip inject" inside the engine
// or the CLI, we inject a wrapper that satisfies inject.Injector and
// makes the "record pipeline → decision render" path structural. The
// engine stays unaware, the CLI stays small, and the --resolve flag
// integrates with the existing record flow (SIGUSR1, silence
// detection, context cancellation) without any special cases.
type resolveInjector struct {
	resolver inject.StrategyResolver
	w        io.Writer
}

// newResolveInjector wraps the real platform injector (exposed as a
// StrategyResolver) in a resolve-only shim. The returned value
// satisfies inject.Injector so the engine does not need to change.
func newResolveInjector(resolver inject.StrategyResolver, w io.Writer) inject.Injector {
	if w == nil {
		w = os.Stdout
	}
	return &resolveInjector{resolver: resolver, w: w}
}

// Inject runs the resolver and writes the decision. The text is
// discarded: --resolve never delivers. This path is only hit if the
// engine calls Inject directly (non-streaming). In Phase 4 the engine
// always goes through InjectStream; Inject is here for forward
// compatibility with any future engine refactor that falls back to
// the synchronous path.
func (r *resolveInjector) Inject(ctx context.Context, _ string) error {
	return r.resolveAndWrite(ctx)
}

// InjectStream drains the transcription channel so the upstream
// pipeline stays unblocked, then runs Resolve and writes the
// decision. The drained chunks are discarded — the whole point of
// --resolve is "what would the injector do with this?", not "here's
// the text".
//
// Chunk errors still propagate so a broken transcribe backend
// surfaces as a pipeline failure rather than being silently swallowed
// by the resolve path. Context cancellation propagates as ctx.Err()
// the same way the real injector reports mid-stream cancellation:
// the user cancelled, so there is no decision to render.
func (r *resolveInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk, ok := <-in:
			if !ok {
				return r.resolveAndWrite(ctx)
			}
			if chunk.Err != nil {
				return chunk.Err
			}
		}
	}
}

// resolveAndWrite invokes Resolve on the underlying resolver and
// writes the decision to w. Wrapped into its own method so Inject
// and InjectStream share exactly one code path for the render.
func (r *resolveInjector) resolveAndWrite(ctx context.Context) error {
	decision, err := r.resolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	writeStrategyDecision(r.w, decision)
	return nil
}

// Compile-time assertion that resolveInjector satisfies inject.Injector.
var _ inject.Injector = (*resolveInjector)(nil)
