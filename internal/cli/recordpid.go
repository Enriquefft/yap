package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/adrg/xdg"
)

// recordPIDFilename is the canonical name for the `yap record`
// process PID file. It lives in the same XDG data directory as the
// daemon's yap.pid so `yap stop` and `yap toggle` can find both
// without a second path configuration knob.
const recordPIDFilename = "yap/yap-record.pid"

// recordPIDPath resolves the PID-file path for the running
// `yap record` process. Wrapping xdg.DataFile keeps the three call
// sites (writeRecordPID, readRecordPID, removeRecordPID) consistent;
// changing the location means changing a constant, not grepping for
// literal strings.
func recordPIDPath() (string, error) {
	path, err := xdg.DataFile(recordPIDFilename)
	if err != nil {
		return "", fmt.Errorf("resolve record pid path: %w", err)
	}
	return path, nil
}

// writeRecordPID atomically creates the record PID file with the
// current process id. Returns an error if the file already exists —
// a second concurrent `yap record` should fail loudly rather than
// stomp an existing process' PID.
func writeRecordPID() error {
	path, err := recordPIDPath()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("record pid file already exists at %s (another `yap record` running?)", path)
		}
		return fmt.Errorf("create record pid file: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("write record pid: %w", err)
	}
	return nil
}

// readRecordPID returns the PID stored in the record PID file, or 0
// with a nil error when the file is absent (no record process
// running). A corrupted file returns a descriptive error — we never
// silently ignore a bad value because signaling PID 0 would target
// the whole process group.
func readRecordPID() (int, error) {
	path, err := recordPIDPath()
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
// terminates.
func removeRecordPID() {
	path, err := recordPIDPath()
	if err != nil {
		return
	}
	os.Remove(path)
}
