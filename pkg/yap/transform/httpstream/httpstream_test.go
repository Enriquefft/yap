package httpstream_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap/transform/httpstream"
)

// newTestClient returns a Client wired to the supplied server with a
// tight retry policy (no backoff) so tests run quickly.
func newTestClient(server *httptest.Server) *httpstream.Client {
	c := httpstream.NewClient(5 * time.Second)
	c.HTTP = server.Client()
	c.MaxRetries = 3
	c.Backoff = []time.Duration{0, 0, 0}
	return c
}

// TestPostJSON_Success asserts that a 2xx response returns the body
// reader for the caller and that the request body was valid JSON
// containing the supplied payload.
func TestPostJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want Bearer secret", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if !strings.Contains(string(body), `"hello":"world"`) {
			t.Errorf("request body missing field: %q", string(body))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.PostJSON(context.Background(), srv.URL, "secret", map[string]string{"hello": "world"})
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	defer body.Close()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(out) != "ok" {
		t.Errorf("body = %q, want %q", out, "ok")
	}
}

// TestPostJSON_NoAuthHeader asserts that an empty apiKey omits the
// Authorization header entirely rather than sending "Bearer ".
func TestPostJSON_NoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header should be empty, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.PostJSON(context.Background(), srv.URL, "", struct{}{})
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	body.Close()
}

// TestPostJSON_4xx_FailsFast asserts that a 4xx response is returned
// immediately (no retries) as a NonRetryableError carrying the status
// and body.
func TestPostJSON_4xx_FailsFast(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.PostJSON(context.Background(), srv.URL, "", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var nre *httpstream.NonRetryableError
	if !errors.As(err, &nre) {
		t.Fatalf("expected NonRetryableError, got %T: %v", err, err)
	}
	if nre.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", nre.StatusCode)
	}
	if !strings.Contains(nre.Body, "bad key") {
		t.Errorf("Body = %q, want to contain %q", nre.Body, "bad key")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (4xx must not retry)", got)
	}
}

// TestPostJSON_5xx_Retries asserts that a 5xx response triggers
// retries up to MaxRetries and eventually succeeds when the server
// starts returning 2xx mid-sequence.
func TestPostJSON_5xx_Retries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("temporary"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("after retry"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.PostJSON(context.Background(), srv.URL, "", struct{}{})
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	defer body.Close()
	out, _ := io.ReadAll(body)
	if string(out) != "after retry" {
		t.Errorf("body = %q, want %q", out, "after retry")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

// TestPostJSON_5xx_Exhausted asserts that persistent 5xx responses
// eventually return the last server error after exhausting retries.
func TestPostJSON_5xx_Exhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	_, err := c.PostJSON(context.Background(), srv.URL, "", struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "upstream down") {
		t.Errorf("error = %v, want to contain %q", err, "upstream down")
	}
	if got := atomic.LoadInt32(&calls); got != 4 { // 1 initial + 3 retries
		t.Errorf("calls = %d, want 4", got)
	}
}

// TestPostJSON_TransportError_Retries asserts that a transport failure
// (connection refused) is retried.
func TestPostJSON_TransportError_Retries(t *testing.T) {
	c := httpstream.NewClient(1 * time.Second)
	c.MaxRetries = 2
	c.Backoff = []time.Duration{0, 0}

	// Point at a closed listener — Dial will fail every time.
	_, err := c.PostJSON(context.Background(), "http://127.0.0.1:1", "", struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "transport") {
		t.Errorf("error = %v, want to contain %q", err, "transport")
	}
}

// TestPostJSON_ContextCancelled asserts that a cancelled context
// short-circuits the retry loop instead of running through every
// attempt.
func TestPostJSON_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Backoff = []time.Duration{200 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := c.PostJSON(ctx, srv.URL, "", struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// TestPostJSON_InvalidJSONPayload asserts that a payload that cannot
// be marshalled (a channel, for example) produces an immediate error.
func TestPostJSON_InvalidJSONPayload(t *testing.T) {
	c := httpstream.NewClient(1 * time.Second)
	_, err := c.PostJSON(context.Background(), "http://example.invalid", "", make(chan int))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Errorf("error = %v, want to contain %q", err, "marshal")
	}
}
