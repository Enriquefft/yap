package inject

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
)

// WaylandConn is the narrow interface the wlroots detector uses to
// talk to a wayland compositor. It is satisfied by *net.UnixConn in
// production and by an in-memory fake in tests, so the detector can
// be exercised end-to-end without a real socket.
//
// Read and Write must observe the deadline set by SetDeadline; the
// detector relies on the deadline to enforce the per-call latency
// budget without leaking goroutines.
type WaylandConn interface {
	io.Reader
	io.Writer
	io.Closer
	SetDeadline(t time.Time) error
}

// Deps is the dependency bag for the inject package. Every external
// effect (process exec, clipboard I/O, filesystem reads, environment
// lookup, time) routes through this struct so tests can substitute
// fakes without touching package-level state.
//
// The struct is intentionally large: it is the only place dependency
// injection happens for this package, which means there are no hidden
// hooks elsewhere. Production code constructs the bag via NewDeps();
// tests build a bag literal field-by-field.
//
// Every exec hook is ctx-aware so cancellation propagates into
// in-flight subprocesses, and every blocking wait routes through
// SleepCtx so tests can substitute an instant return and production
// cancels promptly.
type Deps struct {
	// ExecCommandContext mirrors exec.CommandContext. Strategies build
	// the *exec.Cmd and call .Run(), .Output(), or wire .Stdin
	// themselves so the dependency surface stays narrow. Cancellation
	// of ctx kills the spawned process.
	ExecCommandContext func(ctx context.Context, name string, args ...string) *exec.Cmd

	// ClipboardRead returns the current clipboard contents.
	ClipboardRead func() (string, error)

	// ClipboardWrite replaces the clipboard contents.
	ClipboardWrite func(text string) error

	// LookPath mirrors exec.LookPath.
	LookPath func(file string) (string, error)

	// OSStat mirrors os.Stat. Used to probe the ydotool socket.
	OSStat func(name string) (os.FileInfo, error)

	// OSOpenFile opens a file for writing. Used by the OSC52 strategy
	// to write the escape sequence into /dev/pts/<N>. The returned
	// io.WriteCloser must be closed by the caller.
	OSOpenFile func(name string, flag int, perm os.FileMode) (io.WriteCloser, error)

	// OSReadFile mirrors os.ReadFile. Used by /proc traversal in OSC52
	// resolution.
	OSReadFile func(name string) ([]byte, error)

	// OSReadlink mirrors os.Readlink. Used to resolve /proc/<pid>/fd/0
	// to a /dev/pts/<N> path.
	OSReadlink func(name string) (string, error)

	// OSReadDir mirrors os.ReadDir. Used to walk /proc/<pid>/task/<tid>/children
	// for OSC52 child-process discovery.
	OSReadDir func(name string) ([]os.DirEntry, error)

	// EnvGet mirrors os.Getenv. Used for SWAYSOCK / HYPRLAND_INSTANCE_SIGNATURE
	// / WAYLAND_DISPLAY / DISPLAY / TMUX / SSH_TTY / SSH_CONNECTION /
	// YDOTOOL_SOCKET / XDG_RUNTIME_DIR detection.
	EnvGet func(key string) string

	// WaylandDial opens a connection to the wayland compositor at the
	// resolved socket path. Production wires net.DialUnix; tests
	// inject an in-memory WaylandConn carrying canned protocol
	// frames. The dialer is given the absolute socket path (already
	// resolved from $XDG_RUNTIME_DIR + $WAYLAND_DISPLAY) so it does
	// not have to re-implement the path discovery logic.
	WaylandDial func(socketPath string) (WaylandConn, error)

	// SleepCtx is the only sleep primitive permitted in the inject
	// package. Every bounded wait routes through this hook so tests
	// can replace it with a no-op and cancellation propagates cleanly
	// through polling loops and clipboard-restore delays.
	//
	// Implementations return ctx.Err() on cancel, nil on expiry of d,
	// and nil immediately when d <= 0.
	SleepCtx func(ctx context.Context, d time.Duration) error

	// Now returns the current time. Used for audit log timestamps and
	// duration measurement.
	Now func() time.Time
}

// NewDeps returns a Deps populated with production defaults.
func NewDeps() Deps {
	return Deps{
		ExecCommandContext: exec.CommandContext,
		ClipboardRead:      clipboard.ReadAll,
		ClipboardWrite:     clipboard.WriteAll,
		LookPath:           exec.LookPath,
		OSStat:             os.Stat,
		OSOpenFile:         defaultOpenFile,
		OSReadFile:         os.ReadFile,
		OSReadlink:         os.Readlink,
		OSReadDir:          os.ReadDir,
		EnvGet:             os.Getenv,
		WaylandDial:        defaultWaylandDial,
		SleepCtx:           defaultSleepCtx,
		Now:                time.Now,
	}
}

// defaultWaylandDial is the production WaylandDial implementation. It
// opens an AF_UNIX SOCK_STREAM connection to the compositor's socket
// at the resolved path. The detector handles all path resolution from
// environment variables, so this hook stays a thin syscall wrapper.
func defaultWaylandDial(socketPath string) (WaylandConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
}

// defaultOpenFile is the production OSOpenFile implementation. Wrapped
// so the *os.File satisfies io.WriteCloser through the bag's typed
// signature.
func defaultOpenFile(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// defaultSleepCtx is the single production sleep primitive used by the
// inject package. It respects ctx cancellation so an in-flight poll or
// clipboard-restore delay unblocks promptly when the caller cancels.
// The package as a whole stays clean of literal stdlib blocking-sleep
// references — every wait routes through this hook.
func defaultSleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
