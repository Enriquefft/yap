package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/adrg/xdg"
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
func runStop(out io.Writer) error {
	daemonStopped, dErr := stopDaemon(out)
	recordStopped, rErr := stopRecord(out)

	if !daemonStopped && !recordStopped {
		// Nothing to stop. Report clearly but exit 0 so scripts
		// don't break on repeated invocation.
		fmt.Fprintln(out, "No yap daemon or record process running")
	}
	// Surface the first real error, not the "not running" ones.
	if dErr != nil {
		return dErr
	}
	if rErr != nil {
		return rErr
	}
	return nil
}

// stopDaemon runs the daemon-shutdown path. Returns (stopped, err)
// where stopped is true if the daemon was present and the stop
// request was acknowledged (IPC or fallback cleanup).
func stopDaemon(out io.Writer) (bool, error) {
	pidPath, err := xdg.DataFile("yap/yap.pid")
	if err != nil {
		return false, fmt.Errorf("resolve pid path: %w", err)
	}
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return false, fmt.Errorf("resolve socket path: %w", err)
	}

	if _, err := os.Stat(sockPath); errors.Is(err, os.ErrNotExist) {
		// Daemon not running — not an error, just nothing to stop.
		return false, nil
	}

	resp, err := ipc.Send(sockPath, ipc.CmdStop, 5*time.Second)
	if err != nil {
		// IPC failed — daemon may be hung. Force cleanup.
		pidfile.Remove(pidPath)
		os.Remove(sockPath)
		fmt.Fprintln(out, "IPC stop failed; cleaned up stale files")
		return true, nil
	}
	if !resp.Ok {
		return true, fmt.Errorf("daemon rejected stop command: %s", resp.Error)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if isLive, _ := pidfile.IsLive(pidPath); !isLive {
			fmt.Fprintln(out, "Daemon stopped")
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Fprintln(out, "Warning: Daemon shutdown timeout; PID file still exists")
	return true, nil
}

// stopRecord SIGTERMs the standalone `yap record` process, if any.
// Returns (stopped, err) where stopped is true when the record PID
// file existed and the signal was delivered successfully.
func stopRecord(out io.Writer) (bool, error) {
	pid, err := readRecordPID()
	if err != nil {
		return false, fmt.Errorf("record pid: %w", err)
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
		return false, fmt.Errorf("signal record pid %d: %w", pid, err)
	}
	// The signal was delivered to a real, signalable process. The
	// PID file is no longer authoritative — the child will exit and
	// the file becomes stale. Remove it now so a follow-up `yap stop`
	// reports correctly.
	removeRecordPID()
	fmt.Fprintln(out, "Record process signalled (SIGTERM)")
	return true, nil
}
