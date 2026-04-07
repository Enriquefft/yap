package whisperlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
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
)

// Backend is the whisperlocal implementation of transcribe.Transcriber.
// It owns the lifecycle of one whisper-server child process across the
// lifetime of the parent yap process.
//
// The struct is safe for concurrent use: every public method that
// touches subprocess state acquires mu first.
type Backend struct {
	cfg        transcribe.Config
	serverPath string
	modelPath  string
	language   string
	client     *http.Client

	// spawn is the function that actually starts the subprocess.
	// Tests inject a fake that returns an httptest server URL and a
	// no-op cmd. The default in production is spawnWhisperServer.
	spawn func(ctx context.Context, b *Backend) (*serverProc, error)

	mu     sync.Mutex
	proc   *serverProc
	closed bool
}

// serverProc holds the live subprocess handles. The waitDone channel
// is closed by a goroutine watching cmd.Wait so other goroutines can
// observe an early death without racing on cmd.ProcessState.
type serverProc struct {
	cmd      *exec.Cmd
	baseURL  string
	waitDone chan struct{}
	waitErr  error
}

// New constructs a Backend from cfg without spawning the subprocess.
// It validates the static configuration (binary discovery, model
// resolution, language) and returns a non-nil error if anything is
// missing. Runtime failures (subprocess crash, network) surface from
// Transcribe.
func New(cfg transcribe.Config) (*Backend, error) {
	serverPath, err := discoverServer(cfg)
	if err != nil {
		return nil, err
	}
	modelPath, err := resolveModel(cfg)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &Backend{
		cfg:        cfg,
		serverPath: serverPath,
		modelPath:  modelPath,
		language:   cfg.Language,
		client:     client,
		spawn:      spawnWhisperServer,
	}, nil
}

// NewFactory adapts New into the transcribe.Factory signature for the
// registry.
func NewFactory(cfg transcribe.Config) (transcribe.Transcriber, error) {
	return New(cfg)
}

