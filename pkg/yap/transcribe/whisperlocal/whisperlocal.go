package whisperlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// Default tunables. They live as constants so the package contains no
// mutable globals beyond what the noglobals guard whitelists.
const (
	// defaultRequestTimeout caps individual /inference HTTP calls. The
	// model has already been loaded by then, so this is the wall-time
	// budget for transcribing a single recording. base.en on a modern
	// laptop CPU returns a 5-second clip in well under a second; 60s
	// is a generous safety net for slower hardware and longer clips.
	defaultRequestTimeout = 60 * time.Second

	// startupTimeout caps how long we wait for the subprocess to start
	// accepting TCP connections after Cmd.Start. The model load is the
	// long pole here — base.en is ~150 MB and parses in well under a
	// second on warm caches; 30s covers cold caches and slow disks.
	startupTimeout = 30 * time.Second

	// startupPollInterval is how often the readiness loop tries to
	// dial the chosen port during the startup window.
	startupPollInterval = 50 * time.Millisecond

	// shutdownGraceful is the SIGTERM-to-SIGKILL grace period. The
	// subprocess flushes very little state (no on-disk writes), so a
	// short grace period is correct.
	shutdownGraceful = 2 * time.Second

	// closeDrainTimeout caps how long Close waits for in-flight
	// Transcribe goroutines to drain before forcibly tearing down
	// the subprocess. The bound prevents a hung HTTP request from
	// pinning daemon shutdown forever.
	closeDrainTimeout = 5 * time.Second

	// circuitBreakerThreshold is the number of consecutive
	// transcription failures that trip the breaker. After the
	// threshold is reached the breaker stays open for
	// circuitBreakerCooldown so we do not fork-exec a broken
	// subprocess on every recording.
	circuitBreakerThreshold = 3

	// circuitBreakerCooldown is how long the breaker stays open
	// after tripping. The next Transcribe call after the cooldown
	// elapses retries the spawn; on success the breaker resets, on
	// failure the cycle repeats.
	circuitBreakerCooldown = 30 * time.Second

	// pipeBufferLines is the number of lines retained from the
	// subprocess stdout/stderr ring buffer. 32 lines is enough to
	// surface a model load failure or a bind error in the
	// waitForListening error message without unbounded memory use.
	pipeBufferLines = 32

	// pipeBufferLineBytes is the per-line byte cap on the ring
	// buffer. Whisper-server's banners are short; chatty debug
	// builds get truncated lines rather than unbounded growth.
	pipeBufferLineBytes = 512

	// spawnRetryAttempts is the number of times spawnWhisperServer
	// retries on a recoverable bind failure ("address already in
	// use") before surfacing a structured error. Higher values
	// hide real misconfigurations; lower values are flaky on busy
	// CI hosts.
	spawnRetryAttempts = 3
)

// backendClosedMsg is the sticky error message returned by every
// public entry point after Close has been called. It is a const so
// the whisperlocal package contains zero package-level vars (the
// noglobals AST guard rejects any var declaration). Callers wrap it
// in errors.New at return time via newBackendClosedError.
const backendClosedMsg = "whisperlocal: backend is closed"

// newBackendClosedError constructs the sticky error returned when
// the backend has been closed. Every callsite that wants to return
// "backend is closed" goes through this helper so the wording is
// single-sourced.
func newBackendClosedError() error {
	return errors.New(backendClosedMsg)
}

// spawnFn is the seam tests use to substitute a fake subprocess
// spawner. The production wiring assigns defaultSpawnFunc() (which
// is platform-specific — see whisperlocal_unix.go and
// whisperlocal_windows.go).
type spawnFn func(ctx context.Context, b *Backend) (*serverProc, error)

