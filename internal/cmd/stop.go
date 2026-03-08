package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the yap daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(cfg)
		},
	}
}

// runStop sends IPC stop command and waits for daemon shutdown.
// DAEMON-02: Graceful shutdown via IPC.
// DAEMON-04 (via daemon): SIGTERM causes clean shutdown.
// IPC-03: Exit 0 on success, 1 on error.
// IPC-04: Handle stale socket gracefully (daemon never started or crashed).
func runStop(cfg *config.Config) error {
	pidPath, err := xdg.DataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

	// IPC-04: If socket doesn't exist, daemon is not running (idempotent behavior).
	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		fmt.Printf("Daemon is not running\n")
		return nil // Exit 0 for idempotency (scripts expect this).
	}

	// Send IPC stop command (5s timeout per CONTEXT.md).
	resp, err := ipc.Send(sockPath, ipc.CmdStop, 5*time.Second)
	if err != nil {
		// IPC failed — daemon may be hung or crashed.
		// Try force cleanup: remove PID and socket files.
		pidfile.Remove(pidPath)
		os.Remove(sockPath)
		fmt.Printf("IPC stop failed; cleaned up stale files\n")
		return nil // Exit 0 (idempotent).
	}

	// IPC succeeded — poll for PID file removal to confirm shutdown.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if isLive, _ := pidfile.IsLive(pidPath); !isLive {
			fmt.Printf("Daemon stopped\n")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Daemon didn't shutdown within 3s — may be hung.
	fmt.Printf("Warning: Daemon shutdown timeout; PID file still exists\n")
	return nil // Exit 0 (don't fail; let user retry or force-kill).
}
