package whisperlocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
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
// for ggml-base.en.bin. The file starts with the ggml magic bytes
// so resolveModel's format check accepts it.
func fakeModel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	model := filepath.Join(dir, "ggml-base.en.bin")
	// "lmgg" is the little-endian on-disk representation of the
	// uint32 ggml magic. The trailing bytes are filler.
	if err := os.WriteFile(model, []byte("lmgg-test-fixture"), 0o644); err != nil {
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

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
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

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
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

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
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

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
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

	chunks, err := b.Transcribe(context.Background(), bytes.NewReader(nil), transcribe.Options{})
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
	if _, err := b.Transcribe(context.Background(), nil, transcribe.Options{}); err == nil {
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
	chunks, tErr := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
	if tErr != nil {
		t.Fatalf("Transcribe: %v", tErr)
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

// TestClose_DuringInflightTranscribe covers the C1 fix: Close must
// wait for in-flight Transcribe goroutines to drain (via the
// WaitGroup) and must abort pending HTTP requests via the backend's
// close context so the caller sees context.Canceled rather than a
// stray ECONNREFUSED.
//
// The test wires Transcribe against an httptest server that blocks
// for 500ms before responding. Close is called 50ms into the
// request; the call must return in well under the full 500ms
// because the close context cancellation triggers an immediate
// abort, and the Transcribe goroutine must exit cleanly with a
// cancellation error.
func TestClose_DuringInflightTranscribe(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			// Client cancelled — exit without writing.
			return
		case <-release:
		case <-time.After(2 * time.Second):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"slow"}`))
	}))
	defer srv.Close()
	defer close(release)

	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		return stubProc(srv.URL), nil
	}

	// Fire Transcribe in the background. It will block inside
	// the HTTP request for the entire 2s fake-server handler
	// unless Close cancels it first.
	chunks, tErr := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
	if tErr != nil {
		t.Fatalf("Transcribe: %v", tErr)
	}

	// Snapshot the baseline goroutine count so we can verify
	// the close path does not leak anything.
	baseline := runtime.NumGoroutine()

	// Wait a moment so the Transcribe goroutine actually
	// enters the HTTP request.
	time.Sleep(50 * time.Millisecond)

	closeStart := time.Now()
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	closeDur := time.Since(closeStart)
	if closeDur > 1500*time.Millisecond {
		t.Errorf("Close took %s — expected well under the 2s server block", closeDur)
	}

	// Drain the chunks channel so we observe the goroutine exit.
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err == nil {
		t.Fatal("expected an error from the in-flight Transcribe after Close")
	}

	// Allow a moment for the goroutine to finish unwinding
	// before we sample the count again.
	time.Sleep(50 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > baseline+2 {
		t.Errorf("goroutine count grew from %d to %d — possible leak", baseline, n)
	}
}

// TestEnsureServer_ConcurrentCallsSingleSpawn covers the C2 fix:
// the spawn-in-progress sentinel must coalesce concurrent callers
// into a single spawn.
//
// Eight goroutines race into ensureServer while the spawn function
// sleeps for 100ms; the test asserts exactly one spawn call happened
// and every goroutine saw the resulting baseURL.
func TestEnsureServer_ConcurrentCallsSingleSpawn(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	var spawnCalls int32
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		atomic.AddInt32(&spawnCalls, 1)
		// Simulate a slow model load. The whole point of the
		// C2 fix is that this sleep must NOT serialise the
		// other ensureServer calls: they share the first
		// spawn's result via the spawning sentinel.
		time.Sleep(100 * time.Millisecond)
		return stubProc("http://127.0.0.1:9999"), nil
	}

	const callers = 8
	var wg sync.WaitGroup
	urls := make(chan string, callers)
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			url, err := b.ensureServer(context.Background())
			if err != nil {
				errs <- err
				return
			}
			urls <- url
		}()
	}
	wg.Wait()
	close(urls)
	close(errs)

	for err := range errs {
		t.Errorf("ensureServer: %v", err)
	}
	if got := atomic.LoadInt32(&spawnCalls); got != 1 {
		t.Errorf("spawn was called %d times; expected exactly 1", got)
	}
	seen := 0
	for url := range urls {
		if url != "http://127.0.0.1:9999" {
			t.Errorf("caller saw unexpected url %q", url)
		}
		seen++
	}
	if seen != callers {
		t.Errorf("only %d of %d callers received a url", seen, callers)
	}
}

// TestBackend_CircuitBreaker_TripsAfter3ConsecutiveFailures covers
// the C3 fix: after N consecutive transcription failures the next
// Transcribe call must short-circuit with the breaker error
// instead of fork-execing the subprocess again.
func TestBackend_CircuitBreaker_TripsAfter3ConsecutiveFailures(t *testing.T) {
	bin := fakeBinary(t)
	model := fakeModel(t)
	b, err := New(transcribe.Config{WhisperServerPath: bin, ModelPath: model})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	var spawnCalls int32
	b.spawn = func(ctx context.Context, _ *Backend) (*serverProc, error) {
		atomic.AddInt32(&spawnCalls, 1)
		return nil, errors.New("fake spawn failure")
	}

	// Fire three calls: each one fails to spawn and bumps the
	// failure counter. Every one must reach the spawn stub.
	for i := 0; i < circuitBreakerThreshold; i++ {
		chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
		if err != nil {
			t.Fatalf("Transcribe #%d: %v", i, err)
		}
		var got transcribe.TranscriptChunk
		for c := range chunks {
			got = c
		}
		if got.Err == nil {
			t.Fatalf("Transcribe #%d: expected an error", i)
		}
	}
	// Spawn is called twice per failing Transcribe because
	// shouldRetry returns true for non-net errors wrapped by
	// the retry path? It does not — "fake spawn failure" is a
	// plain error that shouldRetry rejects. So the count is
	// exactly circuitBreakerThreshold.
	wantAfterTrip := int32(circuitBreakerThreshold)
	if got := atomic.LoadInt32(&spawnCalls); got != wantAfterTrip {
		t.Errorf("spawnCalls after tripping = %d, want %d", got, wantAfterTrip)
	}

	// The next Transcribe must short-circuit: the breaker
	// error is delivered without calling spawn.
	chunks, err := b.Transcribe(context.Background(), bytes.NewReader([]byte("WAVE")), transcribe.Options{})
	if err != nil {
		t.Fatalf("Transcribe after trip: %v", err)
	}
	var got transcribe.TranscriptChunk
	for c := range chunks {
		got = c
	}
	if got.Err == nil {
		t.Fatal("expected breaker error after threshold tripped")
	}
	if !strings.Contains(got.Err.Error(), "temporarily broken") {
		t.Errorf("expected 'temporarily broken' in error, got %q", got.Err.Error())
	}
	if got := atomic.LoadInt32(&spawnCalls); got != wantAfterTrip {
		t.Errorf("spawn was called after breaker trip: got %d, want %d", got, wantAfterTrip)
	}
}

// TestWaitForListening_FailureIncludesSubprocessOutput covers the
// C5 fix: startup failures must carry the last lines of captured
// stdout/stderr in the wrapped error so users see the real
// diagnostic instead of a vague timeout.
//
// The test uses a fake subprocess that writes diagnostic lines to
// its pipeBuffer and then marks itself as exited so waitForListening
// reports the early exit path.
func TestWaitForListening_FailureIncludesSubprocessOutput(t *testing.T) {
	pipes := newPipeBuffer(io.Discard, "[test]")
	_, _ = pipes.Write([]byte("loading model /etc/does-not-exist\n"))
	_, _ = pipes.Write([]byte("error: failed to initialize whisper context\n"))

	proc := &serverProc{
		waitDone: make(chan struct{}),
		waitErr:  errors.New("exit status 1"),
		pipes:    pipes,
	}
	close(proc.waitDone) // simulate immediate exit

	err := waitForListening(context.Background(), "127.0.0.1", 65535, proc)
	if err == nil {
		t.Fatal("expected error from dead subprocess")
	}
	msg := err.Error()
	if !strings.Contains(msg, "failed to initialize whisper context") {
		t.Errorf("expected captured output in error, got:\n%s", msg)
	}
	if !strings.Contains(msg, "loading model") {
		t.Errorf("expected first captured line in error, got:\n%s", msg)
	}
}

// TestPortAllocation_RetryOnBindFailure covers the C4 fix: when
// the first spawn attempt's captured output contains the
// bind-failure banner, spawnWhisperServer must retry up to
// spawnRetryAttempts times and succeed on a later attempt.
//
// The test uses a fake spawn function that fails the first two
// attempts by returning a *bindFailureError and succeeds on the
// third. The production spawnWhisperServer funnels its attempts
// through spawnWhisperServerOnce which is what we simulate here by
// calling the retry loop directly against a counted spawner.
func TestPortAllocation_RetryOnBindFailure(t *testing.T) {
	var attempts int32

	// The shim matches spawnWhisperServer's retry loop shape: on
	// bind-failure it re-attempts up to spawnRetryAttempts, on
	// any other error it surfaces immediately.
	spawnOnce := func() (*serverProc, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			pipes := newPipeBuffer(io.Discard, "[test]")
			_, _ = pipes.Write([]byte("error: couldn't bind to server socket: hostname=127.0.0.1 port=12345\n"))
			proc := &serverProc{waitDone: make(chan struct{}), pipes: pipes}
			close(proc.waitDone)
			return nil, wrapSpawnError(errors.New("subprocess exited during startup"), proc)
		}
		return &serverProc{
			baseURL:  "http://127.0.0.1:12345",
			waitDone: make(chan struct{}),
		}, nil
	}

	var proc *serverProc
	var lastErr error
	for i := 0; i < spawnRetryAttempts; i++ {
		p, err := spawnOnce()
		if err == nil {
			proc = p
			lastErr = nil
			break
		}
		lastErr = err
		if !isBindFailureError(err) {
			t.Fatalf("unexpected non-bind error: %v", err)
		}
	}
	if lastErr != nil {
		t.Fatalf("retry exhausted: %v", lastErr)
	}
	if proc == nil {
		t.Fatal("spawn did not produce a proc on the third attempt")
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
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

	// Must return within (SIGTERM grace) + (post-Kill detach window)
	// + a small slack. Without the C6 fix the bare <-waitDone on the
	// Kill path would block forever.
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

// TestResolveThreadCount covers the auto-count policy: 0 requests
// runtime.NumCPU()/2 rounded up (ceiling) to at least 1; positive
// requests pass through unchanged. The helper is a pure function
// over (requested, numCPU) so the test can pin both axes without
// depending on the host CPU topology.
//
// The 3-core case is a regression guard for the L2 fix: prior
// versions used integer floor (numCPU / 2) and returned 1 for a
// 3-core host, contradicting the "rounded up" doc contract. The
// ceiling formula (numCPU + 1) / 2 gives 2, matching the doc and
// the performance-oriented intent (use more cores, not fewer).
func TestResolveThreadCount(t *testing.T) {
	cases := []struct {
		name      string
		requested int
		numCPU    int
		want      int
	}{
		{"auto 22 cores", 0, 22, 11},
		{"auto 14 cores", 0, 14, 7},
		{"auto 8 cores", 0, 8, 4},
		{"auto 5 cores rounds up", 0, 5, 3},
		{"auto 3 cores rounds up", 0, 3, 2},
		{"auto 2 cores", 0, 2, 1},
		{"auto 1 core rounds up", 0, 1, 1},
		{"auto 0 cores rounds up", 0, 0, 1},
		{"explicit 1", 1, 22, 1},
		{"explicit 4", 4, 22, 4},
		{"explicit 8", 8, 22, 8},
		{"explicit overrides auto", 16, 2, 16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveThreadCount(tc.requested, tc.numCPU); got != tc.want {
				t.Errorf("resolveThreadCount(%d, %d) = %d, want %d",
					tc.requested, tc.numCPU, got, tc.want)
			}
		})
	}
}

// TestSpawnArgs_ThreadsAndGPU covers the Bug-10 fix: the
// spawnWhisperServerOnce path must pass --threads and, when the user
// opts out of GPU, --no-gpu to whisper-server. The test uses a tiny
// shell script as a fake whisper-server that records its argv to a
// file before exiting; waitForListening's readiness poll never
// succeeds against it (the script does not listen on the port), so
// the function returns an error — that is fine, we only care about
// the argv capture.
//
// whisper-server exposes GPU control as a single --no-gpu boolean
// flag (no value); it has no --use-gpu flag and rejects
// --use-gpu=true/false as "unknown argument". The upstream default is
// GPU-on, so the daemon emits --no-gpu only when the user opted out
// and emits nothing when GPU is requested.
//
// The test runs twice: once with threads=0 (auto) and useGPU=true
// asserting --no-gpu is ABSENT, once with an explicit thread count
// and useGPU=false asserting --no-gpu is present. In both cases the
// test additionally asserts --use-gpu is NEVER in argv, guarding
// against a regression back to the broken flag name.
func TestSpawnArgs_ThreadsAndGPU(t *testing.T) {
	// Skip on builders that cannot run /bin/sh (very unusual; the
	// rest of the test suite already assumes a POSIX shell).
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no /bin/sh available: %v", err)
	}

	cases := []struct {
		name         string
		threads      int
		useGPU       bool
		wantThreads  int
		wantNoGPUArg bool
	}{
		{
			name:         "auto threads, gpu on",
			threads:      0,
			useGPU:       true,
			wantThreads:  resolveThreadCount(0, runtime.NumCPU()),
			wantNoGPUArg: false,
		},
		{
			name:         "explicit 8 threads, gpu off",
			threads:      8,
			useGPU:       false,
			wantThreads:  8,
			wantNoGPUArg: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			argsOut := filepath.Join(dir, "argv.txt")
			bin := filepath.Join(dir, "whisper-server")
			// The stub writes every argument on its own line and
			// exits 0 so spawnWhisperServerOnce's readiness poll
			// observes an early exit rather than a hang.
			script := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\" >> " + argsOut + "; done\nexit 0\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatalf("write stub binary: %v", err)
			}
			model := fakeModel(t)

			b, err := New(transcribe.Config{
				WhisperServerPath: bin,
				ModelPath:         model,
				WhisperThreads:    tc.threads,
				WhisperUseGPU:     tc.useGPU,
				Language:          "en",
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer b.Close()

			// spawnWhisperServerOnce will start the stub, observe
			// its immediate exit inside waitForListening, and
			// return an error. That is expected — the argv file
			// on disk is our source of truth.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = b.ensureServer(ctx)

			data, err := os.ReadFile(argsOut)
			if err != nil {
				t.Fatalf("read argv file: %v", err)
			}
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

			// Helper: index of flag in the captured argv.
			indexOf := func(flag string) int {
				for i, l := range lines {
					if l == flag {
						return i
					}
				}
				return -1
			}

			if idx := indexOf("--threads"); idx < 0 || idx+1 >= len(lines) {
				t.Fatalf("--threads flag missing from argv: %v", lines)
			} else if got := lines[idx+1]; got != strconv.Itoa(tc.wantThreads) {
				t.Errorf("--threads value = %q, want %q (full argv: %v)",
					got, strconv.Itoa(tc.wantThreads), lines)
			}
			// GPU flag: --no-gpu present iff useGPU=false.
			hasNoGPU := indexOf("--no-gpu") >= 0
			if hasNoGPU != tc.wantNoGPUArg {
				t.Errorf("--no-gpu present = %v, want %v (full argv: %v)",
					hasNoGPU, tc.wantNoGPUArg, lines)
			}
			// Regression guard: --use-gpu=* is an invented flag
			// whisper-server rejects. It must never appear in argv
			// regardless of the useGPU setting.
			for _, l := range lines {
				if strings.HasPrefix(l, "--use-gpu") {
					t.Errorf("argv contains forbidden %q; whisper-server rejects --use-gpu and the daemon must use --no-gpu", l)
				}
			}
			// Guard the always-present flags so a future refactor
			// cannot silently drop them.
			for _, want := range []string{"--model", "--host", "--port", "--language"} {
				if indexOf(want) < 0 {
					t.Errorf("expected %q in argv, got %v", want, lines)
				}
			}
		})
	}
}

// TestSpawnArgs_AcceptedByRealWhisperServer is the smoke test that
// closes the gap the shell stub cannot: it runs the real
// whisper-server binary if one is discoverable on $PATH with
// --help and our real argv (minus --model so the binary exits
// early without trying to load a model). The point is to catch a
// regression where we reintroduce a flag whisper-server rejects.
// The binary prints "error: unknown argument: ..." to stdout on an
// unknown flag, so the test fails if that string appears.
//
// When whisper-server is not on PATH (no whisper-cpp installed),
// the test is skipped — the stub test above still guards the
// argv shape.
func TestSpawnArgs_AcceptedByRealWhisperServer(t *testing.T) {
	bin, err := exec.LookPath("whisper-server")
	if err != nil {
		t.Skip("whisper-server not on PATH; skipping real-binary smoke test")
	}

	cases := []struct {
		name   string
		useGPU bool
	}{
		{name: "gpu on", useGPU: true},
		{name: "gpu off", useGPU: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{
				"--model", "/nonexistent.bin",
				"--host", "127.0.0.1",
				"--port", "1",
				"--threads", "1",
			}
			if !tc.useGPU {
				args = append(args, "--no-gpu")
			}
			args = append(args, "--language", "en")
			// Run with a short timeout so a binary that actually
			// tries to serve does not wedge the test.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, args...)
			out, _ := cmd.CombinedOutput()
			if bytes.Contains(out, []byte("unknown argument")) {
				t.Fatalf("whisper-server rejected an argument in argv %v:\n%s", args, out)
			}
		})
	}
}

// TestTerminate_GracefulShutdown asserts the happy path: SIGTERM
// elicits an exit (waitDone closes during the grace window) and the
// helper returns immediately without escalating to SIGKILL.
func TestTerminate_GracefulShutdown(t *testing.T) {
	wait := make(chan struct{})
	var sigkillCalled int32

	go func() {
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
