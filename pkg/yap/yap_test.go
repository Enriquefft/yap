// This test file lives in package yap_test (external) on purpose:
// ROADMAP.md Phase 3 requires that "A separate Go program can import
// pkg/yap/transcribe/groq and transcribe a WAV". An external test
// package is the tightest in-repo simulation of that third-party
// consumer — it can only touch exported identifiers, exactly as a
// downstream library user would.
package yap_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/groq"
	"github.com/hybridz/yap/pkg/yap/transcribe/mock"
	"github.com/hybridz/yap/pkg/yap/transform"
)

// fakeGroqServer stands up a minimal Groq-compatible endpoint that
// echoes a preset body on success.
func fakeGroqServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"text": %q}`, body)
	}))
}

// TestClientTranscribesViaGroq is the canonical Phase 3 acceptance
// test: a third-party-style consumer builds a Groq backend from the
// public API, wraps it in a Client, and transcribes a WAV. If any
// exported identifier drifts, this test breaks.
func TestClientTranscribesViaGroq(t *testing.T) {
	srv := fakeGroqServer(t, "hello from groq")
	defer srv.Close()

	backend, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "whisper-large-v3-turbo",
		Language:   "en",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}

	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}

	text, err := client.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "hello from groq" {
		t.Errorf("text: got %q, want %q", text, "hello from groq")
	}
}

// TestClientTranscribeStreamExposesChunks proves the streaming entry
// point returns the raw chunk channel so callers can drive their own
// injection loop.
func TestClientTranscribeStreamExposesChunks(t *testing.T) {
	srv := fakeGroqServer(t, "streaming result")
	defer srv.Close()

	backend, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		Language:   "en",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}

	out, err := client.TranscribeStream(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("TranscribeStream: %v", err)
	}

	var sb strings.Builder
	var final transcribe.TranscriptChunk
	for chunk := range out {
		if chunk.Err != nil {
			t.Fatalf("chunk.Err: %v", chunk.Err)
		}
		sb.WriteString(chunk.Text)
		if chunk.IsFinal {
			final = chunk
		}
	}
	if sb.String() != "streaming result" {
		t.Errorf("text: got %q, want streaming result", sb.String())
	}
	if !final.IsFinal {
		t.Errorf("no final chunk observed")
	}
}

// TestClientRequiresTranscriber enforces the New() contract.
func TestClientRequiresTranscriber(t *testing.T) {
	_, err := yap.New()
	if err == nil {
		t.Fatal("yap.New with no transcriber should fail")
	}
}

// TestClientRejectsNilTransformer asserts the C15 fix: explicitly
// passing a nil Transformer to WithTransformer is a programming
// error and New surfaces it. The omission case (no WithTransformer
// at all) still defaults to passthrough — that is the documented
// behavior and the existing
// TestClientDefaultTransformerIsPassthrough test locks it down.
func TestClientRejectsNilTransformer(t *testing.T) {
	backend := mock.New()
	_, err := yap.New(yap.WithTranscriber(backend), yap.WithTransformer(nil))
	if err == nil {
		t.Fatal("expected error when WithTransformer(nil) is called explicitly")
	}
	if !strings.Contains(err.Error(), "WithTransformer") {
		t.Errorf("error should mention WithTransformer, got %v", err)
	}
}

// TestClientUsesMockBackendFromRegistry proves the registry works
// end-to-end from a third-party consumer: look up a backend by name
// and hand it to a Client via the Option API.
func TestClientUsesMockBackendFromRegistry(t *testing.T) {
	factory, err := transcribe.Get("mock")
	if err != nil {
		t.Fatalf("transcribe.Get(mock): %v", err)
	}
	tr, err := factory(transcribe.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	client, err := yap.New(yap.WithTranscriber(tr))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	text, err := client.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text == "" {
		t.Error("mock backend delivered empty text")
	}
}

// TestClientPropagatesChunkError checks that a chunk error is
// surfaced by the batch entry point.
func TestClientPropagatesChunkError(t *testing.T) {
	backend := mock.New(transcribe.TranscriptChunk{
		IsFinal: true,
		Err:     errors.New("boom"),
	})
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	_, err = client.Transcribe(context.Background(), bytes.NewReader(nil))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected chunk error to propagate, got %v", err)
	}
}

// TestClientDefaultTransformerIsPassthrough verifies the default
// transformer preserves chunks so the batch result matches the raw
// Transcriber output when no custom transformer is supplied.
func TestClientDefaultTransformerIsPassthrough(t *testing.T) {
	backend := mock.New(
		transcribe.TranscriptChunk{Text: "hello "},
		transcribe.TranscriptChunk{Text: "world", IsFinal: true},
	)
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	text, err := client.Transcribe(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text: got %q, want %q", text, "hello world")
	}
}

// TestClientCustomTransformer verifies that a user-supplied
// Transformer is invoked between Transcribe and the final text
// accumulation. Uses a tiny upper-case transformer to avoid importing
// an LLM backend into the test.
func TestClientCustomTransformer(t *testing.T) {
	backend := mock.New(
		transcribe.TranscriptChunk{Text: "hello", IsFinal: true},
	)
	up := upperTransformer{}
	client, err := yap.New(
		yap.WithTranscriber(backend),
		yap.WithTransformer(up),
	)
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	text, err := client.Transcribe(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "HELLO" {
		t.Errorf("text: got %q, want HELLO", text)
	}
}

// upperTransformer upper-cases every chunk's text. Used only to
// prove the WithTransformer hook is wired.
type upperTransformer struct{}

func (upperTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		for c := range in {
			c.Text = strings.ToUpper(c.Text)
			select {
			case <-ctx.Done():
				return
			case out <- c:
			}
		}
	}()
	return out, nil
}

// Compile-time assertions that the upperTransformer satisfies the
// public Transformer interface. This is the same shape a third-party
// consumer would use to build a custom backend.
var _ transform.Transformer = upperTransformer{}

// Compile-time assertion that *bytes.Reader (the helper used in
// every test above) satisfies io.Reader. This keeps the io import
// honest and documents the interface the public API consumes.
var _ io.Reader = (*bytes.Reader)(nil)

// TestTranscribeAllPreservesChunkMetadata exercises the C2 fix:
// TranscribeAll returns every chunk with full metadata (Language,
// IsFinal, Text) preserved, so callers that need detected language
// or per-chunk timing have a batch entry point that does not throw
// the metadata away.
func TestTranscribeAllPreservesChunkMetadata(t *testing.T) {
	chunks := []transcribe.TranscriptChunk{
		{Text: "hello ", Language: "en"},
		{Text: "world", Language: "en", IsFinal: true},
	}
	backend := mock.New(chunks...)
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	got, err := client.TranscribeAll(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("TranscribeAll: %v", err)
	}
	if len(got) != len(chunks) {
		t.Fatalf("TranscribeAll returned %d chunks, want %d", len(got), len(chunks))
	}
	if got[0].Text != "hello " || got[0].Language != "en" || got[0].IsFinal {
		t.Errorf("chunk[0] = %+v", got[0])
	}
	if got[1].Text != "world" || got[1].Language != "en" || !got[1].IsFinal {
		t.Errorf("chunk[1] = %+v", got[1])
	}
}

// TestTranscribeAllPropagatesError verifies the C2 contract for
// errors: TranscribeAll returns the chunks accumulated before the
// error plus the error itself, so callers can decide whether to
// surface partial results.
func TestTranscribeAllPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	chunks := []transcribe.TranscriptChunk{
		{Text: "first"},
		{Err: sentinel, IsFinal: true},
	}
	backend := mock.New(chunks...)
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	got, gotErr := client.TranscribeAll(context.Background(), bytes.NewReader(nil))
	if !errors.Is(gotErr, sentinel) {
		t.Errorf("TranscribeAll err = %v, want %v", gotErr, sentinel)
	}
	if len(got) != 1 || got[0].Text != "first" {
		t.Errorf("partial chunks = %+v, want one chunk with Text=first", got)
	}
}

// slowTransformer sleeps between chunks so the C14 test can cancel
// the context mid-pipeline and observe the cancellation latch
// without racing the transcriber.
type slowTransformer struct {
	delay time.Duration
}

func (s slowTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	out := make(chan transcribe.TranscriptChunk)
	go func() {
		defer close(out)
		for c := range in {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.delay):
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

// TestClientCancellationDuringTransform asserts the streaming
// contract: cancelling ctx while the transformer is mid-stream
// surfaces as a context.Canceled error from Transcribe and does not
// leak goroutines beyond a small delta. The C14 finding asks for
// this guarantee — without it, a stuck transformer could trap a
// recording forever.
func TestClientCancellationDuringTransform(t *testing.T) {
	chunks := []transcribe.TranscriptChunk{
		{Text: "first"},
		{Text: "second"},
		{Text: "third"},
		{Text: "fourth", IsFinal: true},
	}
	backend := mock.New(chunks...)
	tr := slowTransformer{delay: 200 * time.Millisecond}
	client, err := yap.New(yap.WithTranscriber(backend), yap.WithTransformer(tr))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}

	// Snapshot the goroutine count before the call so we can
	// detect a leak after.
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	doneErr := make(chan error, 1)
	go func() {
		_, err := client.Transcribe(ctx, bytes.NewReader([]byte("wav")))
		doneErr <- err
	}()

	// Let one or two chunks pass through, then cancel.
	time.Sleep(350 * time.Millisecond)
	cancel()

	select {
	case err := <-doneErr:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Transcribe err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transcribe did not return within 2s of cancel")
	}

	// Allow background goroutines to drain. We are looking for a
	// regression where the transformer goroutine wedges, not for an
	// exact count match — runtime.NumGoroutine includes the test
	// runner's own goroutines plus anything other tests left
	// behind.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if delta := runtime.NumGoroutine() - before; delta <= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if delta := runtime.NumGoroutine() - before; delta > 2 {
		t.Errorf("goroutine delta after cancel = %d, want <= 2", delta)
	}
}

// TestCtxCancellationSurfacesAsError exercises the cancellation path
// by cancelling the context before Transcribe runs.
func TestCtxCancellationSurfacesAsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"text": "late"}`)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	backend, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	client, err := yap.New(yap.WithTranscriber(backend))
	if err != nil {
		t.Fatalf("yap.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Transcribe(ctx, bytes.NewReader([]byte("wav")))
	if err == nil {
		t.Error("expected error for cancelled ctx")
	}
}
