package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/pidfile"
)

// fakeSpawnHandle is a stub spawnHandle that lets each test decide
// whether the simulated daemon "succeeds" (creates the PID and
// socket files synchronously) or "fails" (writes a diagnostic to its
// stderr buffer and never touches the filesystem).
type fakeSpawnHandle struct {
	stderr   string
	released bool
}

func (f *fakeSpawnHandle) Stderr() string { return f.stderr }
func (f *fakeSpawnHandle) Release()       { f.released = true }

// withSpawnFunc swaps the package-level spawnFunc for the duration
// of the test. The cleanup is registered with t.Cleanup so a t.Fatal
// inside the test still restores production behavior.
func withSpawnFunc(t *testing.T, fn func() (spawnHandle, error)) {
	t.Helper()
	prev := spawnFunc
	spawnFunc = fn
	t.Cleanup(func() { spawnFunc = prev })
}

// withScratchListenEnv creates a fresh XDG layout, points YAP_CONFIG
// at a clean config file, and reloads xdg so subsequent
// pidfile.DaemonPath calls resolve into the temp tree.
func withScratchListenEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte("[general]\n  hotkey = \"KEY_RIGHTCTRL\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	xdg.Reload()
	return tmp
}

// TestSpawnDaemonChild_BannerWritesToCobraWriter is the C1
// regression: spawnDaemonChild must write the success banner through
// the supplied io.Writer, not os.Stdout. We stub spawnFunc with a
// fake that creates the PID and socket files synchronously, then
// inspect the captured buffer.
func TestSpawnDaemonChild_BannerWritesToCobraWriter(t *testing.T) {
	withScratchListenEnv(t)

	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		t.Fatalf("daemon path: %v", err)
	}
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		t.Fatalf("socket path: %v", err)
	}

	withSpawnFunc(t, func() (spawnHandle, error) {
		// Simulate an instant daemon: write the PID file and a
		// placeholder socket file before returning so the readiness
		// loop sees both immediately.
		if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
			return nil, err
		}
		if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
			return nil, err
		}
		return &fakeSpawnHandle{}, nil
	})
	t.Cleanup(func() {
		os.Remove(pidPath)
		os.Remove(sockPath)
	})

	var out bytes.Buffer
	if err := spawnDaemonChild(&out); err != nil {
		t.Fatalf("spawnDaemonChild: %v", err)
	}
	if !strings.Contains(out.String(), "Daemon started successfully") {
		t.Errorf("banner missing from cobra writer; got %q", out.String())
	}
}

// TestSpawnDaemonChild_TimeoutIncludesStderr is the S8 regression:
// when the daemon fails to start within 3s, the captured stderr
// must surface in the returned error so operators see why.
func TestSpawnDaemonChild_TimeoutIncludesStderr(t *testing.T) {
	withScratchListenEnv(t)

	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		t.Fatalf("daemon path: %v", err)
	}
	// Make sure the PID file does not exist — the readiness loop
	// must time out so we hit the stderr-tail branch.
	os.Remove(pidPath)

	const diagnostic = "fatal: model file missing"
	withSpawnFunc(t, func() (spawnHandle, error) {
		return &fakeSpawnHandle{stderr: diagnostic}, nil
	})

	var out bytes.Buffer
	err = spawnDaemonChild(&out)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not start within 3s") {
		t.Errorf("error did not include timeout phrase: %v", err)
	}
	if !strings.Contains(err.Error(), diagnostic) {
		t.Errorf("error did not include captured stderr diagnostic: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("no banner expected on failure; got %q", out.String())
	}
}

// TestSpawnDaemonChild_AlreadyRunningRefused covers the
// double-start guard: when the PID file points at a live process,
// spawnDaemonChild must refuse before forking. We seed the file
// with the test process's own PID — which is guaranteed to be live
// because the test is running.
func TestSpawnDaemonChild_AlreadyRunningRefused(t *testing.T) {
	withScratchListenEnv(t)

	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		t.Fatalf("daemon path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(pidPath) })

	called := false
	withSpawnFunc(t, func() (spawnHandle, error) {
		called = true
		return &fakeSpawnHandle{}, nil
	})

	var out bytes.Buffer
	err = spawnDaemonChild(&out)
	if err == nil {
		t.Fatal("expected already-running error")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error did not name the already-running condition: %v", err)
	}
	if called {
		t.Error("spawnFunc must not be called when daemon is already running")
	}
}
