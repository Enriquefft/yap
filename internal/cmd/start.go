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
// FIRSTRUN-01: On first run (no config file), launch interactive wizard.
func runStart(cfg *config.Config) error {
	// Check if wizard is needed (no config file and no GROQ_API_KEY env var)
	if needsWizard() {
		// Run first-run wizard
		if err := runWizard(); err != nil {
			return fmt.Errorf("first-run setup failed: %w", err)
		}

		// Reload config after wizard completion to pick up new file
		loadedCfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("reload config after wizard: %w", err)
		}
		// Update the pointer to point to the loaded config
		*cfg = loadedCfg
	}

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

	// Wait for PID file AND IPC socket to appear (confirms daemon is ready).
	sockPath, err := xdg.DataFile("yap/yap.sock")
	if err != nil {
		return fmt.Errorf("resolve socket path: %w", err)
	}

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
			fmt.Printf("Daemon started successfully\n")
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Daemon didn't fully initialize in time.
	if !pidReady {
		return fmt.Errorf("daemon did not start within 3s (PID file not created)")
	}
	return fmt.Errorf("daemon started but IPC socket not ready within 3s")
}

// needsWizard checks if the first-run wizard should be launched.
// Returns true if: config file missing AND GROQ_API_KEY env var not set.
func needsWizard() bool {
	// Check if GROQ_API_KEY env var is set
	if os.Getenv("GROQ_API_KEY") != "" {
		return false
	}

	// Check if config file exists
	configPath, err := config.ConfigPath()
	if err != nil {
		// Can't determine config path, assume wizard needed
		return true
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		// Config file doesn't exist
		return true
	}

	// Config file exists
	return false
}

// runWizard launches the interactive first-run wizard.
func runWizard() error {
	_, err := config.RunWizard(os.Stdin, os.Stdout)
	if err != nil {
		return fmt.Errorf("wizard failed: %w", err)
	}

	// Verify config file was created
	configPath, err := config.ConfigPath()
	if err != nil {
		return fmt.Errorf("get config path: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not created: %s", configPath)
	}

	return nil
}
