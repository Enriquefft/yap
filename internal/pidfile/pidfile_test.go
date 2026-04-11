package pidfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcquireCreatesFile verifies Acquire creates the pidfile with the
// current PID and the parent directory when both are missing. This is
// the cold-start happy path the daemon exercises every start.
func TestAcquireCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "nested", "yap.pid")

	h, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer h.Close()

	pid, err := Read(pidPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Errorf("pidfile stat: %v", err)
	}
}

// TestAcquireFailsWhenLockHeld verifies Acquire fails when another
// in-process handle already holds the flock, and the error names the
// holder PID and suggests `yap stop`. This is the double-start guard
// operators rely on.
func TestAcquireFailsWhenLockHeld(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "yap.pid")

	first, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer first.Close()

	// The test process holds the flock via `first`. A second
	// Acquire against the same path must fail with a descriptive
	// error. In practice the Flock(LOCK_NB) call on the same fd
	// would succeed (same-process re-entry), but we're opening a
	// fresh fd, so the second Acquire exercises the real
	// cross-process contention path.
	_, err = Acquire(pidPath)
	if err == nil {
		t.Fatal("second Acquire should fail while lock is held")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error should mention already-running: %v", err)
	}
}

// TestAcquireReclaimsStaleFile simulates a crashed process that left a
// pidfile on disk with a dead PID. A fresh Acquire must succeed and
// overwrite the PID — this is the whole point of flock-based locking
// (the OS released the flock when the dead process exited, so there
// is no stale lock to clean up).
func TestAcquireReclaimsStaleFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "yap.pid")

	// Seed a stale pidfile with a bogus PID. No flock is held.
	if err := os.WriteFile(pidPath, []byte("99999\n"), 0o600); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}

	h, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("Acquire should reclaim stale file: %v", err)
	}
	defer h.Close()

	pid, err := Read(pidPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d (Acquire should have overwritten the stale PID)",
			pid, os.Getpid())
	}
}

// TestCloseLeavesFileOnDisk verifies Close releases the flock
// without unlinking the file. Unlinking would open the classic
// lock-then-unlink race documented on Handle: between fd-close
// (flock released) and os.Remove, a second Acquire could bind to
// the same inode, take flock, and write its PID; the original
// Close's os.Remove would then unlink that dentry, and a third
// Acquire would create a fresh inode at the same path and flock
// THAT — yielding two "owners" on two different inodes, both
// believing they hold the daemon lock.
//
// The correct contract is: leave the file behind, let the next
// Acquire's truncate-and-rewrite path reclaim it. tmpfs under
// $XDG_RUNTIME_DIR wipes stale files on reboot or session exit,
// so there is no long-term detritus.
func TestCloseLeavesFileOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "yap.pid")

	h, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Errorf("pidfile should still exist after Close (flock-only release), got err=%v", err)
	}
}

// TestCloseIsIdempotent verifies Close can be called multiple times
// without error. Callers defer Close(), and some paths call it a
// second time on early return, so double-close must be safe.
func TestCloseIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "yap.pid")

	h, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
}

// TestCloseNilReceiver verifies a nil Handle receiver is a no-op.
// Callers that defer h.Close() before checking the Acquire error
// shouldn't panic on the nil path.
func TestCloseNilReceiver(t *testing.T) {
	var h *Handle
	if err := h.Close(); err != nil {
		t.Errorf("nil Close should return nil, got %v", err)
	}
}

// TestReadMissingFile verifies Read surfaces fs.ErrNotExist on a
// missing pidfile so callers can use errors.Is(err, fs.ErrNotExist)
// to distinguish "daemon not running" from a real IO failure.
func TestReadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "missing.pid")

	_, err := Read(pidPath)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

// TestAcquireReleasesLockAfterClose verifies the sequence that a real
// crash-and-restart cycle goes through: Acquire, Close, Acquire again.
// The second Acquire must succeed because the first Close released
// the flock AND removed the file.
func TestAcquireReleasesLockAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "yap.pid")

	h1, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	if err := h1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	h2, err := Acquire(pidPath)
	if err != nil {
		t.Fatalf("second Acquire after Close: %v", err)
	}
	defer h2.Close()

	// Confirm the fresh acquire wrote a current PID to a fresh
	// file — not a lingering byte from h1's state.
	pid, err := Read(pidPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}
