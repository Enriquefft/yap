//go:build !windows

package whisperlocal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// newPlatformBackend is the unix implementation of the Backend
// constructor. It performs the static validation (binary discovery,
// model resolution) that is common to every whisperlocal backend and
// then wires the platform-specific defaultSpawn into the resulting
// struct.
func newPlatformBackend(cfg transcribe.Config) (*Backend, error) {
	serverPath, err := discoverServer(cfg)
	if err != nil {
		return nil, err
	}
	modelPath, err := resolveModel(cfg)
	if err != nil {
		return nil, err
	}
	return newBackendCommon(cfg, serverPath, modelPath, spawnWhisperServer), nil
}

// spawnWhisperServer starts a real whisper-server child process bound
// to a free localhost port and waits until the port accepts
// connections. This is the production spawn function; tests substitute
// b.spawn with a fake.
//
// On a recoverable bind failure ("address already in use") the spawn
// is retried up to spawnRetryAttempts times. The failure is detected
// via the captured pipeBuffer output rather than exit status because
// whisper-server exits 0 even after printing the bind-failure banner.
//
// A hard (non-bind-failure) failure or exhaustion of the retry budget
// returns a wrapped error containing the last pipeBufferLines of
// subprocess output so the caller sees the real diagnostic.
func spawnWhisperServer(ctx context.Context, b *Backend) (*serverProc, error) {
	var lastErr error
	for attempt := 0; attempt < spawnRetryAttempts; attempt++ {
		proc, err := spawnWhisperServerOnce(ctx, b)
		if err == nil {
			return proc, nil
		}
		lastErr = err
		// Retry only on bind failure. Every other class of
		// error is permanent (binary missing, model broken,
		// model load OOM) and retrying buys nothing.
		if !isBindFailureError(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf(
		"whisperlocal: spawn failed after %d bind-failure retries: %w",
		spawnRetryAttempts, lastErr)
}

// spawnWhisperServerOnce performs exactly one spawn attempt: pick a
// port, fork-exec whisper-server, wait for readiness, and return the
// serverProc on success. Any failure tears down the child before
// returning so the caller can retry without leaking a process.
func spawnWhisperServerOnce(ctx context.Context, b *Backend) (*serverProc, error) {
	port, err := pickFreePort()
	if err != nil {
		return nil, fmt.Errorf("whisperlocal: pick port: %w", err)
	}

	args := []string{
		"--model", b.modelPath,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
	}
	if b.language != "" {
		args = append(args, "--language", b.language)
	}

	// We deliberately use context.Background here rather than the
	// caller's ctx because the subprocess outlives a single
	// Transcribe call. Backend.Close (and the daemon's deferred
	// shutdown) is the canonical way to terminate it.
	cmd := exec.CommandContext(context.Background(), b.serverPath, args...)

	// Capture stdout/stderr into a bounded ring buffer with a tee
	// to os.Stderr. The ring feeds wrapStartupError so startup
	// failures carry the real subprocess diagnostic; the tee keeps
	// the daemon's existing log stream populated for operators who
	// are actively watching journalctl.
	pipes := newPipeBuffer(os.Stderr, "[whisper-server]")
	cmd.Stdout = pipes
	cmd.Stderr = pipes

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
		pipes:    pipes,
	}
	go func() {
		proc.waitErr = cmd.Wait()
		close(proc.waitDone)
	}()

	if err := waitForListening(ctx, "127.0.0.1", port, proc); err != nil {
		// Tear down the half-started child before returning. Both
		// the SIGTERM grace and the post-SIGKILL reap are bounded so
		// a wedged kernel state cannot hang startup recovery.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-proc.waitDone:
		case <-time.After(shutdownGraceful):
			_ = cmd.Process.Kill()
			select {
			case <-proc.waitDone:
			case <-time.After(shutdownGraceful):
				slog.Default().Warn("whisperlocal: startup teardown could not reap subprocess; detaching")
			}
		}
		// Re-classify as a bind-failure if the captured
		// output shows the upstream bind banner so the
		// retry loop can distinguish "port taken" from
		// "model file broken".
		return nil, wrapSpawnError(err, proc)
	}
	return proc, nil
}

// bindFailureError wraps a waitForListening error produced by a
// bind failure so the spawn-retry loop can identify it without
// string-matching the outer error text.
type bindFailureError struct {
	inner error
}

func (e *bindFailureError) Error() string { return e.inner.Error() }
func (e *bindFailureError) Unwrap() error { return e.inner }

// isBindFailureError reports whether err was produced by a
// whisper-server bind failure. The signal is the captured subprocess
// output: bindFailureError is only produced when pipeBuffer shows
// the upstream banner.
func isBindFailureError(err error) bool {
	var be *bindFailureError
	return errors.As(err, &be)
}

// wrapSpawnError re-classifies a waitForListening error as a
// bind-failure when the subprocess pipeBuffer shows the
// upstream bind banner. This is invoked inline in
// spawnWhisperServerOnce via a small shim so the retry loop can
// observe the right error class without the subprocess writer
// having to learn about errors at all.
func wrapSpawnError(err error, proc *serverProc) error {
	if err == nil || proc == nil || proc.pipes == nil {
		return err
	}
	if containsBindFailure(proc.pipes.Snapshot()) {
		return &bindFailureError{inner: err}
	}
	return err
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
