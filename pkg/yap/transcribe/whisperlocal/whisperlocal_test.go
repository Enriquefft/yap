package whisperlocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// fakeBinary creates a regular file in the test temp dir so the
// discover layer accepts it as a "binary". Tests that want a real
// subprocess use the on-PATH whisper-server when available; the unit
// tests in this file substitute the spawn function with a fake.
func fakeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "whisper-server")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return bin
}

// fakeModel creates a regular file in the test temp dir to stand in
// for ggml-base.en.bin.
func fakeModel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	model := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(model, []byte("fake-ggml"), 0o644); err != nil {
		t.Fatalf("write fake model: %v", err)
	}
	return model
}

// fakeServer wraps an httptest server that mimics whisper-server's
// /inference endpoint. The handler is supplied by the test so each
// case can choose between success / failure / counter behavior.
func fakeServer(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	return srv.URL, srv.Close
}

// stubProc returns a serverProc whose lifetime is bound to the test
// lifecycle. The waitDone channel never fires unless the test
// explicitly closes it (simulating a crash).
func stubProc(baseURL string) *serverProc {
	return &serverProc{
		cmd:      nil, // intentionally nil — terminate path is no-op
		baseURL:  baseURL,
		waitDone: make(chan struct{}),
	}
}

func TestNew_DoesNotSpawnSubprocess(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)

	b, err := New(transcribe.Config{
		WhisperServerPath: bin,
		ModelPath:         model,
		Model:             "base.en",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	if b.proc != nil {
		t.Fatal("New must not spawn the subprocess; b.proc should be nil")
	}
}

func TestNew_DiscoveryFailureSurfacesAtConstruction(t *testing.T) {
	_, err := New(transcribe.Config{
		WhisperServerPath: "/no/such/binary",
		ModelPath:         "/no/such/model",
	})
	if err == nil {
		t.Fatal("expected discovery error from New")
	}
}

func TestTranscribe_FakeSubprocessReturnsText(t *testing.T) {
	url, cleanup := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hello world"}`))
	})
	defer cleanup()

	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{
		WhisperServerPath: bin,
		ModelPath:         model,
		Language:          "en",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	// Substitute the spawn function so we never fork the real binary.
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		return stubProc(url), nil
	}

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var collected []transcribe.TranscriptChunk
	for c := range chunks {
		collected = append(collected, c)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(collected))
	}
	if collected[0].Err != nil {
		t.Fatalf("chunk error: %v", collected[0].Err)
	}
	if collected[0].Text != "hello world" {
		t.Errorf("text = %q, want %q", collected[0].Text, "hello world")
	}
	if !collected[0].IsFinal {
		t.Error("expected IsFinal=true")
	}
	if collected[0].Language != "en" {
		t.Errorf("language = %q, want %q", collected[0].Language, "en")
	}
}

func TestTranscribe_PlainTextResponseFallback(t *testing.T) {
	url, cleanup := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello plain"))
	})
	defer cleanup()

	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		return stubProc(url), nil
	}

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	for c := range chunks {
		if c.Err != nil {
			t.Fatalf("chunk error: %v", c.Err)
		}
		if c.Text != "hello plain" {
			t.Errorf("text = %q, want %q", c.Text, "hello plain")
		}
	}
}

