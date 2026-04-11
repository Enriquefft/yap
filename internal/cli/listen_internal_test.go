package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/pidfile"
)

// withScratchRuntimeDir installs a fresh XDG_RUNTIME_DIR pointing at
// a test-owned tmpfs-equivalent so pidfile.DaemonPath and friends
// resolve into the temp tree. Tests that only redirect XDG_DATA_HOME
// will still see the real /run/user/UID, which is wrong since the
// Bug 5 & 6 migration moved every runtime file under XDG_RUNTIME_DIR.
func withScratchRuntimeDir(t *testing.T, tmp string) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "run"))
	if err := os.MkdirAll(filepath.Join(tmp, "run"), 0o700); err != nil {
		t.Fatal(err)
	}
	xdg.Reload()
}

// fakeSpawnHandle is a stub spawnHandle that lets each test decide
// whether the simulated daemon "succeeds" (acquires the pidfile flock
// and creates the socket file synchronously) or "fails" (writes a
// diagnostic to its stderr buffer and never touches the filesystem).
//
// The handle owns a live pidfile.Handle when the fake simulates a
// successfully-started daemon so that listen.go's IsLocked readiness
// probe observes a real flock holder — mirroring what a real child
// daemon does inside pidfile.Acquire. Release closes the fake
// daemon's handle, releasing the flock; without this the handle
// would persist across tests and poison subsequent listen probes.
type fakeSpawnHandle struct {
	stderr string
	holder *pidfile.Handle
}

func (f *fakeSpawnHandle) Stderr() string { return f.stderr }
func (f *fakeSpawnHandle) Release() {
	if f.holder != nil {
		_ = f.holder.Close()
		f.holder = nil
	}
}

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
//
// XDG_RUNTIME_DIR is set explicitly (and created) because the bug 5/6
// migration moved every yap runtime file (pidfiles, IPC socket) from
// $XDG_DATA_HOME to $XDG_RUNTIME_DIR. Tests that only redirect DATA
// would otherwise hit the real /run/user/UID and collide with a
// production daemon.
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
	runtimeDir := filepath.Join(tmp, "run")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
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
		// Simulate an instant daemon: take the pidfile flock via
		// the real Acquire so listen.go's IsLocked readiness probe
		// observes a live holder, then drop a placeholder socket
		// file so the os.Stat readiness leg also fires.
		if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
			return nil, err
		}
		holder, herr := pidfile.Acquire(pidPath)
		if herr != nil {
			return nil, herr
		}
		if err := os.WriteFile(sockPath, []byte{}, 0o600); err != nil {
			_ = holder.Close()
			return nil, err
		}
		return &fakeSpawnHandle{holder: holder}, nil
	})
	t.Cleanup(func() {
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

// TestSpawnDaemonChild_AlreadyRunningRefused covers the double-start
// guard: when another process holds the daemon pidfile flock,
// spawnDaemonChild must refuse before forking. We simulate the live
// holder in-process by taking our own pidfile.Acquire on the same
// path and keeping it held for the duration of the test — flock()
// locks are per-OFD on Linux, so a second open() from the same process
// sees the lock as contended, exactly like a real second yap listen.
//
// The test exercises the UX pre-check in spawnDaemonChild (via
// pidfile.IsLocked), which is advisory — the authoritative guard
// lives in daemon.Run's pidfile.Acquire call.
func TestSpawnDaemonChild_AlreadyRunningRefused(t *testing.T) {
	withScratchListenEnv(t)

	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		t.Fatalf("daemon path: %v", err)
	}
	holder, err := pidfile.Acquire(pidPath)
	if err != nil {
		t.Fatalf("acquire holder: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })

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
