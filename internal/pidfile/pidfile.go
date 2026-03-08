package pidfile

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// Write atomically creates a PID file at path with O_EXCL.
// Fails if the file already exists (DAEMON-05 double-start prevention).
// File is readable only by owner (mode 0600).
func Write(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("pid file already exists: %w", err)
		}
		return fmt.Errorf("create pid file: %w", err)
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%d\n", os.Getpid())
	if err != nil {
		return fmt.Errorf("write pid: %w", err)
	}
	return nil
}

// Read parses the PID from a PID file.
func Read(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	return pid, nil
}

// IsLive checks if the process in the PID file is running.
// Uses Signal(0) — the standard Unix liveness test (DAEMON-05).
// os.FindProcess on Unix always succeeds; Signal(0) is the correct check.
// Auto-removes stale PID files (process doesn't exist).
func IsLive(path string) (bool, error) {
	pid, err := Read(path)
	if os.IsNotExist(err) {
		return false, nil // No PID file — not running.
	}
	if err != nil {
		return false, err
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil // Can't find process.
	}

	// Signal(0) tests if we can signal the process (== it exists).
	// No actual signal is sent.
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil // Process exists.
	}

	// Process is dead — clean up stale file.
	if os.IsNotExist(err) || err == os.ErrProcessDone {
		os.Remove(path)
		return false, nil
	}

	// ESRCH: No such process — stale file.
	if err, ok := err.(*os.SyscallError); ok && err.Err == syscall.ESRCH {
		os.Remove(path)
		return false, nil
	}

	// EPERM: Process exists but owned by another user — still live.
	if err, ok := err.(*os.SyscallError); ok && err.Err == syscall.EPERM {
		return true, nil
	}

	return false, nil
}

// Remove deletes the PID file. Idempotent — no error if file doesn't exist.
func Remove(path string) {
	os.Remove(path)
}