// Backend is the whisperlocal implementation of transcribe.Transcriber.
// It owns the lifecycle of one whisper-server child process across the
// lifetime of the parent yap process.
//
// The struct is safe for concurrent use. Synchronisation is split
// across three primitives so concurrent Transcribe / Close calls do
// not pessimise the critical path:
//
//   - mu guards the spawn-vs-reuse decision and the closed flag. It
//     is held only for short non-blocking sections; the expensive
//     subprocess startup happens without the lock via the spawning
//     channel sentinel.
//   - wg counts in-flight Transcribe goroutines so Close can drain
//     them before tearing the subprocess down.
//   - closeCtx is cancelled by Close so an in-flight HTTP POST
//     surfaces context.Canceled instead of ECONNREFUSED when the
//     subprocess is killed underneath it.
type Backend struct {
	cfg        transcribe.Config
	serverPath string
	modelPath  string
	language   string
	// threads is the explicit whisper.cpp thread count forwarded to
	// whisper-server via --threads. Zero means the backend computes
	// a sane auto-count at spawn time (see resolveThreadCount).
	// Storing it on the Backend keeps the spawn function a pure
	// function of *Backend and lets tests override it deterministically
	// without environment sniffing.
	threads int
	// useGPU controls whisper-server's --use-gpu flag. Defaults to
	// true upstream so the production path keeps GPU on; downgrading
	// to CPU is an opt-in knob for systems where the GPU backend
	// misbehaves or the user wants deterministic wall time.
	useGPU bool
	client *http.Client

	// spawn is the function that actually starts the subprocess.
	// Tests inject a fake that returns an httptest server URL and a
	// no-op cmd. Production wires platform-specific defaultSpawn.
	spawn spawnFn

	// closeCtx is cancelled by Close so a pending postInference
	// request aborts as context.Canceled rather than seeing the
	// subprocess vanish underneath it (ECONNREFUSED). closeCancel
	// is the matching CancelFunc.
	closeCtx    context.Context
	closeCancel context.CancelFunc

	// wg counts in-flight Transcribe goroutines. Close waits on
	// it (with a bounded timeout) before terminating the
	// subprocess so callers cannot rug-pull a request mid-flight.
	wg sync.WaitGroup

	mu sync.Mutex
	// proc is the live subprocess record. nil if no subprocess
	// is running. Replaced wholesale on respawn.
	proc *serverProc
	// closed is the sticky one-way flag toggled by Close.
	closed bool
	// spawning is the spawn-in-progress sentinel: when non-nil it
	// is the channel currently-spawning concurrent callers wait
	// on. The mutex is released while waiting so Close and other
	// fast-path operations are not blocked behind a 30s startup.
	spawning chan struct{}

	// Circuit breaker state. failCount is incremented on every
	// terminal Transcribe failure and reset on success. brokenUntil
	// is the wall-clock time before which Transcribe returns the
	// breaker error without forking the subprocess.
	failCount   int
	brokenUntil time.Time
}

// serverProc holds the live subprocess handles.
type serverProc struct {
	cmd     *exec.Cmd
	baseURL string
	// waitDone is closed by the spawn function's wait goroutine
	// when cmd.Wait returns. Custom spawn implementations MUST
	// close this channel when the underlying process dies,
	// otherwise terminateProc will block forever waiting for the
	// final receive at the end of the SIGKILL grace period.
	//
	// The wait goroutine is the single source of truth for the
	// process exit status: it stores the cmd.Wait error in
	// waitErr before closing the channel, so other goroutines can
	// observe the exit (and read the error) via a non-blocking
	// receive on waitDone.
	waitDone chan struct{}
	waitErr  error
	// pipes is the captured stdout/stderr ring of the subprocess.
	// nil for stub subprocesses substituted by tests; populated
	// by the production spawn so waitForListening can include the
	// last few lines of subprocess output in startup errors.
	pipes *pipeBuffer
}

