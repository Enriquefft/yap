package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/spf13/cobra"
)

func newToggleCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "toggle",
		Short: "toggle yap recording (daemon IPC or `yap record` signal)",
		Long: `toggle starts or stops a recording cycle.

When the yap daemon is running, toggle uses IPC to flip its
recording state. When a standalone 'yap record' process is running
instead, toggle sends it SIGUSR1, which ends the recording cleanly
so the captured audio still flows through transcribe and inject.

If neither the daemon nor a record process is running, toggle
starts 'yap record' in the background. The next toggle invocation
stops it via SIGUSR1. This lets users bind 'yap toggle' to a
keybinding in their window manager without running the daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToggle(cmd.OutOrStdout())
		},
	}
}

// runToggle prefers the daemon IPC path. If no daemon socket exists,
// it falls back to signaling the standalone `yap record` process via
// SIGUSR1. If no record process is running either, it starts one in
// the background.
//
// Status messages are written to out so tests can capture them via
// the cobra command's writer; runToggle never touches os.Stdout.
func runToggle(out io.Writer) error {
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return fmt.Errorf("toggle: %w", err)
	}
	if _, err := os.Stat(sockPath); err == nil {
		// Daemon socket exists — use IPC.
		resp, err := ipc.Send(sockPath, ipc.CmdToggle, 5*time.Second)
		if err != nil {
			return fmt.Errorf("toggle: ipc: %w", err)
		}
		if !resp.Ok {
			return fmt.Errorf("toggle: daemon: %s", resp.Error)
		}
		fmt.Fprintf(out, "Toggle successful (state: %s)\n", resp.State)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("toggle: stat socket: %w", err)
	}

	// No daemon — try the standalone record process.
	pid, err := readRecordPID()
	if err != nil {
		return fmt.Errorf("toggle: record pid: %w", err)
	}
	if pid != 0 {
		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("toggle: locate record pid %d: %w", pid, err)
		}
		if err := proc.Signal(syscall.SIGUSR1); err != nil {
			if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
				removeRecordPID()
				// Stale PID — fall through to start a new recording.
			} else {
				return fmt.Errorf("toggle: signal record pid %d: %w", pid, err)
			}
		} else {
			fmt.Fprintf(out, "Toggle signalled record process %d (SIGUSR1)\n", pid)
			return nil
		}
	}

	// No daemon and no live record process — start one.
	return startRecordProcess(out)
}

// startRecordProcess spawns `yap record` as a detached background
// process, then polls the record pidfile until the child has
// acquired its flock and written its PID. This poll-until-ready
// handshake is what makes a rapid second `yap toggle` reliable: the
// second invocation only sees the pidfile once the child owns it,
// which means readRecordPID returns the real PID (never a stale
// zero-byte file pre-written by the parent).
//
// The handshake deadline is deliberately short (~500ms) because
// the child only needs to resolve paths, take the flock, and write
// its PID — none of which touches the audio device. If the child
// fails before that point, Wait() returns so the parent reports a
// clean "start record" error instead of hanging on the poll loop.
//
// Child stderr is redirected to a real file (*os.File passed via
// cmd.Stderr, which Go's exec package inherits into the child as a
// dup'd fd rather than wrapping in a pipe+goroutine). This is
// load-bearing: if we piped the child's stderr into an in-memory
// buffer, the parent goroutine holding the pipe's read end would die
// when this toggle process exits after the handshake, the read end
// would close, and the child's next stderr write would trigger
// SIGPIPE — which Go's runtime converts into a process exit for any
// write on fd 2. The record child would die mid-pipeline before it
// could transcribe and inject, leaving the user with "nothing
// happened" and an orphaned whisper-server. The file-backed fd
// survives parent exit because the child holds its own independent
// open fd after fork+exec, so record logs keep flowing into the file
// for the full pipeline duration.
//
// The handshake failure paths read the tail of the same file so the
// operator still sees the child's diagnostic on "exited before
// registering" and "did not register within 500ms" errors.
func startRecordProcess(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("toggle: resolve executable: %w", err)
	}

	logPath, err := pidfile.RecordLogPath()
	if err != nil {
		return fmt.Errorf("toggle: resolve record log path: %w", err)
	}
	// The parent of logPath (normally $XDG_RUNTIME_DIR/yap) is also
	// created by pidfile.RecordPath callers, but the first ever
	// invocation may race: if the toggle path runs before any
	// pidfile.Read/Write, the directory may not yet exist.
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return fmt.Errorf("toggle: mkdir record log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("toggle: open record log: %w", err)
	}
	// Close the parent's handle after Start. The child keeps its own
	// dup'd fd via fork+exec inheritance, so closing here does not
	// affect the child's writes. Diagnostics on the failure paths
	// re-open the file for reading.
	defer logFile.Close()

	cmd := exec.Command(exe, "record")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("toggle: start record: %w", err)
	}

	pidPath, err := pidfile.RecordPath()
	if err != nil {
		// The child is already running but we can't point the next
		// toggle at it — surface the failure loudly so the user can
		// stop the orphan with `pkill yap` rather than losing track
		// of it.
		return fmt.Errorf("toggle: resolve record pid path: %w", err)
	}

	// Poll for the child's pidfile. The child calls
	// acquireRecordPID() early in runRecord, so the file appears
	// within a few milliseconds on every platform yap supports.
	//
	// We also non-blockingly reap the child each iteration: if it
	// has already exited, the pidfile will never appear and the
	// operator deserves the child's stderr immediately instead of
	// the full 500ms timeout.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if pid, rerr := pidfile.Read(pidPath); rerr == nil && pid == cmd.Process.Pid {
			fmt.Fprintf(out, "Recording started (pid %d)\n", cmd.Process.Pid)
			return nil
		}
		// Non-blocking reap: WNOHANG returns immediately. If the
		// child has already exited, ProcessState is populated and
		// we can bail out of the poll loop with a precise error.
		var ws syscall.WaitStatus
		if wpid, werr := syscall.Wait4(cmd.Process.Pid, &ws, syscall.WNOHANG, nil); werr == nil && wpid == cmd.Process.Pid {
			stderrTail := readRecordLogTail(logPath)
			if stderrTail != "" {
				return fmt.Errorf("toggle: record process exited before registering:\n%s", stderrTail)
			}
			return fmt.Errorf("toggle: record process exited before registering (exit status %d)", ws.ExitStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The child never wrote the pidfile within the deadline. Either
	// it crashed (and Wait4 above missed the reap window) or it is
	// stuck on something slow before acquireRecordPID (e.g., config
	// load). Surface the captured stderr so the operator sees a loud
	// error with the real diagnostic instead of a silent start-then-
	// vanish.
	stderrTail := readRecordLogTail(logPath)
	if stderrTail != "" {
		return fmt.Errorf("toggle: record process did not register within 500ms:\n%s", stderrTail)
	}
	return fmt.Errorf("toggle: record process did not register within 500ms")
}

// readRecordLogTail returns the last recordLogTailBytes bytes of the
// record log, or an empty string if the file cannot be read. The
// handshake failure paths call this to quote the child's diagnostic
// back to the user. Bounded reads prevent an unlikely pathological
// log from blowing up the error message.
func readRecordLogTail(path string) string {
	const recordLogTailBytes = 8192
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) > recordLogTailBytes {
		data = data[len(data)-recordLogTailBytes:]
	}
	return strings.TrimRight(string(data), "\x00\n") + "\n"
}
