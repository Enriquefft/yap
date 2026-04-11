// Package pidfile implements yap's runtime-state pidfile locking and
// path resolution. Every pidfile yap owns lives in
// $XDG_RUNTIME_DIR/yap (see paths.go) and is guarded by a flock-based
// advisory lock so a crashed daemon never leaves a stale pidfile that
// blocks the next start.
package pidfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Handle is an exclusive pidfile lock backed by flock(2).
//
// The kernel releases the flock automatically when the holding
// file descriptor is closed — which happens on process exit too,
// crash or not. That means a stale pidfile left by a crashed process
// is always reclaimable: the next Acquire on the same path sees the
// lock as unheld, takes it, and overwrites the old PID.
//
// Callers acquire a Handle via Acquire and MUST Close() it when done.
// Close releases the flock (by closing the fd) without unlinking the
// file. Close is idempotent.
//
// The file is intentionally left on disk after Close. Unlinking would
// open a lock-then-unlink race: between fd-close (flock released) and
// os.Remove, another process can Acquire the same inode, take flock,
// and write its PID; our subsequent os.Remove then unlinks the dentry
// and a third Acquire creates a fresh inode at the same path and also
// flocks it — two "owners" on two different inodes, both believing they
// hold the daemon lock. Leaving the file avoids the whole class; stale
// files are reclaimed transparently by the next Acquire's truncate-and-
// rewrite path. Since every yap pidfile lives under $XDG_RUNTIME_DIR
// (tmpfs, wiped on reboot or session exit) there is no long-term
// detritus to manage.
type Handle struct {
	f    *os.File
	path string
	once sync.Once
	err  error
}

// Acquire opens path (creating parent directories and the file itself
// if missing), takes an exclusive non-blocking advisory lock via
// flock(LOCK_EX|LOCK_NB), writes the current PID into the file, and
// returns the Handle.
//
// If another live process already holds the lock, Acquire returns an
// error naming the holder PID. A stale pidfile left behind by a
// crashed process is reclaimed transparently: the flock call succeeds
// (no holder), and the PID is overwritten.
//
// Errors from Acquire are always safe to propagate directly to the
// user — they never leak an open file descriptor. On any failure
// after the file was opened, the fd is closed first.
func Acquire(path string) (*Handle, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create pid dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open pid file %s: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			// Another live process holds the lock. Read the PID to
			// include in the error message so operators know who to
			// stop. A read failure here is non-fatal to the error
			// shape — we still report an "already running" error,
			// just without a PID.
			if otherPID, rerr := Read(path); rerr == nil && otherPID > 0 {
				return nil, fmt.Errorf(
					"yap daemon already running (pid %d); stop it first with yap stop",
					otherPID)
			}
			return nil, fmt.Errorf(
				"yap daemon already running; stop it first with yap stop")
		}
		return nil, fmt.Errorf("flock pid file %s: %w", path, err)
	}

	// We hold the exclusive lock. Truncate the file and write the
	// current PID. Any error here must release the flock by closing
	// the fd before returning, otherwise the caller can't retry.
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("truncate pid file %s: %w", path, err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek pid file %s: %w", path, err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write pid file %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("sync pid file %s: %w", path, err)
	}

	return &Handle{f: f, path: path}, nil
}

// Close releases the flock by closing the backing file descriptor.
// The pidfile is intentionally left on disk — see the Handle doc for
// why unlinking would be racy. Close is idempotent: calling it more
// than once returns nil on subsequent calls.
func (h *Handle) Close() error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		if h.f != nil {
			h.err = h.f.Close()
			h.f = nil
		}
	})
	return h.err
}

// Read parses the PID from a pidfile at path without taking any
// locks. This is the read-only path used by `yap stop`, `yap toggle`,
// and the record-process signal path where the caller only needs to
// know the PID, not hold the lock. Returns (0, fs.ErrNotExist) when
// the file is absent.
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

// IsLocked returns true if the pidfile at path is currently held by a
// live flock holder (another yap process). It is a non-destructive
// probe: IsLocked never creates the file, never writes to it, never
// changes the on-disk state. When the file does not exist IsLocked
// returns false; when it exists but no process holds the flock (stale
// file after a crash, or never-started daemon) IsLocked returns false.
//
// IsLocked exists so the listen parent can surface a clean
// "already running" error with the holder PID before it even spawns
// the daemon child — a nicer UX than letting the child fork and then
// observing a startup failure via its stderr tail three seconds later.
// The flock acquired by Acquire is still the single source of truth:
// IsLocked is a read-only hint that can be wrong in a race, but when
// it IS wrong the authoritative Acquire inside daemon.Run fails loudly
// with the same error wording, so nothing is at risk.
func IsLocked(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		// Missing file (the common "nothing running" case), no
		// permission, or some other open error — none of which
		// indicate a live holder. The worst case is a false negative
		// here, which the authoritative Acquire inside daemon.Run
		// catches; there is no correctness risk.
		return false
	}
	defer f.Close()
	// LOCK_NB so we do not block the caller. On success we release the
	// lock immediately — IsLocked must not mutate the holder state.
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return true
		}
		// Any other flock error (EBADF, EINVAL, ...) is a
		// misconfiguration we cannot diagnose here; treat it as "not
		// locked" and let Acquire produce the canonical error on the
		// real start path.
		return false
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false
}