// New constructs a Backend from cfg without spawning the subprocess.
// It validates the static configuration (binary discovery, model
// resolution, language) and returns a non-nil error if anything is
// missing. Runtime failures (subprocess crash, network) surface from
// Transcribe.
//
// New is implemented per-platform: the unix variant wires
// spawnWhisperServer; the Windows variant returns an explicit "not
// supported" error so side-effect imports of this package do not
// break the Windows build.
func New(cfg transcribe.Config) (*Backend, error) {
	return newPlatformBackend(cfg)
}

// NewFactory adapts New into the transcribe.Factory signature for the
// registry.
func NewFactory(cfg transcribe.Config) (transcribe.Transcriber, error) {
	return New(cfg)
}

// newBackendCommon is the platform-agnostic Backend constructor used
// by both the unix and the windows New variants. The unix variant
// passes a real spawnFn; the windows variant never reaches this
// helper because it errors out before constructing anything.
func newBackendCommon(cfg transcribe.Config, serverPath, modelPath string, spawn spawnFn) *Backend {
	client := cfg.HTTPClient
	if client == nil {
		// cfg.Timeout is only relevant on the nil-client path: we're
		// constructing the default HTTP client. When the caller
		// supplies a ready-made client, it already owns its own
		// timeout policy and cfg.Timeout is documented as ignored.
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultRequestTimeout
		}
		client = &http.Client{Timeout: timeout}
	}
	closeCtx, closeCancel := context.WithCancel(context.Background())
	return &Backend{
		cfg:         cfg,
		serverPath:  serverPath,
		modelPath:   modelPath,
		language:    cfg.Language,
		threads:     cfg.WhisperThreads,
		useGPU:      cfg.WhisperUseGPU,
		client:      client,
		spawn:       spawn,
		closeCtx:    closeCtx,
		closeCancel: closeCancel,
	}
}

// resolveThreadCount maps the user-facing threads knob (0 = auto) to an
// explicit positive integer suitable for whisper-server's --threads
// flag. It is the single source of truth for the auto-count policy:
// runtime.NumCPU()/2 rounded up (ceiling) to at least 1. Splitting
// the parent process's logical CPUs in half leaves headroom for the
// rest of the pipeline (audio capture, transform, injection) and
// matches the "noticeable latency → instant" win the reporting user
// measured. Ceiling division ensures odd-count hosts (e.g. a 3-core
// VM) get the extra thread instead of being rounded down to 1.
//
// The helper is defined in the platform-agnostic file so both the
// unix spawn path and any future platform backend share one policy.
// It takes the raw CPU count as an argument so unit tests can force
// deterministic output instead of depending on runtime.NumCPU() on
// the test host.
func resolveThreadCount(requested, numCPU int) int {
	if requested > 0 {
		return requested
	}
	// Integer ceiling division: (numCPU + 1) / 2. Valid for
	// numCPU >= 0; numCPU=0 yields 0 which the guard below lifts
	// to 1. The clamp is also the minimum the whisper-server flag
	// accepts, so it is load-bearing rather than cosmetic.
	auto := (numCPU + 1) / 2
	if auto < 1 {
		return 1
	}
	return auto
}

