package fallback

import (
	"context"
	"errors"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
)

// Transformer is a transform.Transformer decorator that runs Primary
// first and falls back to Fallback on failure. See the package doc
// for the exact semantics.
//
// Both fields are required; New is the preferred constructor. OnError
// is optional — nil means "failures are still retried through the
// fallback, but no user-visible notification is raised".
type Transformer struct {
	Primary  transform.Transformer
	Fallback transform.Transformer
	OnError  func(error)
}

// New constructs a Transformer and validates that both the primary
// and fallback transformers are non-nil. Passing a nil OnError is
// allowed — the caller can wire a notification later.
func New(primary, fallback transform.Transformer, onError func(error)) (*Transformer, error) {
	if primary == nil {
		return nil, errors.New("fallback: primary transformer is required")
	}
	if fallback == nil {
		return nil, errors.New("fallback: fallback transformer is required")
	}
	return &Transformer{Primary: primary, Fallback: fallback, OnError: onError}, nil
}

// Transform drains the input into a slice, runs it through Primary,
// and on primary failure replays the buffered slice through
// Fallback. See the package doc for the full decision tree.
func (t *Transformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	buffered, upstream, ok := drain(ctx, in)
	if !ok {
		// Context cancelled while draining the input.
		out := make(chan transcribe.TranscriptChunk)
		close(out)
		return out, ctx.Err()
	}
	if upstream != nil {
		// Upstream error: propagate directly without running either
		// transformer. Transcription failures are not a transform
		// fallback concern.
		out := make(chan transcribe.TranscriptChunk, 1)
		out <- *upstream
		close(out)
		return out, nil
	}

	// Try the primary.
	primaryOut, err := t.Primary.Transform(ctx, replay(buffered))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return primaryOut, err
		}
		t.notify(err)
		return t.Fallback.Transform(ctx, replay(buffered))
	}

	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		var ok bool
		ok = t.forwardPrimary(ctx, primaryOut, out, buffered)
		if !ok {
			return
		}
	}()
	return out, nil
}

// forwardPrimary copies the primary stream through to out. On the
// primary's error chunk, OnError fires and the buffered input is
// replayed through the fallback. Returns true on completion (success
// or fallback), false on ctx cancellation.
//
// Note: primary emission is staged into a slice and replayed only
// after the primary stream terminates cleanly. That way a primary
// error chunk mid-stream triggers a clean fallback instead of a
// half-transformed, half-raw output.
func (t *Transformer) forwardPrimary(
	ctx context.Context,
	primaryOut <-chan transcribe.TranscriptChunk,
	out chan<- transcribe.TranscriptChunk,
	buffered []transcribe.TranscriptChunk,
) bool {
	var staged []transcribe.TranscriptChunk
	var primaryErr error
	for {
		select {
		case <-ctx.Done():
			return false
		case chunk, open := <-primaryOut:
			if !open {
				// Primary finished without error: drain staged
				// chunks to the caller.
				for _, c := range staged {
					select {
					case <-ctx.Done():
						return false
					case out <- c:
					}
				}
				return true
			}
			if chunk.Err != nil {
				primaryErr = chunk.Err
				// Drain the rest of primaryOut so its goroutine
				// can terminate cleanly, but discard what comes
				// next — partial output is not mixed with fallback
				// output.
				drainRemaining(ctx, primaryOut)
				break
			}
			staged = append(staged, chunk)
			continue
		}
		break
	}

	// Primary failed mid-stream. Run the fallback.
	t.notify(primaryErr)
	fbOut, err := t.Fallback.Transform(ctx, replay(buffered))
	if err != nil {
		select {
		case <-ctx.Done():
			return false
		case out <- transcribe.TranscriptChunk{IsFinal: true, Err: err}:
			return true
		}
	}
	for {
		select {
		case <-ctx.Done():
			return false
		case chunk, open := <-fbOut:
			if !open {
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case out <- chunk:
			}
		}
	}
}

// drainRemaining consumes whatever is still sitting in the primary
// channel so the backend goroutine can exit. Errors are intentionally
// discarded — we already committed to fallback.
func drainRemaining(ctx context.Context, ch <-chan transcribe.TranscriptChunk) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, open := <-ch:
			if !open {
				return
			}
		}
	}
}

// notify calls OnError if set. Swallowing a nil OnError keeps the
// decorator usable as a pure "always retry through fallback"
// wrapper.
func (t *Transformer) notify(err error) {
	if t.OnError != nil {
		t.OnError(err)
	}
}

// drain reads the full input channel into a slice. The second return
// is a non-nil *chunk when an upstream error chunk was observed
// (indicating the caller should propagate it rather than running
// either transformer). The third return is false when ctx was
// cancelled while draining.
func drain(ctx context.Context, in <-chan transcribe.TranscriptChunk) ([]transcribe.TranscriptChunk, *transcribe.TranscriptChunk, bool) {
	var buffered []transcribe.TranscriptChunk
	for {
		select {
		case <-ctx.Done():
			return nil, nil, false
		case chunk, open := <-in:
			if !open {
				return buffered, nil, true
			}
			if chunk.Err != nil {
				c := chunk
				return buffered, &c, true
			}
			buffered = append(buffered, chunk)
		}
	}
}

// replay turns a buffered slice back into a closed channel. The
// returned channel is fully drained on read — no goroutine spin.
func replay(buffered []transcribe.TranscriptChunk) <-chan transcribe.TranscriptChunk {
	ch := make(chan transcribe.TranscriptChunk, len(buffered))
	for _, c := range buffered {
		ch <- c
	}
	close(ch)
	return ch
}
