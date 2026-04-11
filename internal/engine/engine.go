// Package engine owns the streaming record → transcribe → transform →
// inject pipeline. It is a thin channel orchestrator: zero backend
// imports, zero batch-collection shims, zero credentials. The caller
// (daemon or CLI one-shot) constructs an Engine with its desired
// Transcriber, Transformer, and Injector, then calls Run(ctx, opts).
//
// The engine knows nothing about Groq, OpenAI, whisper.cpp, OSC52,
// wtype, or any other concrete backend. It speaks only the public
// pkg/yap/transcribe.Transcriber, pkg/yap/transform.Transformer, and
// pkg/yap/inject.Injector interfaces, plus the platform-level Recorder
// and ChimePlayer contracts. Notifications are owned by the caller —
// Run returns a wrapped error and the daemon (or CLI) decides whether
// to surface it via its own Notifier. Adding a new backend requires
// zero engine changes.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
)

// ChimeSource is a function that returns a WAV reader for a chime
// sound. Using a function (rather than io.Reader directly) allows
// lazy loading and re-use across multiple recording sessions.
type ChimeSource func() (io.Reader, error)

// Engine owns the record → transcribe → transform → inject pipeline.
// All dependencies are injected at construction time. The engine does
// not touch transcription credentials: the Transcriber is constructed
// by the caller with its credentials already baked in. Likewise the
// Injector is constructed by the caller with the platform-bridged
// InjectionOptions already baked in. There is no default Transformer:
// the caller must supply one explicitly (the identity transformer in
// the public library is the canonical fallback).
//
// Note: there is no Notifier here. Pipeline errors are surfaced as
// return values from Run; the caller (daemon hotkey handler or CLI
// one-shot) decides whether to surface a given error to the user via
// its own Notifier. Routing notifications through the engine would
// be a second source of truth for "did this fail" — the orchestrator
// in §1.5 of the Phase 5 plan rejected that explicitly.
type Engine struct {
	recorder    platform.Recorder
	chime       platform.ChimePlayer
	transcriber transcribe.Transcriber
	transformer transform.Transformer
	injector    inject.Injector
	logger      *slog.Logger
}

// New creates an Engine with all dependencies injected. recorder,
// transcriber, transformer, and injector are all required and a nil
// value for any of them returns an error so misconfiguration surfaces
// at construction time. logger is optional — a nil logger is replaced
// with slog.New(slog.DiscardHandler) so audit calls never panic and
// tests opt-in to capture by passing a real handler. chime may be
// nil; the engine guards every call site.
func New(
	recorder platform.Recorder,
	chime platform.ChimePlayer,
	transcriber transcribe.Transcriber,
	transformer transform.Transformer,
	injector inject.Injector,
	logger *slog.Logger,
) (*Engine, error) {
	if recorder == nil {
		return nil, errors.New("engine: Recorder is required")
	}
	if transcriber == nil {
		return nil, errors.New("engine: Transcriber is required")
	}
	if transformer == nil {
		return nil, errors.New("engine: Transformer is required")
	}
	if injector == nil {
		return nil, errors.New("engine: Injector is required")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Engine{
		recorder:    recorder,
		chime:       chime,
		transcriber: transcriber,
		transformer: transformer,
		injector:    injector,
		logger:      logger,
	}, nil
}

// RunOptions bundles the per-session knobs Run consumes. RecordCtx is
// required; the caller (daemon hotkey handler or CLI one-shot) owns
// recording cancellation by cancelling this context on hotkey
// release / silence / external timeout. The other fields are optional:
// nil chime sources are skipped, TimeoutSec ≤ 0 disables the warning
// chime, and StreamPartials defaults to false (single batched final
// chunk to the injector).
type RunOptions struct {
	// RecordCtx is the per-recording context. Cancelling it stops the
	// recorder and proceeds to transcription. Required.
	RecordCtx context.Context
	// StartChime plays immediately before recording begins.
	StartChime ChimeSource
	// StopChime plays immediately after recording ends.
	StopChime ChimeSource
	// WarningChime plays TimeoutSec - 10 seconds into the recording.
	WarningChime ChimeSource
	// TimeoutSec is the recording timeout in seconds. Used only to
	// schedule the warning chime; the actual cancellation is the
	// caller's responsibility (via RecordCtx).
	TimeoutSec int
	// StreamPartials controls whether the injector receives partial
	// transcription chunks as they arrive. When false, the engine
	// batches the entire transcription into a single IsFinal chunk
	// before handing it to the injector.
	StreamPartials bool
	// OnRecordingStop is called after the recorder stops and before
	// the stop chime plays. The daemon uses this to transition state
	// from "recording" to "processing". Optional; nil is safe.
	OnRecordingStop func()
}

// Run executes one recording → transcribe → transform → inject pipeline
// cycle. It blocks until the pipeline finishes, fails, or ctx is
// cancelled.
//
// ctx is the long-lived daemon (or CLI) context — cancelling it tears
// down every goroutine the engine spawned before Run returns.
// opts.RecordCtx is the per-session recording context: cancelling it
// stops the recorder and lets the pipeline proceed to transcription.
// The two contexts are deliberately separate so the caller can stop
// recording without aborting the in-flight transcription.
//
// Run returns the first error encountered. context.Canceled and
// context.DeadlineExceeded are returned wrapped so callers can
// inspect via errors.Is. The caller is responsible for deciding
// whether to surface a given error to the user — the engine itself
// does not call Notifier on errors. Recording, encoding, transcription,
// transformation, and injection failures are all wrapped with a
// stage-identifying prefix so the caller can log them coherently.
func (e *Engine) Run(ctx context.Context, opts RunOptions) error {
	if opts.RecordCtx == nil {
		return errors.New("engine.Run: RecordCtx is required")
	}
	started := time.Now()

	e.playChime(opts.StartChime)

	stopWarn := e.scheduleWarning(opts)
	defer stopWarn()

	if err := e.recorder.Start(opts.RecordCtx); err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		e.logger.ErrorContext(ctx, "recorder error", "error", err)
		return fmt.Errorf("record: %w", err)
	}

	if opts.OnRecordingStop != nil {
		opts.OnRecordingStop()
	}

	e.playChime(opts.StopChime)

	wav, err := e.recorder.Encode()
	if err != nil {
		e.logger.ErrorContext(ctx, "encode error", "error", err)
		return fmt.Errorf("encode: %w", err)
	}

	return e.runPipeline(ctx, wav, opts, started)
}

