// Package mock provides a deterministic Transcriber implementation
// for use in tests. It does no network I/O, drains the supplied audio
// reader to /dev/null, and emits a caller-configurable sequence of
// chunks on the returned channel.
//
// Importing this package for side effects registers a default "mock"
// backend in the transcribe registry, enabling daemon and integration
// tests to select it via transcribe.Get("mock") without importing the
// mock package's concrete type.
package mock

import (
	"context"
	"io"
	"sync"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// Backend is the deterministic test Transcriber. The chunk sequence
// is set once via New (or the registry factory) and not exposed for
// post-construction mutation: the goroutine spawned by Transcribe
// reads from the slice, so post-Transcribe mutation would be a data
// race. Tests that need a different sequence build a new Backend.
//
// LastOptions records the Options struct passed to the most recent
// Transcribe call so tests can assert that per-call options (e.g. the
// Phase 12 hint-bundle Prompt) are threaded through the pipeline. It
// is protected by a mutex because Transcribe runs on a goroutine and
// the test harness reads it from the caller goroutine.
type Backend struct {
	chunks []transcribe.TranscriptChunk

	optsMu      sync.Mutex
	lastOptions transcribe.Options
}

// New constructs a Backend that will emit the given chunks. If no
// chunks are supplied, a single IsFinal chunk with the text
// "mock transcription" is used — the minimum signal a test needs to
// prove the pipeline wired something up.
func New(chunks ...transcribe.TranscriptChunk) *Backend {
	if len(chunks) == 0 {
		chunks = []transcribe.TranscriptChunk{
			{Text: "mock transcription", IsFinal: true},
		}
	}
	// Defensive copy so the caller can reuse / mutate their slice
	// after construction without affecting Backend behavior.
	cp := make([]transcribe.TranscriptChunk, len(chunks))
	copy(cp, chunks)
	return &Backend{chunks: cp}
}

// NewFactory adapts New into the transcribe.Factory signature for the
// registry. cfg is ignored — the mock backend is intentionally
// configurationless.
func NewFactory(cfg transcribe.Config) (transcribe.Transcriber, error) {
	return New(), nil
}

// Transcribe drains audio, then emits the configured chunks in order.
// The channel is closed when the last chunk has been delivered or
// when ctx is cancelled, whichever comes first.
//
// opts is recorded on the Backend so tests can assert that the
// pipeline forwarded the per-call Options through unchanged.
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader, opts transcribe.Options) (<-chan transcribe.TranscriptChunk, error) {
	b.optsMu.Lock()
	b.lastOptions = opts
	b.optsMu.Unlock()
	if audio != nil {
		_, _ = io.Copy(io.Discard, audio)
	}
	out := make(chan transcribe.TranscriptChunk, len(b.chunks))
	go func() {
		defer close(out)
		for _, c := range b.chunks {
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
		}
	}()
	return out, nil
}

// LastOptions returns the Options struct passed to the most recent
// Transcribe call. The zero value is returned before the first call.
func (b *Backend) LastOptions() transcribe.Options {
	b.optsMu.Lock()
	defer b.optsMu.Unlock()
	return b.lastOptions
}
