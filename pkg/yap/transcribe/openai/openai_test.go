package openai_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/openai"
)

// collect drains a chunk channel and returns (text, final chunk).
func collect(t *testing.T, ch <-chan transcribe.TranscriptChunk) (string, transcribe.TranscriptChunk) {
	t.Helper()
	var sb strings.Builder
	var last transcribe.TranscriptChunk
	got := false
	for chunk := range ch {
		got = true
		last = chunk
		sb.WriteString(chunk.Text)
	}
	if !got {
		t.Fatal("no chunks delivered")
	}
	if !last.IsFinal {
		t.Errorf("last chunk IsFinal=false, want true")
	}
	return sb.String(), last
}

func TestNew_RequiresAPIURL(t *testing.T) {
	_, err := openai.New(transcribe.Config{APIKey: "k", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "APIURL") {
		t.Errorf("expected APIURL-required error, got %v", err)
	}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	_, err := openai.New(transcribe.Config{APIURL: "http://example.test", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "APIKey") {
		t.Errorf("expected APIKey-required error, got %v", err)
	}
}

func TestNew_RequiresModel(t *testing.T) {
	_, err := openai.New(transcribe.Config{APIURL: "http://example.test", APIKey: "k"})
	if err == nil || !strings.Contains(err.Error(), "Model") {
		t.Errorf("expected Model-required error, got %v", err)
	}
}

func TestPostsToConfiguredURL(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("model"); got != "gpt-whisper-test" {
			t.Errorf("model: got %q, want gpt-whisper-test", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization: got %q, want Bearer test-key", got)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "ok"}`)
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "gpt-whisper-test",
		Language:   "en",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	text, final := collect(t, ch)
	if final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if text != "ok" {
		t.Errorf("text: got %q, want ok", text)
	}
	if !hit {
		t.Error("test server was never called")
	}
}

func TestErrorResponseUnwrapsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "bad", "type": "invalid_request_error"}}`)
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *openai.APIError
	if !errors.As(final.Err, &apiErr) {
		t.Errorf("error should unwrap to *openai.APIError, got %T", final.Err)
	}
}

func TestRetry5xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"error": {"message": "upstream", "type": "server_error"}}`)
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 4 {
		t.Errorf("callCount = %d, want 4 (initial + 3 retries)", callCount)
	}
}

func TestNoRetry4xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error": {"message": "nope", "type": "permission_denied"}}`)
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry on 4xx)", callCount)
	}
}

func TestEmptyWave(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for empty audio")
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error for empty audio")
	}
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"text": "late"}`)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch, err := b.Transcribe(ctx, bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// Drain.
	for chunk := range ch {
		if chunk.Err == nil && chunk.Text != "" {
			t.Errorf("unexpected text after cancel: %q", chunk.Text)
		}
	}
}

// TestRetryBackoffCtxCancel asserts the F3 fix: cancelling the
// context while the retry backoff is sleeping returns within a tight
// deadline (well under the full backoff sequence). The server returns
// a permanent 5xx so the backend would otherwise sleep ~3.5s in total
// across the three retry attempts. We cancel after ~100ms and assert
// the call returns within ~750ms — proving sleepCtx honors ctx.
func TestRetryBackoffCtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "boom", "type": "server_error"}}`)
	}))
	defer srv.Close()

	b, err := openai.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("openai.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := b.Transcribe(ctx, bytes.NewReader([]byte("wav")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	// Cancel after the first 5xx response lands and the backoff
	// sleep begins. 100ms is enough for the first POST to round-trip
	// against the local httptest server (typical: <10ms) and for the
	// backend to enter sleepCtx.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("Transcribe did not return within 750ms of cancel — sleepCtx ignoring ctx?")
	}
	elapsed := time.Since(start)
	if elapsed > 600*time.Millisecond {
		t.Errorf("elapsed = %v, want < 600ms (sleepCtx must short-circuit on ctx cancel)", elapsed)
	}
}

func TestRegistryHasOpenAI(t *testing.T) {
	factory, err := transcribe.Get("openai")
	if err != nil {
		t.Fatalf("transcribe.Get(openai): %v", err)
	}
	tr, err := factory(transcribe.Config{
		APIURL: "http://example.test",
		APIKey: "k",
		Model:  "m",
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if tr == nil {
		t.Fatal("factory returned nil transcriber")
	}
}