// runPipeline drives the channel-piped transcribe → [batch] →
// transform → inject pipeline against an already-encoded WAV blob.
// It owns a sub-context derived from the daemon ctx so a downstream
// failure tears down upstream goroutines without leaking and without
// touching the caller's ctx.
func (e *Engine) runPipeline(ctx context.Context, wav []byte, opts RunOptions, started time.Time) error {
	pipeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	transcribeChan, err := e.transcriber.Transcribe(pipeCtx, bytes.NewReader(wav))
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}

	// stream_partials = false collapses the transcript into a single
	// IsFinal chunk before the transformer sees it. The injector
	// still receives a channel — preserving the channel-based
	// invariant end to end — but it will see exactly one chunk.
	inChan := transcribeChan
	if !opts.StreamPartials {
		inChan = e.batchChunks(pipeCtx, transcribeChan)
	}

	transformChan, err := e.transformer.Transform(pipeCtx, inChan)
	if err != nil {
		return fmt.Errorf("transform: %w", err)
	}

	if err := e.injector.InjectStream(pipeCtx, transformChan); err != nil {
		// Cancellation is the normal way the daemon stops the
		// pipeline — return it as-is (still wrapped) so the caller
		// can errors.Is it without the inject prefix being load-bearing.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		e.logger.ErrorContext(ctx, "inject error", "error", err)
		return fmt.Errorf("inject: %w", err)
	}

	// If pipeCtx was cancelled mid-stream the injector may return nil
	// after committing whatever it had buffered (Phase 4 contract).
	// Surface the cancellation as the pipeline's outcome so callers
	// can distinguish "user cancelled" from "transcript was empty".
	if err := pipeCtx.Err(); err != nil {
		return err
	}

	e.logger.InfoContext(ctx, "pipeline complete",
		"duration_ms", time.Since(started).Milliseconds(),
		"stream_partials", opts.StreamPartials)
	return nil
}

// batchChunks collects every chunk from in and re-emits them as a
// single IsFinal chunk on the returned channel. It is the only place
// in the engine that accumulates transcript text — and it exists
// solely because of the explicit StreamPartials=false config flag.
//
// On a chunk with Err set, batchChunks forwards the error chunk
// downstream and returns; it never silently swallows transcription
// errors. On ctx cancellation it returns without emitting anything,
// because the consumer is already on its way down.
func (e *Engine) batchChunks(ctx context.Context, in <-chan transcribe.TranscriptChunk) <-chan transcribe.TranscriptChunk {
	out := make(chan transcribe.TranscriptChunk, 1)
	go func() {
		defer close(out)
		var sb strings.Builder
		var lang string
		var lastOffset time.Duration
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-in:
				if !ok {
					if ctx.Err() != nil {
						return
					}
					select {
					case <-ctx.Done():
					case out <- transcribe.TranscriptChunk{
						Text:     sb.String(),
						IsFinal:  true,
						Language: lang,
						Offset:   lastOffset,
					}:
					}
					return
				}
				if chunk.Err != nil {
					select {
					case <-ctx.Done():
					case out <- transcribe.TranscriptChunk{
						Err:     chunk.Err,
						IsFinal: true,
					}:
					}
					return
				}
				sb.WriteString(chunk.Text)
				if chunk.Language != "" {
					lang = chunk.Language
				}
				if chunk.Offset > lastOffset {
					lastOffset = chunk.Offset
				}
			}
		}
	}()
	return out
}

// playChime is a nil-safe chime player. The engine treats chime errors
// as best-effort — a failure here must not block transcription.
func (e *Engine) playChime(src ChimeSource) {
	if src == nil || e.chime == nil {
		return
	}
	r, err := src()
	if err != nil {
		return
	}
	e.chime.Play(r)
}

// scheduleWarning starts the warning chime timer and returns a stop
// function. When TimeoutSec ≤ 0 or WarningChime is nil the returned
// function is a no-op so the call site can defer it unconditionally.
func (e *Engine) scheduleWarning(opts RunOptions) func() {
	if opts.WarningChime == nil || opts.TimeoutSec <= 0 {
		return func() {}
	}
	warnAfter := opts.TimeoutSec - 10
	if warnAfter < 1 {
		warnAfter = 1
	}
	t := time.AfterFunc(time.Duration(warnAfter)*time.Second, func() {
		e.playChime(opts.WarningChime)
	})
	return func() { t.Stop() }
}
