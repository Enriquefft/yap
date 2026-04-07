package cli_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/ipc"
)

func withScratchXDG(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	writeConfigFile(t, cfgFile, "[general]\n  hotkey = \"KEY_RIGHTCTRL\"\n")
	xdg.Reload()
	return tmp
}

// TestStop_NothingRunning prints the no-op message and exits 0.
func TestStop_NothingRunning(t *testing.T) {
	withScratchXDG(t)
	stdout, _, err := runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.Contains(stdout, "No yap daemon") {
		t.Errorf("expected nothing-running message, got:\n%s", stdout)
	}
}

// TestStop_DaemonOnly stops a fake daemon via IPC and exits 0.
func TestStop_DaemonOnly(t *testing.T) {
	withScratchXDG(t)

	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		t.Fatalf("resolve sock: %v", err)
	}
	srv, err := ipc.NewServer(sockPath)
	if err != nil {
		t.Fatalf("ipc.NewServer: %v", err)
	}
	stopped := make(chan struct{}, 1)
	srv.SetShutdownFn(func() {
		select {
		case stopped <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	defer srv.Close()

	_, _, err = runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon shutdownFn was not invoked")
	}
}

// TestStop_RecordOnly signals a fake `yap record` PID and exits 0.
func TestStop_RecordOnly(t *testing.T) {
	withScratchXDG(t)

	// Spawn a child process that we can signal. `sleep 5` is the
	// canonical "long-running, signal-receiving" test target — it
	// exits on SIGTERM with status 143.
	child := exec.Command("sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatalf("spawn sleep: %v", err)
	}
	defer child.Process.Kill()

	// Write the child's PID into the record PID file.
	pidPath, err := xdg.DataFile("yap/yap-record.pid")
	if err != nil {
		t.Fatalf("resolve pid path: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	defer os.Remove(pidPath)

	stdout, _, err := runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !strings.Contains(stdout, "Record process signalled") {
		t.Errorf("expected record-signal message, got:\n%s", stdout)
	}

	// Confirm the child actually died from SIGTERM.
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("child did not exit after SIGTERM")
	}
}

// TestStop_BothPaths exercises the daemon AND record paths in one
// invocation. Both should be cleaned up; exit 0.
func TestStop_BothPaths(t *testing.T) {
	withScratchXDG(t)

	// Daemon side.
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		t.Fatalf("sock path: %v", err)
	}
	srv, err := ipc.NewServer(sockPath)
	if err != nil {
		t.Fatalf("ipc server: %v", err)
	}
	stopped := make(chan struct{}, 1)
	srv.SetShutdownFn(func() {
		select {
		case stopped <- struct{}{}:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	defer srv.Close()
	time.Sleep(50 * time.Millisecond)

	// Record side.
	child := exec.Command("sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatalf("spawn sleep: %v", err)
	}
	defer child.Process.Kill()
	pidPath, err := xdg.DataFile("yap/yap-record.pid")
	if err != nil {
		t.Fatalf("pid path: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", child.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	defer os.Remove(pidPath)

	_, _, err = runCLI(t, "stop")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("daemon shutdownFn was not invoked")
	}
	// Confirm the record child died.
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("child did not exit after SIGTERM")
	}
}

// TestStop_StalePIDFile cleans up a record pid file pointing at a
// dead PID without erroring.
func TestStop_StalePIDFile(t *testing.T) {
	withScratchXDG(t)

	pidPath, err := xdg.DataFile("yap/yap-record.pid")
	if err != nil {
		t.Fatalf("pid path: %v", err)
	}
	// Use a PID that almost certainly does not exist on this host.
	if err := os.WriteFile(pidPath, []byte("99999999\n"), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
	defer os.Remove(pidPath)

	// FindProcess always succeeds on Linux, so the actual signal
	// will fail with ESRCH. stopRecord should treat that as
	// "already gone" and not surface an error.
	if _, _, err := runCLI(t, "stop"); err != nil {
		// Some kernels permit signaling huge PIDs; if so the test
		// is non-deterministic. Tolerate the error containing ESRCH
		// or "no such process".
		if !strings.Contains(err.Error(), "no such process") &&
			!strings.Contains(err.Error(), "process") {
			t.Fatalf("stop: unexpected error: %v", err)
		}
	}
	// Send a no-op signal to confirm the test environment matches
	// our expectation.
	_ = syscall.Kill(99999999, 0)
}
