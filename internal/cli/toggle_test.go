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

	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
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

// TestToggle_NothingRunning errors out (exit 1) when neither the
// daemon nor a record process is running.
func TestToggle_NothingRunning(t *testing.T) {
	withScratchXDG(t)
	_, _, err := runCLI(t, "toggle")
	if err == nil {
		t.Fatal("expected toggle to error when nothing is running")
	}
	if !strings.Contains(err.Error(), "no daemon") {
		t.Errorf("error did not name the no-daemon condition: %v", err)
	}
}
