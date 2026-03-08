package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/pidfile"
	"github.com/adrg/xdg"
	"github.com/spf13/cobra"
)

func newStartCmd(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the yap daemon in the background",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cfg)
		},
	}
}

// runStart spawns the daemon in the background.
// DAEMON-01: Daemon writes PID file to $XDG_DATA_HOME/yap/yap.pid
// DAEMON-05: Second start detects live daemon and exits with error.
func runStart(cfg *config.Config) error {
	pidPath, err := xdg.DataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	// DAEMON-05: Check if daemon is already running.
	if isLive, _ := pidfile.IsLive(pidPath); isLive {
		return fmt.Errorf("yap is already running (PID file: %s)", pidPath)
	}

	// Get the path to the current executable.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Spawn detached daemon child.
	// The child will exec "yap --daemon-run" which triggers daemon.Run() in PersistentPreRunE.
	cmd := exec.Command(self, "--daemon-run")

	// Detach from parent: new session, no terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// Redirect stdio to /dev/null (daemon must not inherit terminal).
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Start child process.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Release our reference to the child — it runs independently.
	cmd.Process.Release()

	// Wait for PID file to appear (confirms daemon started successfully).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidPath); err == nil {
			fmt.Printf("Daemon started successfully\n")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	// PID file didn't appear — daemon startup may have failed.
	return fmt.Errorf("daemon did not start within 2s (PID file not created)")
}
