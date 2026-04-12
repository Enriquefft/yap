package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
//
// The authoritative double-start guard lives in daemon.Run itself:
// it takes the exclusive flock on the daemon pidfile via
// pidfile.Acquire and fails loudly on contention. pidfile.Acquire is
// the single source of truth for "am I allowed to start?".
//
// As a UX improvement we run a non-destructive pidfile.IsLocked probe
// before forking the child. On a true positive the user gets a clean
// "already running (pid N)" error in milliseconds instead of waiting
// the full 3s startup deadline for the child's stderr tail. The probe
// is advisory: a false negative (stale pidfile with no flock holder)
// is reclaimed by Acquire in the child, and a race between probe and
// child-side Acquire still resolves correctly because the child fails
// loudly on its own flock contention.
func spawnDaemonChild(out io.Writer) error {
	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	if pidfile.IsLocked(pidPath) {
		if holder, rerr := pidfile.Read(pidPath); rerr == nil && holder > 0 {
			return fmt.Errorf(
				"listen: yap daemon already running (pid %d); stop it first with `yap stop`",
				holder)
		}
		return fmt.Errorf("listen: yap daemon already running; stop it first with `yap stop`")
	}

	handle, err := spawnFunc()
	if err != nil {
		return fmt.Errorf("listen: start daemon: %w", err)
	}
	defer handle.Release()

	// Readiness has two signals:
	//
	//   1. pidfile.IsLocked(pidPath) becomes true. This is the
	//      authoritative flock-based signal that the child has
	//      reached pidfile.Acquire inside daemon.Run and committed
	//      to being the owner. We use IsLocked rather than
	//      os.Stat(pidPath) because after the C2 fix to
	//      pidfile.Handle.Close, a previously-stopped daemon leaves
	//      its pidfile on disk with no flock holder — a bare Stat
	//      would declare success on the stale file without waiting
	//      for our new child.
	//
	//   2. os.Stat(sockPath) succeeds. The ipc.NewServer constructor
	//      os.Removes any stale socket before binding, so this is a
	//      fresh signal: the socket exists exactly when this child's
	//      IPC server is up.
	//
	// Both signals must fire before we declare success. The parent's
	// pre-spawn IsLocked check at line 136 guarantees no pre-existing
	// flock holder, so a transition to IsLocked=true inside the loop
	// can only be our child.
	deadline := time.Now().Add(3 * time.Second)
	pidReady, sockReady := false, false
	for time.Now().Before(deadline) {
		if !pidReady && pidfile.IsLocked(pidPath) {
			pidReady = true
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
			return fmt.Errorf("listen: daemon did not start within 3s (pidfile flock not acquired):\n%s", stderrTail)
		}
		return fmt.Errorf("listen: daemon did not start within 3s (pidfile flock not acquired)")
	}
	if stderrTail != "" {
		return fmt.Errorf("listen: daemon started but IPC socket not ready within 3s:\n%s", stderrTail)
	}
	return fmt.Errorf("listen: daemon started but IPC socket not ready within 3s")
}

// osSpawnDaemon is the production spawnFunc. It re-execs the current
// binary with YAP_DAEMON=1 set, detaches it via setsid, and routes
// the child's stderr to a real file so the readiness loop can quote
// the tail on a timeout failure AND the daemon survives once this
// parent exits.
//
// Child stderr is redirected to pidfile.DaemonLogPath via an *os.File
// passed on cmd.Stderr. Go's exec package detects *os.File and dups
// its fd directly into the child at fork+exec instead of routing
// writes through an in-memory pipe + parent goroutine. This matters
// because the `yap listen` (non-foreground) flow spawns the daemon,
// waits for the readiness signals, then RETURNS — at which point the
// parent Go process exits and every parent-owned goroutine dies. If
// the child's stderr were a pipe backed by a parent goroutine, the
// pipe's read end would close on parent exit and the daemon's next
// stderr write would trigger SIGPIPE, which Go's runtime converts
// into an immediate process exit for any write on fd 1 or 2. The
// daemon would die silently before it serviced its first hotkey
// event — same failure mode that the toggle→record spawn hit. The
// file-backed fd survives parent exit because the child keeps its
// own independent open fd after the fork+exec dup.
//
// The readiness-failure paths read back the log file tail via
// readDaemonLogTail so the operator still sees the daemon's
// diagnostic on "did not start within 3s" errors.
func osSpawnDaemon() (spawnHandle, error) {
	logPath, err := pidfile.DaemonLogPath()
	if err != nil {
		return nil, fmt.Errorf("resolve daemon log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir daemon log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon log: %w", err)
	}

	childCmd, err := daemonChildCommand()
	if err != nil {
		_ = logFile.Close()
		return nil, err
	}

	// Detach from parent: new session, no controlling terminal.
	childCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	childCmd.Stdin = nil
	childCmd.Stdout = nil
	childCmd.Stderr = logFile

	if err := childCmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	// The parent's write-end handle is redundant once Start dups the
	// fd into the child. Close it here so the parent does not hold
	// an extra fd open for the lifetime of the process.
	_ = logFile.Close()

	return &osSpawnHandle{cmd: childCmd, logPath: logPath}, nil
}

// daemonChildCommand builds the *exec.Cmd that osSpawnDaemon forks
// into the detached daemon child. The package-level function-valued
// variable exists as a test seam: without it, unit tests that drive
// osSpawnDaemon end-to-end would re-exec the go-test binary with
// YAP_DAEMON=1, which the test binary would not recognise and would
// instead re-run the entire test suite — a recursive fork that never
// terminates. Tests override daemonChildCommand with a fast-exiting
// stub (e.g. /bin/true) so the file-descriptor plumbing is still
// exercised without the recursion. Production always goes through
// defaultDaemonChildCommand, which resolves os.Executable and sets
// YAP_DAEMON=1 in the child's environment so cmd/yap/main.go routes
// into daemon.Run directly.
var daemonChildCommand = defaultDaemonChildCommand

func defaultDaemonChildCommand() (*exec.Cmd, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "YAP_DAEMON=1")
	return cmd, nil
}

// osSpawnHandle wraps the live exec.Cmd plus its stderr log path so
// spawnDaemonChild can read the diagnostic tail through the
// spawnHandle interface without keeping a parent fd alive.
type osSpawnHandle struct {
	cmd     *exec.Cmd
	logPath string
}

func (h *osSpawnHandle) Stderr() string { return readDaemonLogTail(h.logPath) }

func (h *osSpawnHandle) Release() {
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Release()
	}
}

// readDaemonLogTail returns the last daemonLogTailBytes bytes of the
// daemon log, or an empty string if the file cannot be read. The
// readiness-failure paths call this to quote the child's diagnostic
// back to the user. Bounded reads prevent a pathological log from
// blowing up the error message.
func readDaemonLogTail(path string) string {
	const daemonLogTailBytes = 8192
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > daemonLogTailBytes {
		data = data[len(data)-daemonLogTailBytes:]
	}
	return strings.TrimRight(string(data), "\x00\n") + "\n"
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
