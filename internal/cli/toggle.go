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
	"github.com/spf13/cobra"
)

func newToggleCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "toggle",
		Short: "toggle yap recording (daemon IPC or `yap record` signal)",
		Long: `toggle starts/stops a recording cycle.

When the yap daemon is running, toggle uses IPC to flip its
recording state. When a standalone 'yap record' process is running
instead, toggle sends it SIGUSR1, which ends the recording cleanly
so the captured audio still flows through transcribe and inject.

If neither the daemon nor a record process is running, toggle exits
with status 1 so scripts can detect the no-op state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runToggle(cmd.OutOrStdout())
		},
	}
}

// runToggle prefers the daemon IPC path. If no daemon socket exists,
// it falls back to signaling the standalone `yap record` process via
// SIGUSR1 (which the record command's signal handler routes into a
// recCtx cancellation, ending the recording cleanly so transcribe and
// inject still run on the captured audio).
//
// Status messages are written to out so tests can capture them via
// the cobra command's writer; runToggle never touches os.Stdout.
func runToggle(out io.Writer) error {
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}
	if _, err := os.Stat(sockPath); err == nil {
		// Daemon socket exists — use IPC.
		resp, err := ipc.Send(sockPath, ipc.CmdToggle, 5*time.Second)
		if err != nil {
			return fmt.Errorf("toggle: %w", err)
		}
		if !resp.Ok {
			return fmt.Errorf("toggle: %s", resp.Error)
		}
		fmt.Fprintf(out, "Toggle successful (state: %s)\n", resp.State)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat socket: %w", err)
	}

	// No daemon — try the standalone record process.
	pid, err := readRecordPID()
	if err != nil {
		return fmt.Errorf("toggle: %w", err)
	}
	if pid == 0 {
		return fmt.Errorf("toggle: no daemon and no `yap record` process running")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("toggle: locate record pid %d: %w", pid, err)
	}
	if err := proc.Signal(syscall.SIGUSR1); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			removeRecordPID()
			return fmt.Errorf("toggle: record process %d already gone", pid)
		}
		return fmt.Errorf("toggle: signal record pid %d: %w", pid, err)
	}
	fmt.Fprintf(out, "Toggle signalled record process %d (SIGUSR1)\n", pid)
	return nil
}