// Transcribe spawns the subprocess on first use, POSTs the audio at
// the /inference endpoint, and emits the response as a single IsFinal
// chunk. The channel is closed exactly once.
//
// On a subprocess crash detected before the request, Transcribe
// respawns once and retries. A second failure is returned as the
// chunk's Err.
//
// Repeated terminal failures trip a circuit breaker: after
// circuitBreakerThreshold consecutive failures the next Transcribe
// call returns the breaker error immediately for circuitBreakerCooldown
// without fork-execing the subprocess. A successful transcription
// resets the breaker.
//
// opts.Prompt, when non-empty, is forwarded as the `prompt` multipart
// field to whisper-server's /inference endpoint so the local Whisper
// instance biases its token probabilities toward the supplied
// vocabulary. The same prompt is used on the retry path.
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader, opts transcribe.Options) (<-chan transcribe.TranscriptChunk, error) {
	if audio == nil {
		return nil, errors.New("whisperlocal: audio reader is nil")
	}
	out := make(chan transcribe.TranscriptChunk, 1)

	// Read the audio synchronously so a retry path can re-POST the
	// same bytes without rewinding the reader. Audio for a single
	// hold-to-talk press is small (16 kHz mono 16-bit ~= 32 KB/s) so
	// the in-memory buffer is the right shape.
	wavData, err := io.ReadAll(audio)
	if err != nil {
		go func() {
			defer close(out)
			emit(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.language,
				Err:      fmt.Errorf("whisperlocal: read audio: %w", err),
			})
		}()
		return out, nil
	}
	if len(wavData) == 0 {
		go func() {
			defer close(out)
			emit(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.language,
				Err:      errors.New("whisperlocal: audio data is empty"),
			})
		}()
		return out, nil
	}

	// Circuit-breaker fast path: a recently-broken backend short-
	// circuits before any subprocess interaction so a wedged
	// install does not fork-exec on every recording.
	if breakerErr := b.checkBreaker(); breakerErr != nil {
		go func() {
			defer close(out)
			emit(ctx, out, transcribe.TranscriptChunk{
				IsFinal:  true,
				Language: b.language,
				Err:      breakerErr,
			})
		}()
		return out, nil
	}

	// Capture the per-call prompt locally so the retry path uses the
	// exact same value even if the caller mutates opts after return.
	prompt := opts.Prompt

	// Track this in-flight call so Close can drain it before
	// tearing the subprocess down.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer close(out)

		// reqCtx links the caller's ctx to the backend's
		// closeCtx so a Close mid-flight aborts the HTTP POST
		// as context.Canceled instead of leaking ECONNREFUSED
		// from the torn-down subprocess.
		reqCtx, cancel := mergeContexts(ctx, b.closeCtx)
		defer cancel()

		text, err := b.transcribeOnce(reqCtx, wavData, prompt)
		if err != nil && b.shouldRetry(err) {
			// One retry: respawn the subprocess and try again.
			b.killProc()
			text, err = b.transcribeOnce(reqCtx, wavData, prompt)
		}
		if err != nil {
			b.recordFailure()
		} else {
			b.recordSuccess()
		}
		emit(ctx, out, transcribe.TranscriptChunk{
			Text:     text,
			IsFinal:  true,
			Language: b.language,
			Err:      err,
		})
	}()
	return out, nil
}

// transcribeOnce ensures the subprocess is running, POSTs the audio,
// and returns the transcribed text or an error. It does NOT retry on
// its own — the retry decision lives one level up in Transcribe.
// prompt is the per-call Whisper prompt forwarded by the caller; the
// same value is used on the retry path so retries bias identically to
// the first attempt.
func (b *Backend) transcribeOnce(ctx context.Context, wavData []byte, prompt string) (string, error) {
	baseURL, err := b.ensureServer(ctx)
	if err != nil {
		return "", err
	}
	return b.postInference(ctx, baseURL, wavData, prompt)
}

// shouldRetry decides whether an error from the subprocess is worth a
// single retry. Connection failures (subprocess crashed between calls)
// and 5xx HTTP responses qualify; 4xx and validation errors do not.
//
// Context cancellation (caller-initiated or close-initiated) does
// not retry — that would mask the user's intent.
func (b *Backend) shouldRetry(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Network-level failure to the localhost subprocess: respawn.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// Pipe broken / EOF / connect refused.
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, io.EOF) {
		return true
	}
	// Server-reported 5xx.
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.statusCode >= 500
	}
	return false
}

// checkBreaker is the circuit-breaker fast path. It returns a non-nil
// error if Transcribe should short-circuit without touching the
// subprocess.
func (b *Backend) checkBreaker() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return newBackendClosedError()
	}
	if !b.brokenUntil.IsZero() && time.Now().Before(b.brokenUntil) {
		return fmt.Errorf(
			"whisperlocal: backend temporarily broken due to repeated failures; retry after %s",
			b.brokenUntil.Format(time.RFC3339))
	}
	return nil
}

