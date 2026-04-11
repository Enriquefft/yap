//go:build windows

package models

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// acquireCacheLock takes an exclusive advisory lock on
// <dir>/.lock via LockFileEx and returns a release function.
//
// Windows' file locking is mandatory rather than advisory, which
// is exactly what we want: two concurrent yap processes cannot
// race-download the same model file because the second one
// blocks on LockFileEx until the first closes the handle.
//
// The lock file is created with 0o600 to match the Unix lock's
// single-user cache semantics. Windows treats the mode bits as
// advisory but the intent stays consistent across platforms.
//
// The release function closes the handle, which releases the
// lock atomically.
func acquireCacheLock(dir string) (func(), error) {
	lockPath := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("models: open lock file %s: %w", lockPath, err)
	}
	// LOCKFILE_EXCLUSIVE_LOCK is the exclusive-lock flag; the
	// absence of LOCKFILE_FAIL_IMMEDIATELY turns the call into
	// a blocking acquire. The range (0,maxUint32,maxUint32)
	// locks the whole file, matching the Unix flock semantics.
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("models: acquire lock %s: %w", lockPath, err)
	}
	return func() { _ = f.Close() }, nil
}
