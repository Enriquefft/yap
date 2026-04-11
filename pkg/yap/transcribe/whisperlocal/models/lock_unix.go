//go:build !windows

package models

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireCacheLock takes an exclusive advisory lock on
// <dir>/.lock and returns a release function. The lock file is
// created if missing and persists across runs (it is empty and
// effectively zero-cost on disk).
//
// The lock file is created with 0o600 so it matches the permissions
// of the model files alongside it — the cache is single-user state
// and no other user has a legitimate reason to read the sentinel.
//
// The release function closes the file descriptor, which releases
// the flock atomically with the close on Linux and the other Unix
// targets yap builds for.
func acquireCacheLock(dir string) (func(), error) {
	lockPath := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("models: open lock file %s: %w", lockPath, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("models: acquire lock %s: %w", lockPath, err)
	}
	return func() { _ = f.Close() }, nil
}