// recordFailure is called from Transcribe after the retry path has
// also failed. It increments the consecutive-failure counter and
// trips the breaker if the threshold is reached.
func (b *Backend) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failCount++
	if b.failCount >= circuitBreakerThreshold {
		b.brokenUntil = time.Now().Add(circuitBreakerCooldown)
		b.failCount = 0
	}
}

// recordSuccess is called from Transcribe after a successful
// transcription. It resets the breaker so the next failure starts
// counting from zero.
func (b *Backend) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failCount = 0
	b.brokenUntil = time.Time{}
}

// ensureServer returns the base URL of a running whisper-server,
// spawning one if needed. It is safe to call concurrently.
//
// The mutex is held only for the spawn-vs-reuse decision; the
// expensive subprocess startup happens without the lock so a 30s
// model load does not serialise every other Transcribe call. The
// in-progress sentinel (b.spawning) coordinates concurrent callers:
// the first goroutine creates the channel, runs the spawn, then
// closes the channel; the rest wait on it without holding the
// mutex.
func (b *Backend) ensureServer(ctx context.Context) (string, error) {
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return "", newBackendClosedError()
		}

		// Detect a dead subprocess and clear the slot so the
		// spawn-or-reuse decision below sees nil.
		if b.proc != nil {
			select {
			case <-b.proc.waitDone:
				b.proc = nil
			default:
			}
		}

		if b.proc != nil {
			url := b.proc.baseURL
			b.mu.Unlock()
			return url, nil
		}

		// Another goroutine is mid-spawn — wait for it without
		// holding the mutex, then loop and re-check.
		if b.spawning != nil {
			ch := b.spawning
			b.mu.Unlock()
			select {
			case <-ch:
				// Loop and re-evaluate. The spawner may
				// have published a proc, in which case
				// we reuse; or it may have failed, in
				// which case we attempt our own spawn.
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}

		// We are the spawner. Publish the sentinel and release
		// the mutex before doing the expensive work.
		b.spawning = make(chan struct{})
		b.mu.Unlock()

		proc, spawnErr := b.spawn(ctx, b)

		b.mu.Lock()
		// Always close + clear the sentinel so waiting
		// goroutines unblock regardless of success or failure.
		close(b.spawning)
		b.spawning = nil

		// If Close raced us, throw away the freshly-spawned
		// proc so we honor the close intent.
		if b.closed {
			b.mu.Unlock()
			if spawnErr == nil && proc != nil {
				terminateProc(proc)
			}
			return "", newBackendClosedError()
		}

		if spawnErr != nil {
			b.mu.Unlock()
			return "", spawnErr
		}
		b.proc = proc
		url := proc.baseURL
		b.mu.Unlock()
		return url, nil
	}
}

// killProc tears down the current subprocess (if any) without locking
// the parent's exit path. It is the retry helper: if a request fails
// because the subprocess is dead, killProc clears the slot so
// ensureServer respawns on the next call.
func (b *Backend) killProc() {
	b.mu.Lock()
	proc := b.proc
	b.proc = nil
	b.mu.Unlock()
	if proc == nil {
		return
	}
	terminateProc(proc)
}

