package linux

import (
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopSleep(time.Duration) {}

func makeDeps(execFn func(string, ...string) *exec.Cmd, readFn func() (string, error), writeFn func(string) error, lookFn func(string) (string, error), statFn func(string) (os.FileInfo, error)) pasterDeps {
	return pasterDeps{
		execCommand:    execFn,
		clipboardRead:  readFn,
		clipboardWrite: writeFn,
		lookPath:       lookFn,
		osStat:         statFn,
		sleep:          noopSleep,
	}
}

func TestPaster_EmptyTextIsNoop(t *testing.T) {
	p := newPasterWithDeps(makeDeps(
		exec.Command,
		func() (string, error) { return "", nil },
		func(string) error { return nil },
		exec.LookPath,
		os.Stat,
	))
	err := p.Paste("   ")
	assert.NoError(t, err)
}

func TestPaster_ClipboardWriteFailureReturnsError(t *testing.T) {
	p := newPasterWithDeps(makeDeps(
		exec.Command,
		func() (string, error) { return "prev", nil },
		func(string) error { return assert.AnError },
		exec.LookPath,
		os.Stat,
	))
	err := p.Paste("hello")
	assert.Error(t, err)
}

func TestPaster_NoDisplayServer_NoError(t *testing.T) {
	// With no WAYLAND_DISPLAY or DISPLAY set, Paste should succeed silently.
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")

	written := ""
	p := newPasterWithDeps(makeDeps(
		exec.Command,
		func() (string, error) { return "prev", nil },
		func(text string) error { written = text; return nil },
		exec.LookPath,
		os.Stat,
	))
	err := p.Paste("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", written)
}

func TestPaster_WaylandUsesWtype(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", ":wayland-0")
	t.Setenv("DISPLAY", "")

	var ran string
	p := newPasterWithDeps(pasterDeps{
		execCommand: func(name string, args ...string) *exec.Cmd {
			ran = name
			// Return a no-op command that succeeds
			return exec.Command("true")
		},
		clipboardRead:  func() (string, error) { return "", nil },
		clipboardWrite: func(string) error { return nil },
		lookPath:       func(name string) (string, error) { return "/usr/bin/" + name, nil },
		osStat:         os.Stat,
		sleep:          noopSleep,
	})
	err := p.Paste("hello")
	assert.NoError(t, err)
	assert.Equal(t, "wtype", ran)
}

func TestPaster_X11UsesXdotool(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")

	var ran string
	p := newPasterWithDeps(pasterDeps{
		execCommand: func(name string, args ...string) *exec.Cmd {
			ran = name
			return exec.Command("true")
		},
		clipboardRead:  func() (string, error) { return "", nil },
		clipboardWrite: func(string) error { return nil },
		lookPath:       exec.LookPath,
		osStat:         os.Stat,
		sleep:          noopSleep,
	})
	err := p.Paste("hello")
	assert.NoError(t, err)
	assert.Equal(t, "xdotool", ran)
}

func TestPaster_ClipboardRestored(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", ":0")

	var writes []string
	p := newPasterWithDeps(pasterDeps{
		execCommand:    func(name string, args ...string) *exec.Cmd { return exec.Command("true") },
		clipboardRead:  func() (string, error) { return "original", nil },
		clipboardWrite: func(text string) error { writes = append(writes, text); return nil },
		lookPath:       exec.LookPath,
		osStat:         os.Stat,
		sleep:          noopSleep,
	})
	err := p.Paste("new text")
	require.NoError(t, err)
	require.Len(t, writes, 2)
	assert.Equal(t, "new text", writes[0])
	assert.Equal(t, "original", writes[1])
}
