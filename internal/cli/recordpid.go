package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/hybridz/yap/internal/pidfile"
)

// writeRecordPID atomically creates the record PID file with the
// current process id. Returns an error if the file already exists —
// a second concurrent `yap record` should fail loudly rather than
// stomp an existing process' PID. The actual write goes through
// pidfile.Write so the daemon and record paths share one
// implementation.
func writeRecordPID() error {
	path, err := pidfile.RecordPath()
	if err != nil {
		return err
	}
	return pidfile.Write(path)
}

// readRecordPID returns the PID stored in the record PID file, or 0
// with a nil error when the file is absent (no record process
// running). A corrupted file returns a descriptive error — we never
// silently ignore a bad value because signaling PID 0 would target
// the whole process group.
func readRecordPID() (int, error) {
	path, err := pidfile.RecordPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read record pid: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid record pid file %s: %w", path, err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("invalid record pid file %s: non-positive pid %d", path, pid)
	}
	return pid, nil
}

// removeRecordPID deletes the record PID file. Idempotent — callers
// defer this to guarantee cleanup regardless of how the command
// terminates. Path-resolution failures are surfaced via slog.Warn so
// they show up in CI without breaking a successful exit.
func removeRecordPID() {
	path, err := pidfile.RecordPath()
	if err != nil {
		slog.Default().Warn("could not resolve record pid path", "err", err)
		return
	}
	os.Remove(path)
}
