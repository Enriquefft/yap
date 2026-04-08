package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/daemon"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/hybridz/yap/internal/platform"
	"github.com/spf13/cobra"
)

// newListenCmd builds the `yap listen` command. It is the primary way
// users start the yap daemon. By default it spawns a detached child
// process and returns once the child has written its PID file and
// opened its IPC socket. The child runs with YAP_DAEMON=1 set in its
// environment so cmd/yap/main.go routes it directly into daemon.Run.
//
// With --foreground the daemon runs in-process attached to the
// terminal. That mode is used by systemd, launchd, container
// entrypoints, and any supervisor that expects PID 1 / no-fork
// lifecycle.
func newListenCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "listen",
		Short: "start the yap daemon and listen for the hotkey",
		Long: `listen starts the yap background daemon. The daemon owns the
hotkey listener, the audio device, and (when local) the loaded
whisper model. Once running it stays invisible until you hold the
configured hotkey.

Use --foreground when running under systemd, launchd, a container,
or any process supervisor that expects a no-fork lifecycle.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListen(cfg, p, foreground, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false,
		"run the daemon in the foreground (no fork) — for systemd, launchd, containers")
	return cmd
}

// runListen is the shared handler for `yap listen`. It runs the
// first-run wizard if no config file exists, then either invokes
// daemon.Run in-process (foreground) or spawns a detached child that
// will re-exec with YAP_DAEMON=1.
func runListen(cfg *config.Config, p platform.Platform, foreground bool, out io.Writer) error {
	if needsWizard() {
		if err := runWizard(p); err != nil {
			return fmt.Errorf("listen: first-run setup: %w", err)
		}
		loaded, err := config.Load()
		if err != nil {
			return fmt.Errorf("listen: reload config after wizard: %w", err)
		}
		*cfg = loaded
	}

	if foreground {
		return daemon.Run(cfg, daemon.DefaultDeps(p))
	}
	return spawnDaemonChild(out)
}

// spawnHandle is the live child handle returned by spawnFunc. It
// owns the per-process stderr buffer plus a Release method that the
// readiness loop calls once it has consumed everything it needs from
// the child. Tests substitute spawnFunc to install a fake handle
// that simulates an instant daemon (writes the PID and socket files
// immediately) or a failing daemon (writes a diagnostic to stderr
// and exits without ever creating the PID file).
type spawnHandle interface {
	// Stderr returns the captured stderr the child has produced
	// since spawn. The returned string is a snapshot — readiness
	// failures call this once on the timeout path to enrich their
	// error message.
	Stderr() string
	// Release tells the handle the parent is done watching it. For
	// the production fork this calls os.Process.Release so the OS
	// can reap the daemon when it exits; for fakes it is a no-op.
	Release()
}

// spawnFunc is the indirection point for daemon child creation. The
// production value (osSpawnDaemon) execs the current binary with
// YAP_DAEMON=1 set; tests assign a fake that fabricates PID/socket
// files synchronously. Keeping the indirection at one variable
// preserves a single production code path — there is no test-only
// branch in spawnDaemonChild itself.
var spawnFunc = osSpawnDaemon

// spawnDaemonChild forks a detached child process running the yap
// binary with YAP_DAEMON=1 set. cmd/yap/main.go reads that env sentinel
// at startup and calls daemon.Run directly, bypassing cobra entirely
// so the user-visible CLI surface never shows a hidden flag.
//
// spawnDaemonChild blocks until either (a) the child's PID file and
// IPC socket both appear — meaning the daemon is ready — or (b) the
// 3s deadline expires, in which case it returns a descriptive error
// that includes any diagnostic the child wrote to stderr before
// failing. The success banner is written through the supplied io.Writer
// (the cobra command's OutOrStdout) so tests can capture it without
// touching os.Stdout.
func spawnDaemonChild(out io.Writer) error {
	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// DAEMON-05: Check if daemon is already running.
	if isLive, _ := pidfile.IsLive(pidPath); isLive {
		return fmt.Errorf("listen: yap is already running (PID file: %s)", pidPath)
	}

	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	handle, err := spawnFunc()
	if err != nil {
		return fmt.Errorf("listen: start daemon: %w", err)
	}
	defer handle.Release()

	deadline := time.Now().Add(3 * time.Second)
	pidReady, sockReady := false, false
	for time.Now().Before(deadline) {
		if !pidReady {
			if _, err := os.Stat(pidPath); err == nil {
				pidReady = true
			}
		}
		if !sockReady {
			if _, err := os.Stat(sockPath); err == nil {
				sockReady = true
			}
		}
		if pidReady && sockReady {
			fmt.Fprintln(out, "Daemon started successfully")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	stderrTail := handle.Stderr()
	if !pidReady {
		if stderrTail != "" {
			return fmt.Errorf("listen: daemon did not start within 3s (PID file not created):\n%s", stderrTail)
		}
		return fmt.Errorf("listen: daemon did not start within 3s (PID file not created)")
	}
	if stderrTail != "" {
		return fmt.Errorf("listen: daemon started but IPC socket not ready within 3s:\n%s", stderrTail)
	}
	return fmt.Errorf("listen: daemon started but IPC socket not ready within 3s")
}

// osSpawnDaemon is the production spawnFunc. It re-execs the current
// binary with YAP_DAEMON=1 set, detaches it via setsid, and routes
// the child's stderr through a thread-safe buffer so the readiness
// loop can quote it on a timeout failure.
func osSpawnDaemon() (spawnHandle, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}

	// Spawn detached daemon child. The YAP_DAEMON env sentinel is
	// picked up by cmd/yap/main.go BEFORE cobra parses os.Args, so
	// the child never sees any listen-specific flags.
	childCmd := exec.Command(self)
	childCmd.Env = append(os.Environ(), "YAP_DAEMON=1")

	// Detach from parent: new session, no controlling terminal.
	childCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	childCmd.Stdin = nil
	childCmd.Stdout = nil

	// Tee the child's stderr through a thread-safe buffer for the
	// duration of the readiness wait. If the daemon fails to start
	// within the deadline its diagnostic surfaces in the error
	// returned to the user instead of being silently lost.
	stderrBuf := &lockedBuffer{}
	childCmd.Stderr = stderrBuf

	if err := childCmd.Start(); err != nil {
		return nil, err
	}
	return &osSpawnHandle{cmd: childCmd, stderr: stderrBuf}, nil
}

// osSpawnHandle wraps the live exec.Cmd plus its stderr buffer so
// spawnDaemonChild can read both through the spawnHandle interface.
type osSpawnHandle struct {
	cmd    *exec.Cmd
	stderr *lockedBuffer
}

func (h *osSpawnHandle) Stderr() string { return h.stderr.String() }

func (h *osSpawnHandle) Release() {
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Release()
	}
}

// lockedBuffer is a small thread-safe wrapper around bytes.Buffer
// suitable for assignment to exec.Cmd.Stderr. The child process
// writes from its own goroutine while the parent reads from the
// readiness-wait loop, so unsynchronized access would race.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// needsWizard checks if the first-run wizard should be launched.
// Returns true if: no config file AND GROQ_API_KEY env var not set.
func needsWizard() bool {
	if os.Getenv("GROQ_API_KEY") != "" {
		return false
	}
	configPath, err := config.ConfigPath()
	if err != nil {
		// Can't determine config path — assume wizard needed.
		return true
	}
	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		return true
	}
	return false
}

// runWizard launches the interactive first-run wizard.
func runWizard(p platform.Platform) error {
	_, err := config.RunWizard(os.Stdin, os.Stdout, p.HotkeyCfg)
	if err != nil {
		return fmt.Errorf("wizard failed: %w", err)
	}
	configPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("get config path: %w", err)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not created: %s", configPath)
	}
	return nil
}
