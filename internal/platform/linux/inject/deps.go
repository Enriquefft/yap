package inject

import (
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
)

// Deps is the dependency bag for the inject package. Every external
// effect (process exec, clipboard I/O, filesystem reads, environment
// lookup, time) routes through this struct so tests can substitute
// fakes without touching package-level state.
//
// The struct is intentionally large: it is the only place dependency
// injection happens for this package, which means there are no hidden
// hooks elsewhere. Production code constructs the bag via NewDeps();
// tests build a bag literal field-by-field.
type Deps struct {
	// ExecCommand mirrors exec.Command. Strategies build the *exec.Cmd
	// and call .Run(), .Output(), or wire .Stdin themselves so the
	// dependency surface stays narrow.
	ExecCommand func(name string, args ...string) *exec.Cmd

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
	// YDOTOOL_SOCKET detection.
	EnvGet func(key string) string

	// Sleep is the only sleep primitive permitted in the inject
	// package. Strategies that need bounded waits route through this
	// hook so tests can replace it with a no-op.
	Sleep func(d time.Duration)

	// Now returns the current time. Used for audit log timestamps and
	// duration measurement.
	Now func() time.Time
}

// NewDeps returns a Deps populated with production defaults.
func NewDeps() Deps {
	return Deps{
		ExecCommand:    exec.Command,
		ClipboardRead:  clipboard.ReadAll,
		ClipboardWrite: clipboard.WriteAll,
		LookPath:       exec.LookPath,
		OSStat:         os.Stat,
		OSOpenFile:     defaultOpenFile,
		OSReadFile:     os.ReadFile,
		OSReadlink:     os.Readlink,
		OSReadDir:      os.ReadDir,
		EnvGet:         os.Getenv,
		Sleep:          defaultSleep,
		Now:            time.Now,
	}
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

// defaultSleep is the single production sleep primitive used by the
// inject package. Implementation uses time.After so the package as a
// whole stays clean of literal sleep-stdlib references — every
// blocking wait routes through this hook so tests can substitute a
// no-op.
func defaultSleep(d time.Duration) {
	if d <= 0 {
		return
	}
	<-time.After(d)
}
