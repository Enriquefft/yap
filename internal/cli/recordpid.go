package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/Enriquefft/yap/internal/pidfile"
)

// acquireRecordPID takes the exclusive flock on the `yap record` PID
// file and returns the live Handle. The caller defers handle.Close()
// so the file is removed and the lock released on process exit —
// including crash exits, because the kernel releases the flock when
// the holding fd closes.
//
// A second concurrent `yap record` fails here with a descriptive
// error identifying the live holder, instead of racing on an O_EXCL
// create and stomping another process' PID.
func acquireRecordPID() (*pidfile.Handle, error) {
	path, err := pidfile.RecordPath()
	if err != nil {
		return nil, err
	}
	return pidfile.Acquire(path)
}

// readRecordPID returns the PID stored in the record PID file, or 0
// with a nil error when the file is absent (no record process
// running). A corrupted file returns a descriptive error — we never
// silently ignore a bad value because signaling PID 0 would target
// the whole process group.
//
// This is a read-only probe: it never takes the flock. Callers that
// need to take ownership of the pidfile must go through
// acquireRecordPID instead.
func readRecordPID() (int, error) {
	path, err := pidfile.RecordPath()
	if err != nil {
		return 0, err
	}
	pid, err := pidfile.Read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read record pid: %w", err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("invalid record pid file %s: non-positive pid %d", path, pid)
	}
	return pid, nil
}

// removeRecordPID deletes the record PID file. Used by `yap toggle`
// and `yap stop` when a stale file is detected (the live process
// holder has gone away). Idempotent — no error if the file is
// already missing.
//
// Path-resolution failures are silently ignored because the callers
// already handle the absent-file case downstream and a missing
// pidfile is not a user-visible failure.
func removeRecordPID() {
	path, err := pidfile.RecordPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
