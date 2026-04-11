package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/spf13/cobra"
)

// Import-level note: toggle reuses lockedBuffer from listen.go. Both
// files live in package cli so the symbol is accessible here without
// re-declaration — single source of truth for the thread-safe
// bytes.Buffer wrapper assigned to exec.Cmd.Stderr.

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
// Child stderr is teed into a thread-safe buffer for the duration
// of the handshake so the two failure paths — "child exited before
// writing pidfile" and "child is stuck / took too long" — can quote
// the child's own diagnostic in the returned error. Without this
// tee any crash message from the child is silently dropped and the
// operator is left debugging a "did not register within 500ms"
// generic timeout.
func startRecordProcess(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("toggle: resolve executable: %w", err)
	}
	cmd := exec.Command(exe, "record")
	cmd.Stdin = nil
	cmd.Stdout = nil
	stderrBuf := &lockedBuffer{}
	cmd.Stderr = stderrBuf
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
			stderrTail := stderrBuf.String()
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
	stderrTail := stderrBuf.String()
	if stderrTail != "" {
		return fmt.Errorf("toggle: record process did not register within 500ms:\n%s", stderrTail)
	}
	return fmt.Errorf("toggle: record process did not register within 500ms")
}
