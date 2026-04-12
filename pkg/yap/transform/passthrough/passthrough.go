// Package passthrough is the default Transformer: it forwards every
// input chunk to its output channel without modification. It is the
// identity element of the transform pipeline and is always available
// in the registry so the engine can fall back to it when the transform
// stage is disabled.
package passthrough

import (
	"context"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
)

// Transformer is the zero-configuration passthrough transformer.
type Transformer struct{}

// New constructs a new passthrough Transformer. A zero-value
// Transformer{} is equally valid, but using New makes the call site
// consistent with the rest of pkg/yap.
func New() *Transformer { return &Transformer{} }

// NewFactory adapts New into the transform.Factory signature. cfg is
// ignored because passthrough has no tunable knobs.
func NewFactory(cfg transform.Config) (transform.Transformer, error) {
	return New(), nil
}

// Transform copies chunks from in to the returned channel, respecting
// ctx cancellation. The output channel is closed when in closes or
// when ctx is cancelled.
func (*Transformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-in:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case out <- chunk:
				}
			}
		}
	}()
	return out, nil
}
