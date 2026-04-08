package pidfile

import (
	"fmt"

	"github.com/adrg/xdg"
)

// XDG-relative path constants for yap's runtime files. Centralizing
// the literal strings here means changing the on-disk layout is a
// single edit, not a grep across the CLI and daemon packages.
const (
	daemonPIDFile = "yap/yap.pid"
	recordPIDFile = "yap/yap-record.pid"
	socketFile    = "yap/yap.sock"
)

// DaemonPath resolves the absolute path of the yap daemon's PID file.
// Wraps xdg.DataFile so callers do not have to repeat the literal
// "yap/yap.pid" string. Returns a wrapped error on resolution failure
// so the caller can chain it into a higher-level command error.
func DaemonPath() (string, error) {
	path, err := xdg.DataFile(daemonPIDFile)
	if err != nil {
		return "", fmt.Errorf("resolve daemon pid path: %w", err)
	}
	return path, nil
}

// RecordPath resolves the absolute path of the standalone `yap record`
// process PID file. Lives in the same XDG data directory as the
// daemon PID so `yap stop` and `yap toggle` can locate both without
// a second configuration knob.
func RecordPath() (string, error) {
	path, err := xdg.DataFile(recordPIDFile)
	if err != nil {
		return "", fmt.Errorf("resolve record pid path: %w", err)
	}
	return path, nil
}

// SocketPath resolves the absolute path of the daemon's IPC unix
// socket. Co-located with the PID files in $XDG_DATA_HOME/yap so the
// whole runtime tree is one directory operators can clean up.
func SocketPath() (string, error) {
	path, err := xdg.DataFile(socketFile)
	if err != nil {
		return "", fmt.Errorf("resolve socket path: %w", err)
	}
	return path, nil
}