// Close terminates the subprocess and prevents further use. It is
// idempotent: a second call is a no-op. Close blocks until in-flight
// Transcribe calls drain (bounded by closeDrainTimeout) and then until
// cmd.Wait returns or the SIGTERM/SIGKILL grace period elapses.
//
// Close returns nil on the expected shutdown path (SIGTERM/SIGKILL).
// A non-nil error indicates the subprocess died of something other
// than our shutdown signal — useful for the daemon's audit log.
//
// Shutdown sequence:
//
//  1. Toggle b.closed so future Transcribe calls return immediately.
//  2. Cancel b.closeCtx so any in-flight HTTP request aborts as
//     context.Canceled instead of seeing the subprocess vanish.
//  3. Wait for in-flight Transcribe goroutines to drain via b.wg.
//     Bounded by closeDrainTimeout so a hung request cannot pin
//     daemon shutdown forever.
//  4. SIGTERM the subprocess and wait shutdownGraceful for it to
//     exit; SIGKILL if it does not.
func (b *Backend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	proc := b.proc
	b.proc = nil
	b.mu.Unlock()

	// Step 2: cancel any in-flight HTTP request so it aborts
	// cleanly before we kill the subprocess.
	b.closeCancel()

	// Step 3: wait for the in-flight goroutines to drain. The
	// bounded wait prevents a hung HTTP request from pinning
	// shutdown indefinitely.
	if !waitWithTimeout(&b.wg, closeDrainTimeout) {
		slog.Default().Warn(
			"whisperlocal: in-flight transcribe goroutines did not drain within timeout; proceeding with subprocess teardown",
			"timeout", closeDrainTimeout,
		)
	}

	if proc == nil {
		return nil
	}
	// Step 4: tear down the subprocess.
	terminateProc(proc)
	return closeError(proc)
}