func TestTranscribe_5xxRetriesOnce(t *testing.T) {
	var calls int32
	url, cleanup := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "transient", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"recovered"}`))
	})
	defer cleanup()

	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		return stubProc(url), nil
	}

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err != nil {
		t.Fatalf("expected recovery on retry, got error: %v", got.Err)
	}
	if got.Text != "recovered" {
		t.Errorf("text = %q, want %q", got.Text, "recovered")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 retry), got %d", calls)
	}
}

func TestTranscribe_4xxDoesNotRetry(t *testing.T) {
	var calls int32
	url, cleanup := fakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	defer cleanup()

	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		return stubProc(url), nil
	}

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err == nil {
		t.Fatal("expected error from 4xx response")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on 4xx), got %d", calls)
	}
}

func TestTranscribe_EmptyAudio(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		t.Fatal("spawn should not be called for empty audio")
		return nil, nil
	}

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err == nil {
		t.Fatal("expected error for empty audio")
	}
	if !strings.Contains(got.Err.Error(), "empty") {
		t.Errorf("expected error to mention empty, got %q", got.Err.Error())
	}
}

func TestTranscribe_NilAudio(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	if _, err := b.Transcribe(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil audio reader")
	}
}

func TestClose_Idempotent(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestClose_RejectsFurtherTranscribes(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err == nil {
		t.Fatal("expected error after Close")
	}
	if !strings.Contains(got.Err.Error(), "closed") {
		t.Errorf("expected closed-backend error, got %q", got.Err.Error())
	}
}

func TestEnsureServer_DeadProcRespawnsLazily(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	var spawnCount int32
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		atomic.AddInt32(&spawnCount, 1)
		return stubProc("http://127.0.0.1:0"), nil
	}

	// First ensureServer spawns once.
	if _, err := b.ensureServer(context.Background()); err != nil {
		t.Fatalf("ensureServer #1: %v", err)
	}
	if spawnCount != 1 {
		t.Fatalf("spawnCount = %d, want 1", spawnCount)
	}
	// Second call without simulating death must reuse.
	if _, err := b.ensureServer(context.Background()); err != nil {
		t.Fatalf("ensureServer #2: %v", err)
	}
	if spawnCount != 1 {
		t.Fatalf("after reuse spawnCount = %d, want 1", spawnCount)
	}

	// Simulate the subprocess dying: close the waitDone channel of
	// the currently-tracked proc. Next ensureServer must respawn.
	b.mu.Lock()
	close(b.proc.waitDone)
	b.mu.Unlock()

	if _, err := b.ensureServer(context.Background()); err != nil {
		t.Fatalf("ensureServer #3: %v", err)
	}
	if spawnCount != 2 {
		t.Fatalf("after death spawnCount = %d, want 2", spawnCount)
	}
}

func TestImplementsCloser(t *testing.T) {
	var _ io.Closer = (*Backend)(nil)
}

// TestTerminate_WedgedSubprocessIsBounded asserts the C6 fix: when
// the kernel wedges and waitDone never fires, the post-SIGKILL wait
// times out at shutdownGraceful instead of blocking the daemon
// shutdown forever. We synthesize a "wedged" child by handing the
// helper a waitDone channel that is never closed; the function must
// return within ~2*shutdownGraceful (SIGTERM grace + post-SIGKILL
// detach window).
func TestTerminate_WedgedSubprocessIsBounded(t *testing.T) {
	wedged := make(chan struct{}) // never closed — simulates wedged kernel
	var sigtermCalled, sigkillCalled int32

	start := time.Now()
	terminate(
		wedged,
		func() { atomic.AddInt32(&sigtermCalled, 1) },
		func() { atomic.AddInt32(&sigkillCalled, 1) },
		1234,
	)
	elapsed := time.Since(start)

	// The function MUST return within (SIGTERM grace) + (post-Kill
	// detach window) + a small slack. Without the C6 fix it would
	// block forever on the bare <-waitDone receive.
	upper := 2*shutdownGraceful + 500*time.Millisecond
	if elapsed > upper {
		t.Errorf("terminate elapsed = %v, want < %v (post-SIGKILL wait must be bounded)", elapsed, upper)
	}
	// Lower bound: at least the SIGTERM grace, since waitDone never
	// fires.
	if elapsed < shutdownGraceful {
		t.Errorf("terminate returned in %v, want >= %v (SIGTERM grace must elapse)", elapsed, shutdownGraceful)
	}
	if atomic.LoadInt32(&sigtermCalled) != 1 {
		t.Errorf("sigterm called %d times, want 1", sigtermCalled)
	}
	if atomic.LoadInt32(&sigkillCalled) != 1 {
		t.Errorf("sigkill called %d times, want 1", sigkillCalled)
	}
}

// TestTerminate_GracefulShutdown asserts the happy path: SIGTERM
// elicits an exit (waitDone closes during the grace window) and the
// helper returns immediately without escalating to SIGKILL.
func TestTerminate_GracefulShutdown(t *testing.T) {
	wait := make(chan struct{})
	var sigkillCalled int32

	go func() {
		// Simulate the subprocess exiting promptly after SIGTERM.
		time.Sleep(20 * time.Millisecond)
		close(wait)
	}()

	start := time.Now()
	terminate(
		wait,
		func() {}, // SIGTERM no-op for the simulation
		func() { atomic.AddInt32(&sigkillCalled, 1) },
		1234,
	)
	elapsed := time.Since(start)

	if elapsed > shutdownGraceful {
		t.Errorf("graceful path elapsed = %v, want < %v", elapsed, shutdownGraceful)
	}
	if atomic.LoadInt32(&sigkillCalled) != 0 {
		t.Errorf("sigkill called %d times, want 0 (graceful path should not escalate)", sigkillCalled)
	}
}

// TestSpawnReadiness_TimesOut exercises the production spawn function
// against a binary that exits immediately. The spawn must surface an
// error rather than hang.
func TestSpawnReadiness_BinaryThatExits(t *testing.T) {
	dir := t.TempDir()
	exiter := filepath.Join(dir, "exit-now")
	if err := os.WriteFile(exiter, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write exit-now binary: %v", err)
	}
	model := fakeModel(t)
	b, err := New(transcribe.Config{
		WhisperServerPath: exiter,
		ModelPath:         model,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = b.ensureServer(ctx)
	if err == nil {
		t.Fatal("expected spawn error from immediately-exiting binary")
	}
	if !errors.Is(err, errors.Unwrap(err)) && !strings.Contains(err.Error(), "exit") && !strings.Contains(err.Error(), "subprocess") {
		t.Errorf("error should mention subprocess exit, got %q", err.Error())
	}
}
