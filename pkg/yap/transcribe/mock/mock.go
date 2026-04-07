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

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// Backend is the deterministic test Transcriber. Chunks is replayed
// verbatim on every call; the final element should have IsFinal=true.
// Chunks is a public field so tests can tweak individual cases
// (partial sequences, error chunks, multi-chunk streams) without
// paying for a builder API.
type Backend struct {
	Chunks []transcribe.TranscriptChunk
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
	return &Backend{Chunks: chunks}
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
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	if audio != nil {
		_, _ = io.Copy(io.Discard, audio)
	}
	out := make(chan transcribe.TranscriptChunk, len(b.Chunks))
	go func() {
		defer close(out)
		for _, c := range b.Chunks {
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
		}
	}()
	return out, nil
}