// waitWithTimeout waits for wg.Wait to complete or for the timeout
// to elapse. Returns true if the WaitGroup drained, false if the
// timeout fired first. The bounded variant exists because we need
// to surface a warning on shutdown timeout without taking a hard
// dependency on a specific logger here.
func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// mergeContexts returns a context that cancels when either parent
// cancels. The returned cancel function releases the goroutine that
// watches the parents.
func mergeContexts(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	go func() {
		select {
		case <-b.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// terminateProc sends SIGTERM, waits up to shutdownGraceful for the
// process to exit, then SIGKILLs it. The waitDone channel is closed by
// the goroutine started in spawnWhisperServer so we can observe the
// exit without re-calling cmd.Wait (which would race the goroutine).
//
// SIGTERM-on-shutdown is the expected exit path; the cmd.Wait error
// it produces (exit status 143 = 128+SIGTERM) is intentionally
// swallowed by Backend.Close so the daemon does not log a warning on
// every clean shutdown.
func terminateProc(proc *serverProc) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	pid := proc.cmd.Process.Pid
	terminate(
		proc.waitDone,
		func() { _ = proc.cmd.Process.Signal(syscall.SIGTERM) },
		func() { _ = proc.cmd.Process.Kill() },
		pid,
	)
}

// terminate is the testable core of terminateProc. It takes a
// waitDone channel (closed when the process has actually exited) and
// two side-effect hooks for SIGTERM and SIGKILL so the control flow
// can be exercised in unit tests without a real subprocess.
//
// Both the SIGTERM grace window and the post-SIGKILL reap are
// bounded. If the kernel has wedged the process (uninterruptible
// sleep / D-state), an unbounded <-waitDone on the Kill path would
// block daemon shutdown forever. The worst case with the bound in
// place is a detached zombie — strictly better than a hung daemon.
// This is the pkg-yap review C6 guarantee.
func terminate(waitDone <-chan struct{}, sigterm, sigkill func(), pid int) {
	sigterm()
	select {
	case <-waitDone:
		return
	case <-time.After(shutdownGraceful):
	}
	sigkill()
	select {
	case <-waitDone:
	case <-time.After(shutdownGraceful):
		slog.Default().Warn("whisperlocal: subprocess did not exit after SIGKILL; detaching", "pid", pid)
	}
}

// emit sends chunk on out unless ctx is cancelled first.
func emit(ctx context.Context, out chan<- transcribe.TranscriptChunk, chunk transcribe.TranscriptChunk) {
	select {
	case <-ctx.Done():
	case out <- chunk:
	}
}

// apiError is the structured form of a non-200 response from the
// whisper-server /inference endpoint. The status code is preserved so
// retry logic can branch on 5xx vs 4xx.
type apiError struct {
	statusCode int
	body       string
}

// Error implements the error interface.
func (e *apiError) Error() string {
	return fmt.Sprintf("whisperlocal: HTTP %d: %s", e.statusCode, e.body)
}

// postInference POSTs wavData as a multipart/form-data request to the
// subprocess's /inference endpoint and returns the decoded "text"
// field of the JSON response. prompt is the per-call Whisper prompt
// (from transcribe.Options.Prompt); it is forwarded as-is when
// non-empty.
func (b *Backend) postInference(ctx context.Context, baseURL string, wavData []byte, prompt string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("whisperlocal: build form file: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return "", fmt.Errorf("whisperlocal: write form file: %w", err)
	}
	if b.language != "" {
		if err := writer.WriteField("language", b.language); err != nil {
			return "", fmt.Errorf("whisperlocal: write language field: %w", err)
		}
	}
	if prompt != "" {
		if err := writer.WriteField("prompt", prompt); err != nil {
			return "", fmt.Errorf("whisperlocal: write prompt field: %w", err)
		}
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("whisperlocal: write response_format: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("whisperlocal: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/inference", body)
	if err != nil {
		return "", fmt.Errorf("whisperlocal: build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := b.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", fmt.Errorf("whisperlocal: request cancelled: %w", ctx.Err())
		}
		return "", fmt.Errorf("whisperlocal: POST /inference: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("whisperlocal: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", &apiError{statusCode: resp.StatusCode, body: string(respBody)}
	}

	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		// whisper-server occasionally returns plain text for some
		// formats; treat the whole body as the transcription rather
		// than failing the request.
		return string(bytes.TrimSpace(respBody)), nil
	}
	return decoded.Text, nil
}

// pickFreePort asks the kernel for an unused TCP port by binding to
// :0, reading back the assigned port, and immediately closing.
//
// There is a tiny race window between Close and the subprocess
// Listen, which is why spawnWhisperServer retries on bind failure
// (detectable via the pipeBuffer snapshot) up to spawnRetryAttempts
// times. The retry covers the rare case where the kernel hands the
// same port to a sibling process between our Listen+Close and the
// child's bind; bind-failure-then-respawn is the only safe path
// short of upstream support for fd handoff.
//
// Whisper-server does not support port-0 auto-bind (it literally
// listens on port 0, which is non-routable), so banner parsing for
// the assigned port is not an option here.
func pickFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("whisperlocal: net.Listen returned non-TCP address")
	}
	return addr.Port, nil
}

// waitForListening polls until the subprocess accepts a TCP
// connection on host:port, or until startupTimeout elapses, or until
// proc exits early. Returns nil on success.
//
// On failure the error includes the last several lines of the
// subprocess's stdout/stderr (via proc.pipes), so a model load
// failure or bind error surfaces in the wrapped error path rather
// than only in the daemon's stderr stream.
func waitForListening(ctx context.Context, host string, port int, proc *serverProc) error {
	deadline := time.Now().Add(startupTimeout)
	address := net.JoinHostPort(host, strconv.Itoa(port))
	for {
		// Subprocess died during startup — surface the wait
		// error along with the captured output.
		select {
		case <-proc.waitDone:
			return wrapStartupError(
				fmt.Errorf("whisperlocal: subprocess exited during startup: %w", proc.waitErr),
				proc,
			)
		case <-ctx.Done():
			return wrapStartupError(
				fmt.Errorf("whisperlocal: ctx cancelled during subprocess startup: %w", ctx.Err()),
				proc,
			)
		default:
		}

		conn, err := net.DialTimeout("tcp", address, startupPollInterval)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return wrapStartupError(
				fmt.Errorf("whisperlocal: subprocess did not start listening on %s within %s",
					address, startupTimeout),
				proc,
			)
		}
		time.Sleep(startupPollInterval)
	}
}

// wrapStartupError appends the subprocess's captured output (last
// pipeBufferLines lines) to the supplied error so users see the
// real diagnostic instead of a vague timeout.
func wrapStartupError(err error, proc *serverProc) error {
	if proc == nil || proc.pipes == nil {
		return err
	}
	lines := proc.pipes.Snapshot()
	if len(lines) == 0 {
		return err
	}
	return fmt.Errorf("%w\nlast subprocess output:\n%s", err, joinLines(lines))
}

// joinLines concatenates lines with newline separators. Used by
// wrapStartupError to assemble a multi-line error string without
// pulling in strings.Join (which is fine, but the explicit form
// keeps the dependency surface minimal).
func joinLines(lines []string) string {
	var b bytes.Buffer
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
	}
	return b.String()
}

