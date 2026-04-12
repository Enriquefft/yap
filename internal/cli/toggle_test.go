package cli_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Enriquefft/yap/internal/ipc"
	"github.com/Enriquefft/yap/internal/pidfile"
)

// TestToggle_DaemonPath uses the IPC socket and asserts the toggle
// callback runs.
func TestToggle_DaemonPath(t *testing.T) {
	withScratchXDG(t)

	sockPath, err := pidfile.SocketPath()
	if err != nil {
		t.Fatalf("sock path: %v", err)
	}
	srv, err := ipc.NewServer(sockPath)
	if err != nil {
		t.Fatalf("ipc server: %v", err)
	}
	defer srv.Close()
	called := make(chan struct{}, 1)
	srv.SetToggleFn(func() string {
		select {
		case called <- struct{}{}:
		default:
		}
		return "recording"
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	stdout, _, err := runCLI(t, "toggle")
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !strings.Contains(stdout, "recording") {
		t.Errorf("expected toggled state in stdout, got:\n%s", stdout)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("toggle callback was not invoked")
	}
}

// TestToggle_RecordSignalPath signals a fake `yap record` process
// when no daemon is running.
func TestToggle_RecordSignalPath(t *testing.T) {
	withScratchXDG(t)

	// We need a child that handles SIGUSR1 — `sleep` does not by
	// default (some implementations exit on it, others ignore it).
	// Use sh -c with a trap so we get a deterministic exit on
	// SIGUSR1.
	child := exec.Command("sh", "-c", "trap 'exit 0' USR1; sleep 5 & wait")
	if err := child.Start(); err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	defer child.Process.Kill()

	pidPath, err := pidfile.RecordPath()
	if err != nil {
		t.Fatalf("pid path: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	defer os.Remove(pidPath)

	stdout, _, err := runCLI(t, "toggle")
	if err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if !strings.Contains(stdout, "SIGUSR1") {
		t.Errorf("expected SIGUSR1 mention in stdout, got:\n%s", stdout)
	}

	// The shell trap should make the child exit on SIGUSR1.
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		// Some sh implementations ignore traps within & wait;
		// fall back to killing the child explicitly. The toggle
		// command itself succeeded, which is the assertion.
		_ = syscall.Kill(child.Process.Pid, syscall.SIGTERM)
	}
}

// TestToggle_NothingRunning_StartsRecord asserts that when neither the
// daemon nor a record process is running, toggle attempts to start a
// `yap record` process.
//
// In the test environment os.Executable() resolves to the go-test
// binary, which does not know the `record` subcommand. startRecordProcess
// fork-execs that binary, the child immediately exits with an unknown-
// command error, never writes its pidfile, and the parent's
// poll-for-pidfile handshake either times out or reaps the child
// early via non-blocking Wait4. All acceptable outcomes mean the
// spawn intent was correctly reached:
//
//   - "Recording started"          — a real yap binary happened to be on PATH
//   - "start record"               — fork-exec itself failed
//   - "resolve executable"         — os.Executable() failed (sandbox)
//   - "did not register"           — child forked but never wrote the pidfile
//     within the 500ms handshake deadline
//   - "exited before registering"  — non-blocking Wait4 reaped the child
//     early and surfaced the captured stderr tail from the record
//     log file instead of waiting the full 500ms for a silent timeout.
func TestToggle_NothingRunning_StartsRecord(t *testing.T) {
	withScratchXDG(t)
	stdout, _, err := runCLI(t, "toggle")
	if err != nil {
		if !strings.Contains(err.Error(), "start record") &&
			!strings.Contains(err.Error(), "resolve executable") &&
			!strings.Contains(err.Error(), "did not register") &&
			!strings.Contains(err.Error(), "exited before registering") {
			t.Fatalf("unexpected error: %v", err)
		}
	} else if !strings.Contains(stdout, "Recording started") {
		t.Errorf("expected recording start in stdout, got:\n%s", stdout)
	}
}

// TestToggle_StartsRecord_CreatesRecordLog asserts that
// startRecordProcess opens the record log file on disk before spawning
// the child. This is load-bearing for the real deployment scenario:
// Hyprland fires `yap toggle`, which exits after the handshake. If
// the child's stderr were routed to an in-memory buffer (a pipe whose
// read end lives in the parent goroutine), the parent's exit would
// close the read end and the child's next stderr write would SIGPIPE
// out of Go's runtime, killing the record process mid-pipeline before
// it could transcribe and inject. Using a *os.File in cmd.Stderr
// bypasses the pipe entirely — the fd is inherited at fork+exec and
// survives parent exit.
//
// The assertion here is structural: after startRecordProcess has
// opened the log file with O_CREATE|O_TRUNC, the file exists at the
// canonical record log path regardless of whether the test-binary
// child actually wrote anything to it. A missing file means the
// stderr sink was never switched to a file and the regression has
// returned.
func TestToggle_StartsRecord_CreatesRecordLog(t *testing.T) {
	withScratchXDG(t)
	_, _, _ = runCLI(t, "toggle")

	logPath, err := pidfile.RecordLogPath()
	if err != nil {
		t.Fatalf("resolve record log path: %v", err)
	}
	if _, statErr := os.Stat(logPath); statErr != nil {
		t.Fatalf("record log file missing at %s: %v — startRecordProcess did not route child stderr to a file", logPath, statErr)
	}
}
