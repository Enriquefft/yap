// Package engine owns the core record → transcribe → transform →
// paste pipeline. It is platform-agnostic: all OS-specific behavior
// is injected via the platform interfaces, and the transcription and
// transform stages come from pkg/yap. The engine runs for one
// recording session at a time, invoked by either daemon mode or CLI
// one-shot mode.
package engine

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/passthrough"
)

// ChimeSource is a function that returns a WAV reader for a chime
// sound. Using a function (rather than io.Reader directly) allows
// lazy loading and re-use across multiple recording sessions.
type ChimeSource func() (io.Reader, error)

// Engine owns the record → transcribe → transform → paste pipeline.
// All dependencies are injected at construction time. The engine
// does not touch transcription credentials: the Transcriber is
// constructed by the caller with its credentials already baked in.
type Engine struct {
	recorder    platform.Recorder
	chime       platform.ChimePlayer
	paster      platform.Paster
	notifier    platform.Notifier
	transcriber transcribe.Transcriber
	transformer transform.Transformer
}

// New creates an Engine with all dependencies injected. recorder must
// be created for this specific session (device name already applied).
// transcriber owns the credentials for the chosen backend. A nil
// transformer is replaced with the passthrough transformer so the
// pipeline always has a non-nil stage between Transcribe and Paste.
func New(
	recorder platform.Recorder,
	chime platform.ChimePlayer,
	paster platform.Paster,
	notifier platform.Notifier,
	transcriber transcribe.Transcriber,
	transformer transform.Transformer,
) *Engine {
	if transformer == nil {
		transformer = passthrough.New()
	}
	return &Engine{
		recorder:    recorder,
		chime:       chime,
		paster:      paster,
		notifier:    notifier,
		transcriber: transcriber,
		transformer: transformer,
	}
}

// RecordAndPaste runs the full pipeline for one recording session:
// record (until recCtx cancelled) → encode → transcribe → transform
// → paste.
//
// daemonCtx is the long-lived daemon context used for transcription
// and paste. recCtx is the short-lived recording context (cancelled
// when the user releases the hotkey or when the timeout fires).
// Since recCtx is already cancelled by the time we transcribe, we
// must use daemonCtx for the API call.
//
// timeoutSec controls when the warning chime fires (10s before
// timeout, minimum 1s).
//
// Chime sources are passed as functions so the engine does not
// import the assets package directly — keeping it platform-agnostic
// and independently testable.
//
// This method blocks until the full pipeline completes (or fails).
// It does NOT return errors — all errors are surfaced via the
// Notifier. The caller (daemon or CLI) should run this in a
// goroutine if it needs to remain responsive.
func (e *Engine) RecordAndPaste(
	daemonCtx context.Context,
	recCtx context.Context,
	timeoutSec int,
	startChime, stopChime, warningChime ChimeSource,
) {
	// Play start chime immediately.
	if startChime != nil {
		if r, err := startChime(); err == nil {
			e.chime.Play(r)
		}
	}

	// Schedule warning chime: 10s before timeout, minimum 1s into
	// recording.
	warningSec := timeoutSec - 10
	if warningSec < 1 {
		warningSec = 1
	}
	warningTimer := time.AfterFunc(time.Duration(warningSec)*time.Second, func() {
		if warningChime != nil {
			if r, err := warningChime(); err == nil {
				e.chime.Play(r)
			}
		}
	})
	defer warningTimer.Stop()

	// Record until recCtx is cancelled (hotkey release, timeout, or
	// toggle).
	if err := e.recorder.Start(recCtx); err != nil &&
		err != context.Canceled && err != context.DeadlineExceeded {
		e.notifier.Notify("audio device error", err.Error())
		return
	}

	// Play stop chime after recording ends.
	if stopChime != nil {
		if r, err := stopChime(); err == nil {
			e.chime.Play(r)
		}
	}

	// Encode to WAV bytes.
	wavData, err := e.recorder.Encode()
	if err != nil {
		e.notifier.Notify("audio encode error", err.Error())
		return
	}

	// Transcribe using the daemon context (recCtx is already
	// cancelled). The channel collect pattern is Phase-3 batch
	// compatibility; Phase 5 rewrites the engine as a true
	// streaming pipeline.
	chunks, err := e.transcriber.Transcribe(daemonCtx, bytes.NewReader(wavData))
	if err != nil {
		e.notifier.Notify("transcription failed", err.Error())
		return
	}
	transformed, err := e.transformer.Transform(daemonCtx, chunks)
	if err != nil {
		e.notifier.Notify("transcription failed", err.Error())
		return
	}
	var sb strings.Builder
	for chunk := range transformed {
		if chunk.Err != nil {
			e.notifier.Notify("transcription failed", chunk.Err.Error())
			return
		}
		sb.WriteString(chunk.Text)
	}
	if err := daemonCtx.Err(); err != nil {
		e.notifier.Notify("transcription failed", err.Error())
		return
	}

	// Paste at cursor (clipboard save/restore is handled by the
	// Paster).
	if err := e.paster.Paste(sb.String()); err != nil {
		// Paste errors are best-effort; text remains in clipboard.
		// Paster implementations log internally when needed.
		return
	}
}