// pipeBuffer is a fixed-capacity ring of subprocess stdout/stderr
// lines, with a tee to os.Stderr (prefixed) so the daemon's log
// stream still surfaces the output. Snapshot returns the current
// ring contents in chronological order.
//
// The ring is bounded so a chatty subprocess (verbose mode, debug
// builds) cannot grow the daemon's memory unbounded. Lines longer
// than pipeBufferLineBytes are truncated with a "..." suffix.
type pipeBuffer struct {
	mu      sync.Mutex
	lines   []string
	next    int
	full    bool
	tee     io.Writer
	prefix  string
	scratch bytes.Buffer
}

// newPipeBuffer constructs a pipeBuffer with the given tee target
// (typically os.Stderr) and prefix tag (typically
// "[whisper-server]"). Pass nil for tee to disable tee output.
func newPipeBuffer(tee io.Writer, prefix string) *pipeBuffer {
	return &pipeBuffer{
		lines:  make([]string, pipeBufferLines),
		tee:    tee,
		prefix: prefix,
	}
}

// Write satisfies io.Writer. It splits incoming bytes on newlines,
// appends each complete line to the ring, and tees a prefixed copy
// to the tee target. A trailing partial line is buffered until the
// next Write completes it.
func (p *pipeBuffer) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	written := len(b)
	for _, c := range b {
		if c == '\n' {
			p.flushLineLocked()
			continue
		}
		if p.scratch.Len() < pipeBufferLineBytes {
			p.scratch.WriteByte(c)
		}
	}
	return written, nil
}

// flushLineLocked moves the scratch buffer into the ring as a
// complete line and tees it. Caller must hold p.mu.
func (p *pipeBuffer) flushLineLocked() {
	line := p.scratch.String()
	if p.scratch.Len() >= pipeBufferLineBytes {
		line += "..."
	}
	p.scratch.Reset()
	p.lines[p.next] = line
	p.next = (p.next + 1) % len(p.lines)
	if p.next == 0 {
		p.full = true
	}
	if p.tee != nil {
		fmt.Fprintf(p.tee, "%s %s\n", p.prefix, line)
	}
}

// Snapshot returns the ring contents in chronological order. The
// returned slice is freshly allocated so callers cannot mutate the
// internal ring state.
func (p *pipeBuffer) Snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.full {
		out := make([]string, p.next)
		copy(out, p.lines[:p.next])
		// Include a trailing partial line if any bytes are
		// pending in scratch (e.g. process died mid-line).
		if p.scratch.Len() > 0 {
			out = append(out, p.scratch.String())
		}
		return out
	}
	out := make([]string, 0, len(p.lines)+1)
	out = append(out, p.lines[p.next:]...)
	out = append(out, p.lines[:p.next]...)
	if p.scratch.Len() > 0 {
		out = append(out, p.scratch.String())
	}
	return out
}

// containsBindFailure returns true if any line in the snapshot
// matches the whisper-server bind-failure banner. The exact text
// (`couldn't bind to server socket`) was extracted from the
// whisper.cpp upstream source.
func containsBindFailure(lines []string) bool {
	for _, line := range lines {
		if bytes.Contains([]byte(line), []byte("couldn't bind to server socket")) ||
			bytes.Contains([]byte(line), []byte("server listen failed")) {
			return true
		}
	}
	return false
}
