package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "stop the yap daemon or an active `yap record`",
		Long: `stop signals the running yap daemon to shut down cleanly, and
also signals any running 'yap record' process (identified by its
yap-record.pid file) to terminate. Either one missing is fine —
stop exits 0 if anything was stopped.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(cmd.OutOrStdout())
		},
	}
}

// runStop is idempotent and best-effort. It stops the daemon (when
// present) and the standalone `yap record` process (when present) and
// exits 0 if at least one was running. When nothing was running it
// still exits 0 so shell pipelines can invoke `yap stop` safely.
//
// Status messages are written to out so tests can capture them via
// the cobra command's writer; runStop never touches os.Stdout.
//
// Errors from the two paths are combined via errors.Join so callers
// see every real failure (not just the first one) without losing the
// original error chain — this matches the "fail loudly, fail
// completely" rule from the project quality bar.
func runStop(out io.Writer) error {
	daemonStopped, dErr := stopDaemon(out)
	recordStopped, rErr := stopRecord(out)

	if !daemonStopped && !recordStopped {
		// Nothing to stop. Report clearly but exit 0 so scripts
		// don't break on repeated invocation.
		fmt.Fprintln(out, "No yap daemon or record process running")
	}
	return errors.Join(dErr, rErr)
}

// stopDaemon runs the daemon-shutdown path. Returns (stopped, err)
// where stopped is true if a live daemon was present at the start of
// the call and some stop action was taken (IPC or signal).
//
// Liveness is probed via pidfile.IsLocked — the authoritative signal.
// A stale pidfile with no flock holder reads as "not running" so the
// next listen can reclaim it cleanly. File existence (of pidfile or
// socket) is never used as a liveness signal because both can outlive
// a crashed daemon and would produce false positives.
//
// On IPC failure the fallback is SIGTERM+SIGKILL to the PID read from
// the pidfile — NOT os.Remove. Unlinking the pidfile while the daemon
// is still alive opens the exact lock-then-unlink race the pidfile
// Handle contract was designed to eliminate: two concurrent Acquires
// on the path would bind to two different inodes and both "own" the
// lock. Killing the process instead lets the kernel release the flock
// on exit, and the next Acquire truncates and rewrites the existing
// file cleanly.
func stopDaemon(out io.Writer) (bool, error) {
	pidPath, err := pidfile.DaemonPath()
	if err != nil {
		return false, fmt.Errorf("stop: daemon: %w", err)
	}
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		return false, fmt.Errorf("stop: daemon: %w", err)
	}

	if !pidfile.IsLocked(pidPath) {
		// No live daemon holding the flock. Stale pidfile (if any)
		// is not our problem — the next Acquire reclaims it.
		return false, nil
	}

	resp, ipcErr := ipc.Send(sockPath, ipc.CmdStop, 5*time.Second)
	if ipcErr == nil {
		if !resp.Ok {
			return true, fmt.Errorf("stop: daemon rejected stop command: %s", resp.Error)
		}
		if waitForFlockRelease(pidPath, 3*time.Second) {
			fmt.Fprintln(out, "Daemon stopped")
			return true, nil
		}
		fmt.Fprintln(out, "Warning: Daemon shutdown timeout; process still holds pidfile lock")
		return true, nil
	}

	// IPC failed but the daemon holds the flock. Fall back to
	// SIGTERM against the PID recorded in the pidfile. The kernel
	// releases the flock when the process exits; the next listen
	// reclaims the pidfile via Acquire's truncate-and-rewrite path.
	pid, rerr := pidfile.Read(pidPath)
	if rerr != nil || pid <= 0 {
		return false, fmt.Errorf(
			"stop: daemon IPC failed (%v) and pidfile unreadable (%v); investigate manually",
			ipcErr, rerr)
	}
	proc, ferr := os.FindProcess(pid)
	if ferr != nil {
		return false, fmt.Errorf("stop: daemon IPC failed (%v); FindProcess(%d): %w", ipcErr, pid, ferr)
	}
	fmt.Fprintf(out, "IPC stop failed (%v); sending SIGTERM to pid %d\n", ipcErr, pid)
	if serr := proc.Signal(syscall.SIGTERM); serr != nil {
		if errors.Is(serr, os.ErrProcessDone) || errors.Is(serr, syscall.ESRCH) {
			// Daemon died between the flock probe and the signal.
			return true, nil
		}
		return false, fmt.Errorf("stop: SIGTERM pid %d: %w", pid, serr)
	}
	if waitForFlockRelease(pidPath, 3*time.Second) {
		fmt.Fprintln(out, "Daemon stopped")
		return true, nil
	}
	// SIGTERM did not stick. Escalate to SIGKILL. The kernel
	// releases the flock on exit regardless of the signal.
	fmt.Fprintln(out, "Daemon unresponsive to SIGTERM; escalating to SIGKILL")
	if kerr := proc.Signal(syscall.SIGKILL); kerr != nil {
		if errors.Is(kerr, os.ErrProcessDone) || errors.Is(kerr, syscall.ESRCH) {
			return true, nil
		}
		return false, fmt.Errorf("stop: SIGKILL pid %d: %w", pid, kerr)
	}
	if waitForFlockRelease(pidPath, 2*time.Second) {
		fmt.Fprintln(out, "Daemon killed")
		return true, nil
	}
	return true, fmt.Errorf("stop: pid %d unresponsive to SIGKILL; kernel may have wedged the process", pid)
}

// waitForFlockRelease polls pidfile.IsLocked until it returns false or
// the deadline elapses. Returns true on release, false on timeout.
// This is the authoritative signal that a daemon process has exited,
// replacing the old PID-signal-probe loop that couldn't distinguish a
// dying process from a freshly-started one with the same PID.
func waitForFlockRelease(pidPath string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !pidfile.IsLocked(pidPath) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// stopRecord SIGTERMs the standalone `yap record` process, if any.
// Returns (stopped, err) where stopped is true when the record PID
// file existed and the signal was delivered successfully.
func stopRecord(out io.Writer) (bool, error) {
	pid, err := readRecordPID()
	if err != nil {
		return false, fmt.Errorf("stop: record pid: %w", err)
	}
	if pid == 0 {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		// Stale PID file — remove it and keep going.
		removeRecordPID()
		return false, nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			removeRecordPID()
			return false, nil
		}
		return false, fmt.Errorf("stop: signal record pid %d: %w", pid, err)
	}
	// The signal was delivered. The record child's defer in runRecord
	// is the canonical owner of the PID file lifecycle and will remove
	// it as the process exits — mirroring the daemon path, where
	// stopDaemon waits for the daemon to clean up its own PID file.
	fmt.Fprintln(out, "Record process signalled (SIGTERM)")
	return true, nil
}
