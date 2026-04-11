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
// Lives in $XDG_RUNTIME_DIR/yap (tmpfs, wiped on reboot/logout) so no
// stale state can survive a crash + reboot cycle. Returns a wrapped
// error on resolution failure so the caller can chain it into a
// higher-level command error.
func DaemonPath() (string, error) { return xdgRuntime(daemonPIDFile) }

// RecordPath resolves the absolute path of the standalone `yap record`
// process PID file. Co-located with the daemon PID file in
// $XDG_RUNTIME_DIR/yap so `yap stop` and `yap toggle` can locate both
// without a second configuration knob.
func RecordPath() (string, error) { return xdgRuntime(recordPIDFile) }

// SocketPath resolves the absolute path of the daemon's IPC unix
// socket. Lives in $XDG_RUNTIME_DIR/yap, which the OS wipes on reboot
// or logout — no stale state across reboots.
func SocketPath() (string, error) { return xdgRuntime(socketFile) }

// xdgRuntime resolves relPath against $XDG_RUNTIME_DIR (falling back
// to the OS temp dir inside adrg/xdg when the variable is unset) and
// creates the parent directory chain. Any resolution failure is
// wrapped with the literal name so callers can tell which runtime
// file failed to resolve without re-parsing the error chain.
func xdgRuntime(name string) (string, error) {
	path, err := xdg.RuntimeFile(name)
	if err != nil {
		return "", fmt.Errorf("resolve runtime path %s: %w", name, err)
	}
	return path, nil
}
