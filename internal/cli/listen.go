package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/adrg/xdg"
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
			return runListen(cfg, p, foreground)
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false,
		"run the daemon in the foreground (no fork) — for systemd, launchd, containers")
	return cmd
}

// newStartCmd is a hidden alias for `yap listen` kept for one release.
// It prints a deprecation notice to stderr on every invocation and
// then runs the same handler. A CHANGELOG entry tracks the removal.
func newStartCmd(cfg *config.Config, p platform.Platform) *cobra.Command {
	return &cobra.Command{
		Use:    "start",
		Short:  "deprecated alias for `yap listen`",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"yap: 'start' is deprecated; use 'yap listen' instead")
			return runListen(cfg, p, false)
		},
	}
}

// runListen is the shared handler for `yap listen` and the hidden
// `yap start` alias. It runs the first-run wizard if no config file
// exists, then either invokes daemon.Run in-process (foreground) or
// spawns a detached child that will re-exec with YAP_DAEMON=1.
func runListen(cfg *config.Config, p platform.Platform, foreground bool) error {
	if needsWizard() {
		if err := runWizard(p); err != nil {
			return fmt.Errorf("first-run setup failed: %w", err)
		}
		loaded, err := config.Load()
		if err != nil {
			return fmt.Errorf("reload config after wizard: %w", err)
		}
		*cfg = loaded
	}

	if foreground {
		return daemon.Run(cfg, daemon.DefaultDeps(p))
	}
	return spawnDaemonChild()
}

// spawnDaemonChild forks a detached child process running the yap
// binary with YAP_DAEMON=1 set. cmd/yap/main.go reads that env sentinel
// at startup and calls daemon.Run directly, bypassing cobra entirely
// so the user-visible CLI surface never shows a hidden flag.
//
// spawnDaemonChild blocks until either (a) the child's PID file and
// IPC socket both appear — meaning the daemon is ready — or (b) the
// 3s deadline expires, in which case it returns a descriptive error.
func spawnDaemonChild() error {
	pidPath, err := xdg.DataFile("yap/yap.pid")
	if err != nil {
		return fmt.Errorf("resolve pid path: %w", err)
	}

	// DAEMON-05: Check if daemon is already running.
	if isLive, _ := pidfile.IsLive(pidPath); isLive {
		return fmt.Errorf("yap is already running (PID file: %s)", pidPath)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
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
	childCmd.Stderr = nil

	if err := childCmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	childCmd.Process.Release()

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

	if !pidReady {
		return fmt.Errorf("daemon did not start within 3s (PID file not created)")
	}
	return fmt.Errorf("daemon started but IPC socket not ready within 3s")
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
