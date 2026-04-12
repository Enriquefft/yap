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
	daemonLogFile = "yap/yap.log"
	recordPIDFile = "yap/yap-record.pid"
	recordLogFile = "yap/yap-record.log"
	socketFile    = "yap/yap.sock"
)

// DaemonPath resolves the absolute path of the yap daemon's PID file.
// Lives in $XDG_RUNTIME_DIR/yap (tmpfs, wiped on reboot/logout) so no
// stale state can survive a crash + reboot cycle. Returns a wrapped
// error on resolution failure so the caller can chain it into a
// higher-level command error.
func DaemonPath() (string, error) { return xdgRuntime(daemonPIDFile) }

// DaemonLogPath resolves the absolute path of the detached yap
// daemon's stderr log. `yap listen` (the non-foreground background
// fork path) opens this file as the child's stderr so the daemon
// keeps a real fd on its log destination after the spawning parent
// exits. If listen piped the child's stderr into an in-memory
// buffer instead, the parent goroutine holding the pipe's read end
// would die when the listen parent returned from the readiness
// wait, the read end would close, and the daemon's next stderr
// write would trigger SIGPIPE — killing the daemon before it
// processed a single hotkey. Same failure mode as the record child
// spawned by `yap toggle`; using a file here is the single source
// of truth for "where do detached daemon logs go" and sidesteps the
// failure entirely.
func DaemonLogPath() (string, error) { return xdgRuntime(daemonLogFile) }

// RecordPath resolves the absolute path of the standalone `yap record`
// process PID file. Co-located with the daemon PID file in
// $XDG_RUNTIME_DIR/yap so `yap stop` and `yap toggle` can locate both
// without a second configuration knob.
func RecordPath() (string, error) { return xdgRuntime(recordPIDFile) }

// RecordLogPath resolves the absolute path of the standalone `yap
// record` process stderr log. `yap toggle` opens this file as the
// child's stderr so the child keeps a real fd on its log destination
// after the toggle parent exits. If toggle piped the child's stderr
// into an in-memory buffer instead, the underlying Go runtime would
// create a pipe whose read end lives in a parent goroutine; when the
// parent exits that goroutine dies, the read end closes, and the next
// child stderr write triggers SIGPIPE — killing the record process
// mid-pipeline before it can transcribe and inject. Using a file here
// is the single source of truth for "where do detached record logs
// go" and sidesteps that failure mode entirely.
func RecordLogPath() (string, error) { return xdgRuntime(recordLogFile) }

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
