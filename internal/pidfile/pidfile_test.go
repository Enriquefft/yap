package pidfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWriteCreatesFile verifies Write creates a file with PID (DAEMON-01).
func TestWriteCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	err := Write(pidPath)
	require.NoError(t, err)

	pid, err := Read(pidPath)
	require.NoError(t, err)
	require.Equal(t, os.Getpid(), pid)
}

// TestWriteFailsIfExists verifies O_EXCL prevents double-write (DAEMON-05).
func TestWriteFailsIfExists(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	err := Write(pidPath)
	require.NoError(t, err)

	// Second write should fail.
	err = Write(pidPath)
	require.Error(t, err)
	require.True(t, errors.Is(err, fs.ErrExist))
}

// TestIsLiveForRunningProcess returns true for current process.
func TestIsLiveForRunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	err := Write(pidPath)
	require.NoError(t, err)

	live, err := IsLive(pidPath)
	require.NoError(t, err)
	require.True(t, live)
}

// TestIsLiveRemovesStaleFile auto-cleans stale PID files.
func TestIsLiveRemovesStaleFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write a PID file with an obviously dead PID (e.g., PID 1 is init, unlikely to be what we can signal).
	// For testing, write PID 99999 (almost certainly doesn't exist).
	err := os.WriteFile(pidPath, []byte("99999\n"), 0600)
	require.NoError(t, err)

	// Verify file exists before check.
	_, err = os.Stat(pidPath)
	require.NoError(t, err)

	live, err := IsLive(pidPath)
	require.NoError(t, err)
	require.False(t, live)

	// File should be removed by IsLive.
	_, err = os.Stat(pidPath)
	require.True(t, os.IsNotExist(err))
}

// TestRemoveIsIdempotent verifies Remove doesn't error if file missing.
func TestRemoveIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "missing.pid")

	// Should not error even though file doesn't exist.
	Remove(pidPath)
}
