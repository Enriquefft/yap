package fallback_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/Enriquefft/yap/pkg/yap/transform/fallback"
)

// stubTransformer is a test double for transform.Transformer. It
// emits a fixed list of chunks (plus an optional terminal error
// chunk) or returns a factory error from Transform.
type stubTransformer struct {
	name        string
	emit        []transcribe.TranscriptChunk
	factoryErr  error
	calls       int32
	block       chan struct{}
	lastInput   []transcribe.TranscriptChunk
	captureDone chan struct{}
}

func (s *stubTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk, _ transform.Options) (<-chan transcribe.TranscriptChunk, error) {
	atomic.AddInt32(&s.calls, 1)
	// Drain and capture the input.
	var input []transcribe.TranscriptChunk
	for c := range in {
		input = append(input, c)
	}
	s.lastInput = input
	if s.captureDone != nil {
		close(s.captureDone)
		s.captureDone = nil
	}
	if s.factoryErr != nil {
		out := make(chan transcribe.TranscriptChunk)
		close(out)
		return out, s.factoryErr
	}
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		for _, c := range s.emit {
			if s.block != nil {
				select {
				case <-ctx.Done():
					return
				case <-s.block:
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
		}
	}()
	return out, nil
}

func inputChunks(chunks ...transcribe.TranscriptChunk) <-chan transcribe.TranscriptChunk {
	ch := make(chan transcribe.TranscriptChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func drain(out <-chan transcribe.TranscriptChunk) []transcribe.TranscriptChunk {
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	return got
}

// echoTransformer is a passthrough-style stub that emits the inputs
// verbatim. It stands in for the fallback path in tests that want to
// see the buffered slice replayed.
type echoTransformer struct {
	calls int32
}

func (e *echoTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk, _ transform.Options) (<-chan transcribe.TranscriptChunk, error) {
	atomic.AddInt32(&e.calls, 1)
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		for c := range in {
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
		}
	}()
	return out, nil
}

func TestNew_RejectsNil(t *testing.T) {
	if _, err := fallback.New(nil, &echoTransformer{}, nil); err == nil {
		t.Error("expected error on nil primary")
	}
	if _, err := fallback.New(&echoTransformer{}, nil, nil); err == nil {
		t.Error("expected error on nil fallback")
	}
}

func TestTransform_PrimarySuccess_NoFallback(t *testing.T) {
	primary := &stubTransformer{
		name: "primary",
		emit: []transcribe.TranscriptChunk{
			{Text: "hello"},
			{Text: " world", IsFinal: true},
		},
	}
	fb := &echoTransformer{}
	var onErrCalls int32
	fbt, _ := fallback.New(primary, fb, func(error) { atomic.AddInt32(&onErrCalls, 1) })

	out, err := fbt.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "raw"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 2 || got[0].Text != "hello" || got[1].Text != " world" {
		t.Errorf("got = %+v, want primary chunks", got)
	}
	if atomic.LoadInt32(&fb.calls) != 0 {
		t.Errorf("fallback calls = %d, want 0", atomic.LoadInt32(&fb.calls))
	}
	if atomic.LoadInt32(&onErrCalls) != 0 {
		t.Errorf("OnError calls = %d, want 0", atomic.LoadInt32(&onErrCalls))
	}
}

func TestTransform_PrimaryFactoryError_FallbackTakesOver(t *testing.T) {
	primary := &stubTransformer{factoryErr: errors.New("primary factory boom")}
	fb := &echoTransformer{}
	var onErrCalls int32
	var capturedErr error
	fbt, _ := fallback.New(primary, fb, func(err error) {
		atomic.AddInt32(&onErrCalls, 1)
		capturedErr = err
	})

	raw := transcribe.TranscriptChunk{Text: "raw", IsFinal: true}
	out, err := fbt.Transform(context.Background(), inputChunks(raw), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || got[0].Text != "raw" || !got[0].IsFinal {
		t.Errorf("got = %+v, want buffered replay via fallback", got)
	}
	if atomic.LoadInt32(&fb.calls) != 1 {
		t.Errorf("fallback calls = %d, want 1", atomic.LoadInt32(&fb.calls))
	}
	if atomic.LoadInt32(&onErrCalls) != 1 {
		t.Errorf("OnError calls = %d, want 1", atomic.LoadInt32(&onErrCalls))
	}
	if capturedErr == nil || capturedErr.Error() != "primary factory boom" {
		t.Errorf("captured err = %v", capturedErr)
	}
}

func TestTransform_PrimaryErrorChunk_FallbackTakesOver(t *testing.T) {
	primary := &stubTransformer{
		emit: []transcribe.TranscriptChunk{
			// A partially-transformed chunk followed by a terminal
			// error. The fallback must REPLACE the partial output
			// entirely — we never mix transformed + raw.
			{Text: "partial"},
			{IsFinal: true, Err: errors.New("mid-stream boom")},
		},
	}
	fb := &echoTransformer{}
	var onErrCalls int32
	fbt, _ := fallback.New(primary, fb, func(error) { atomic.AddInt32(&onErrCalls, 1) })

	raw1 := transcribe.TranscriptChunk{Text: "raw-a"}
	raw2 := transcribe.TranscriptChunk{Text: "raw-b", IsFinal: true}
	out, err := fbt.Transform(context.Background(), inputChunks(raw1, raw2), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 2 || got[0].Text != "raw-a" || got[1].Text != "raw-b" {
		t.Errorf("got = %+v, want exactly the buffered raw inputs (no partial 'partial')", got)
	}
	if atomic.LoadInt32(&fb.calls) != 1 {
		t.Errorf("fallback calls = %d, want 1", atomic.LoadInt32(&fb.calls))
	}
	if atomic.LoadInt32(&onErrCalls) != 1 {
		t.Errorf("OnError calls = %d, want 1", atomic.LoadInt32(&onErrCalls))
	}
}

func TestTransform_UpstreamError_NeitherRuns(t *testing.T) {
	primary := &stubTransformer{}
	fb := &echoTransformer{}
	var onErrCalls int32
	fbt, _ := fallback.New(primary, fb, func(error) { atomic.AddInt32(&onErrCalls, 1) })

	sentinel := errors.New("transcribe boom")
	out, err := fbt.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Err: sentinel, IsFinal: true},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || !errors.Is(got[0].Err, sentinel) {
		t.Errorf("got = %+v, want single upstream-error chunk", got)
	}
	if atomic.LoadInt32(&primary.calls) != 0 {
		t.Errorf("primary calls = %d, want 0", atomic.LoadInt32(&primary.calls))
	}
	if atomic.LoadInt32(&fb.calls) != 0 {
		t.Errorf("fallback calls = %d, want 0", atomic.LoadInt32(&fb.calls))
	}
	if atomic.LoadInt32(&onErrCalls) != 0 {
		t.Errorf("OnError calls = %d, want 0", atomic.LoadInt32(&onErrCalls))
	}
}

func TestTransform_OnErrorCalledExactlyOnce(t *testing.T) {
	// Two ways to fail: factory error AND error chunk. Each path
	// must fire OnError exactly once.
	primary := &stubTransformer{
		emit: []transcribe.TranscriptChunk{
			{IsFinal: true, Err: errors.New("boom")},
		},
	}
	fb := &echoTransformer{}
	var onErrCalls int32
	fbt, _ := fallback.New(primary, fb, func(error) { atomic.AddInt32(&onErrCalls, 1) })

	raw := transcribe.TranscriptChunk{Text: "raw"}
	out, err := fbt.Transform(context.Background(), inputChunks(raw), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	drain(out)
	if got := atomic.LoadInt32(&onErrCalls); got != 1 {
		t.Errorf("OnError calls = %d, want 1", got)
	}
}

func TestTransform_CancelledBeforePrimary_ReturnsErr(t *testing.T) {
	primary := &stubTransformer{}
	fb := &echoTransformer{}
	fbt, _ := fallback.New(primary, fb, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// Build an input channel that blocks forever so drain loops.
	in := make(chan transcribe.TranscriptChunk)

	done := make(chan error, 1)
	go func() {
		_, err := fbt.Transform(ctx, in, transform.Options{})
		done <- err
	}()
	// Let drain spin once.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transform did not return after cancel")
	}
	if atomic.LoadInt32(&primary.calls) != 0 {
		t.Errorf("primary calls = %d, want 0", atomic.LoadInt32(&primary.calls))
	}
	if atomic.LoadInt32(&fb.calls) != 0 {
		t.Errorf("fallback calls = %d, want 0", atomic.LoadInt32(&fb.calls))
	}
}

func TestTransform_NilOnError_SilentlyRetries(t *testing.T) {
	primary := &stubTransformer{factoryErr: errors.New("boom")}
	fb := &echoTransformer{}
	fbt, _ := fallback.New(primary, fb, nil)

	out, err := fbt.Transform(context.Background(), inputChunks(
		transcribe.TranscriptChunk{Text: "raw"},
	), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	got := drain(out)
	if len(got) != 1 || got[0].Text != "raw" {
		t.Errorf("got = %+v, want buffered raw", got)
	}
}

// TestTransform_CancelledDuringPrimary_NoFallback asserts that
// cancelling the context while the primary is mid-stream closes the
// output without invoking the fallback. The streaming contract says
// a closed channel on a cancelled context is the terminal signal —
// callers observe ctx.Err themselves, they do not see an error chunk
// from the fallback.
func TestTransform_CancelledDuringPrimary_NoFallback(t *testing.T) {
	unblock := make(chan struct{})
	primary := &stubTransformer{
		emit: []transcribe.TranscriptChunk{
			{Text: "first"},
			{Text: "second"}, // blocked until unblock closes
		},
		block: unblock,
	}
	fb := &echoTransformer{}
	fbt, _ := fallback.New(primary, fb, nil)

	ctx, cancel := context.WithCancel(context.Background())
	out, err := fbt.Transform(ctx, inputChunks(transcribe.TranscriptChunk{Text: "raw"}), transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}

	// Let the first chunk land in the staged buffer (it will be
	// blocked by stubTransformer on the second send) then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	close(unblock)

	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("output not closed after ctx cancel")
	}
	if got := atomic.LoadInt32(&fb.calls); got != 0 {
		t.Errorf("fallback calls = %d, want 0 (ctx cancel must not trigger fallback)", got)
	}
}

// Ensure the decorator still satisfies transform.Transformer.
func TestTransform_InterfaceSatisfied(t *testing.T) {
	var _ transform.Transformer = (*fallback.Transformer)(nil)
}
