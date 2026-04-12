package groq_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transcribe/groq"
)

// newTestBackend constructs a Backend pointed at the given test server
// with safe defaults. Tests that need different knobs construct the
// Config directly.
func newTestBackend(t *testing.T, srv *httptest.Server) *groq.Backend {
	t.Helper()
	b, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "whisper-large-v3-turbo",
		Language:   "en",
		Timeout:    30 * time.Second,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	return b
}

// collect drains the chunk channel and returns the accumulated text
// and the final chunk (including any error). It verifies the usual
// invariants: the channel must be closed, there must be at least one
// chunk, and the final chunk must be IsFinal=true.
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
		t.Fatal("no chunks delivered; channel closed empty")
	}
	if !last.IsFinal {
		t.Errorf("last chunk IsFinal=false, want true")
	}
	return sb.String(), last
}

func TestModelParam(t *testing.T) {
	receivedModel := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		receivedModel = r.FormValue("model")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test transcription"}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("fake wav data")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if receivedModel != "whisper-large-v3-turbo" {
		t.Errorf("got model %q, want whisper-large-v3-turbo", receivedModel)
	}
}

func TestModelParam_CustomModel(t *testing.T) {
	var receivedModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatal(err)
		}
		receivedModel = r.FormValue("model")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "ok"}`)
	}))
	defer srv.Close()

	b, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "custom-model-name",
		Language:   "en",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if receivedModel != "custom-model-name" {
		t.Errorf("got model %q, want custom-model-name", receivedModel)
	}
}

func TestAPIURLOverride_PostedHere(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "hit"}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if _, final := collect(t, ch); final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if !hit {
		t.Error("request did not reach httptest server — APIURL not honored")
	}
}

func TestMultipartForm(t *testing.T) {
	var gotFilename string
	var gotLanguage string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("multipart reader: %v", err)
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			switch part.FormName() {
			case "file":
				gotFilename = part.FileName()
			case "language":
				lang, _ := io.ReadAll(part)
				gotLanguage = string(lang)
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("fake wav data")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if _, final := collect(t, ch); final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if gotFilename != "audio.wav" {
		t.Errorf("filename: got %q, want audio.wav", gotFilename)
	}
	if gotLanguage != "en" {
		t.Errorf("language: got %q, want en", gotLanguage)
	}
}

func TestHTTPTimeout(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	b, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "whisper-large-v3-turbo",
		HTTPClient: &http.Client{Timeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Error("expected timeout error, got nil")
	}
	if requestCount < 1 {
		t.Errorf("expected at least 1 HTTP request, got %d", requestCount)
	}
}

func TestRetryClassification_4xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": {"message": "invalid API key", "type": "authentication_error"}}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if callCount != 1 {
		t.Errorf("got %d calls, want 1 (no retry on 4xx)", callCount)
	}
	if !strings.Contains(final.Err.Error(), "invalid API key") {
		t.Errorf("error should contain API message, got: %v", final.Err)
	}
}

func TestRetryClassification_5xx(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected 503 error, got nil")
	}
	if callCount != 4 {
		t.Errorf("got %d calls, want 4 (initial + 3 retries)", callCount)
	}
}

func TestRetryClassification_Timeout(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	b, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     "test-key",
		Model:      "whisper-large-v3-turbo",
		HTTPClient: &http.Client{Timeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected timeout error")
	}
	if callCount < 2 {
		t.Errorf("got %d calls, want >= 2 (timeout retried)", callCount)
	}
}

func TestAPIKey(t *testing.T) {
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	testKey := "gsk_test_api_key_12345"
	b, err := groq.New(transcribe.Config{
		APIURL:     srv.URL,
		APIKey:     testKey,
		Model:      "whisper-large-v3-turbo",
		Language:   "en",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if _, final := collect(t, ch); final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	want := "Bearer " + testKey
	if gotAuthHeader != want {
		t.Errorf("auth header: got %q, want %q", gotAuthHeader, want)
	}
}

func TestSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "hello world"}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	text, final := collect(t, ch)
	if final.Err != nil {
		t.Fatalf("final.Err: %v", final.Err)
	}
	if text != "hello world" {
		t.Errorf("got %q, want %q", text, "hello world")
	}
	if final.Language != "en" {
		t.Errorf("chunk.Language: got %q, want en", final.Language)
	}
}

func TestErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "invalid file format", "type": "invalid_request_error"}}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(final.Err.Error(), "invalid file format") {
		t.Errorf("error should contain API message, got: %v", final.Err)
	}
	if !strings.Contains(final.Err.Error(), "transcription API error") {
		t.Errorf("error should be APIError shape, got: %v", final.Err)
	}
	var apiErr *groq.APIError
	if !errors.As(final.Err, &apiErr) {
		t.Errorf("error should unwrap to *groq.APIError, got %T", final.Err)
	}
}

func TestRetryBackoff(t *testing.T) {
	var timestamps []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timestamps = append(timestamps, time.Now())
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "service unavailable", "type": "server_error"}}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected 503 error")
	}
	if len(timestamps) != 4 {
		t.Fatalf("expected 4 timestamps, got %d", len(timestamps))
	}
	expected := []time.Duration{500 * time.Millisecond, 1000 * time.Millisecond, 2000 * time.Millisecond}
	for i, e := range expected {
		actual := timestamps[i+1].Sub(timestamps[i])
		lo := time.Duration(float64(e) * 0.9)
		hi := time.Duration(float64(e) * 1.1)
		if actual < lo || actual > hi {
			t.Errorf("retry %d: got %v, want %v ±10%%", i, actual, e)
		}
	}
}

// TestRetryBackoffCtxCancel asserts the F3 fix: cancelling the
// context while the retry backoff is sleeping returns within a tight
// deadline (well under the full backoff sequence). The server returns
// a permanent 5xx so the backend would otherwise sleep ~3.5s in total
// across the three retry attempts. We cancel after ~100ms and assert
// the call returns within ~250ms — proving sleepCtx honors ctx.
func TestRetryBackoffCtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"error": {"message": "boom", "type": "server_error"}}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(ctx, bytes.NewReader([]byte("wav")), transcribe.Options{})
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

func TestTranscribeEmptyWave(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit when wav is empty")
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader(nil), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	_, final := collect(t, ch)
	if final.Err == nil {
		t.Fatal("expected error for empty WAV data")
	}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	_, err := groq.New(transcribe.Config{APIURL: "http://example.test", Model: "whisper"})
	if err == nil || !strings.Contains(err.Error(), "APIKey") {
		t.Errorf("expected APIKey-required error, got %v", err)
	}
}

func TestNew_RequiresModel(t *testing.T) {
	_, err := groq.New(transcribe.Config{APIKey: "key"})
	if err == nil || !strings.Contains(err.Error(), "Model") {
		t.Errorf("expected Model-required error, got %v", err)
	}
}

func TestNew_DefaultsAPIURL(t *testing.T) {
	b, err := groq.New(transcribe.Config{APIKey: "key", Model: "whisper"})
	if err != nil {
		t.Fatalf("groq.New: %v", err)
	}
	if b == nil {
		t.Fatal("groq.New returned nil backend")
	}
	// We cannot introspect b.cfg from outside the package, but the
	// fact that New did not complain about the missing URL proves
	// DefaultAPIURL was substituted. Verify the exported default is
	// stable.
	if groq.DefaultAPIURL == "" {
		t.Error("DefaultAPIURL constant must not be empty")
	}
}

func TestContextCancellation(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		time.Sleep(1 * time.Second)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "test"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(ctx, bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// The cancelled context should produce a closed channel without
	// a successful result.
	for chunk := range ch {
		if chunk.Err == nil && chunk.Text != "" {
			t.Errorf("expected error or empty result after cancel, got text=%q", chunk.Text)
		}
	}
}

func TestChannelClosedOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text": "closed"}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// Drain the channel.
	for range ch {
	}
	// A second receive on a closed channel returns immediately with
	// the zero value and ok=false.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after draining")
		}
	default:
		t.Error("channel receive should not block after drain")
	}
}

func TestChannelClosedOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error": {"message": "nope", "type": "bad"}}`)
	}))
	defer srv.Close()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	// Drain.
	for range ch {
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after error")
		}
	default:
		t.Error("channel receive should not block after drain")
	}
}

func TestCtxCancelDrainsChannel(t *testing.T) {
	// Server sleeps long enough that our cancellation happens while
	// the client is waiting on the response. The server handler
	// exits on its own after the sleep so srv.Close() does not wait
	// on a hung goroutine.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"text": "late"}`)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newTestBackend(t, srv)
	ch, err := b.Transcribe(ctx, bytes.NewReader([]byte("wav")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("channel did not close within 5s of ctx cancel")
	}
}

func TestRegistryHasGroq(t *testing.T) {
	factory, err := transcribe.Get("groq")
	if err != nil {
		t.Fatalf("transcribe.Get(groq): %v", err)
	}
	tr, err := factory(transcribe.Config{APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if tr == nil {
		t.Fatal("factory returned nil transcriber")
	}
}