// Transcribe spawns the subprocess on first use, POSTs the audio at
// the /inference endpoint, and emits the response as a single IsFinal
// chunk. The channel is closed exactly once.
//
// On a subprocess crash detected before the request, Transcribe
// respawns once and retries. A second failure is returned as the
// chunk's Err.
func (b *Backend) Transcribe(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
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

	go func() {
		defer close(out)

		text, err := b.transcribeOnce(ctx, wavData)
		if err != nil && b.shouldRetry(err) {
			// One retry: respawn the subprocess and try again.
			b.killProc()
			text, err = b.transcribeOnce(ctx, wavData)
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
func (b *Backend) transcribeOnce(ctx context.Context, wavData []byte) (string, error) {
	baseURL, err := b.ensureServer(ctx)
	if err != nil {
		return "", err
	}
	return b.postInference(ctx, baseURL, wavData)
}

// shouldRetry decides whether an error from the subprocess is worth a
// single retry. Connection failures (subprocess crashed between calls)
// and 5xx HTTP responses qualify; 4xx and validation errors do not.
func (b *Backend) shouldRetry(err error) bool {
	if err == nil {
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

// ensureServer returns the base URL of a running whisper-server,
// spawning one if needed. It is safe to call concurrently — the mutex
// guards both the spawn decision and the dead-process detection.
func (b *Backend) ensureServer(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return "", errors.New("whisperlocal: backend is closed")
	}

	// Detect a dead subprocess and clear the slot so the spawn-or-
	// reuse decision below sees nil.
	if b.proc != nil {
		select {
		case <-b.proc.waitDone:
			// Already dead — drop and respawn.
			b.proc = nil
		default:
		}
	}

	if b.proc != nil {
		return b.proc.baseURL, nil
	}

	proc, err := b.spawn(ctx, b)
	if err != nil {
		return "", err
	}
	b.proc = proc
	return proc.baseURL, nil
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
// idempotent: a second call is a no-op. Close blocks until cmd.Wait
// returns or the grace period elapses.
//
// Close returns nil on the expected shutdown path (SIGTERM/SIGKILL).
// A non-nil error indicates the subprocess died of something other
// than our shutdown signal — useful for the daemon's audit log.
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

	if proc == nil {
		return nil
	}
	terminateProc(proc)
	return closeError(proc)
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
	_ = proc.cmd.Process.Signal(syscall.SIGTERM)

	select {
	case <-proc.waitDone:
		return
	case <-time.After(shutdownGraceful):
	}
	_ = proc.cmd.Process.Kill()
	<-proc.waitDone
}

// closeError returns the wait error from the (now-terminated)
// subprocess unless it is the expected SIGTERM/SIGKILL exit, in which
// case nil is returned. The daemon's deferred Close path logs the
// returned error if non-nil; we want it to stay quiet on the happy
// path.
func closeError(proc *serverProc) error {
	if proc == nil {
		return nil
	}
	err := proc.waitErr
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// Linux exec.ExitError exposes ProcessState.Sys() as
		// syscall.WaitStatus. SIGTERM (15) and SIGKILL (9) are
		// our intentional shutdown signals.
		if status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			switch status.Signal() {
			case syscall.SIGTERM, syscall.SIGKILL:
				return nil
			}
		}
	}
	return err
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
// field of the JSON response.
func (b *Backend) postInference(ctx context.Context, baseURL string, wavData []byte) (string, error) {
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
	if b.cfg.Prompt != "" {
		if err := writer.WriteField("prompt", b.cfg.Prompt); err != nil {
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

// spawnWhisperServer starts a real whisper-server child process bound
// to a free localhost port and waits until the port accepts
// connections. This is the production spawn function; tests substitute
// b.spawn with a fake.
func spawnWhisperServer(ctx context.Context, b *Backend) (*serverProc, error) {
	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("whisperlocal: pick port: %w", err)
	}

	args := []string{
		"--model", b.modelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		// `--no-prints` would silence the model load banner; the
		// build of whisper-cpp on Nix accepts it but it's not
		// universal across distros, so we leave it off and let the
		// child write to its inherited stderr where it is harmless
		// in the daemon's stderr stream.
	}
	if b.language != "" {
		args = append(args, "--language", b.language)
	}

	// We deliberately use context.Background here rather than the
	// caller's ctx because the subprocess outlives a single
	// Transcribe call. Backend.Close (and the daemon's deferred
	// shutdown) is the canonical way to terminate it.
	cmd := exec.CommandContext(context.Background(), b.serverPath, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	// Detach from any session-wide signals so a Ctrl+C in a foreground
	// terminal does not race the parent's signal handler. The daemon
	// owns shutdown via Close.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("whisperlocal: start %s: %w", b.serverPath, err)
	}

	proc := &serverProc{
		cmd:      cmd,
		baseURL:  fmt.Sprintf("http://127.0.0.1:%d", port),
		waitDone: make(chan struct{}),
	}
	go func() {
		proc.waitErr = cmd.Wait()
		close(proc.waitDone)
	}()

	if err := waitForListening(ctx, "127.0.0.1", port, proc); err != nil {
		// Tear down the half-started child before returning.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-proc.waitDone:
		case <-time.After(shutdownGraceful):
			_ = cmd.Process.Kill()
			<-proc.waitDone
		}
		return nil, err
	}
	return proc, nil
}

// pickFreePort asks the kernel for an unused TCP port by binding to
// :0, reading back the assigned port, and immediately closing. There
// is a tiny race window between Close and the subprocess Listen — in
// practice this has not bitten anyone for localhost subprocesses, and
// the alternative (parsing the child's stderr banner) is fragile and
// version-dependent.
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
func waitForListening(ctx context.Context, host string, port int, proc *serverProc) error {
	deadline := time.Now().Add(startupTimeout)
	address := net.JoinHostPort(host, strconv.Itoa(port))
	for {
		// Subprocess died during startup — surface the wait error.
		select {
		case <-proc.waitDone:
			return fmt.Errorf("whisperlocal: subprocess exited during startup: %w", proc.waitErr)
		case <-ctx.Done():
			return fmt.Errorf("whisperlocal: ctx cancelled during subprocess startup: %w", ctx.Err())
		default:
		}

		conn, err := net.DialTimeout("tcp", address, startupPollInterval)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("whisperlocal: subprocess did not start listening on %s within %s",
				address, startupTimeout)
		}
		time.Sleep(startupPollInterval)
	}
}
