package linux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/hybridz/yap/internal/platform"
)

// pasterDeps holds injectable dependencies for paster (used in tests).
type pasterDeps struct {
	execCommand    func(name string, args ...string) *exec.Cmd
	clipboardRead  func() (string, error)
	clipboardWrite func(text string) error
	lookPath       func(file string) (string, error)
	osStat         func(name string) (os.FileInfo, error)
	sleep          func(d time.Duration)
}

// paster implements platform.Paster for Linux using display-server-aware input simulation.
type paster struct {
	deps pasterDeps
}

// NewPaster returns a Paster for Linux that handles Wayland and X11.
func NewPaster() platform.Paster {
	return &paster{
		deps: pasterDeps{
			execCommand:    exec.Command,
			clipboardRead:  clipboard.ReadAll,
			clipboardWrite: clipboard.WriteAll,
			lookPath:       exec.LookPath,
			osStat:         os.Stat,
			sleep:          time.Sleep,
		},
	}
}

// newPasterWithDeps creates a paster with injected deps (for tests).
func newPasterWithDeps(deps pasterDeps) platform.Paster {
	return &paster{deps: deps}
}

// Paste copies text to clipboard then simulates a paste keystroke (Ctrl+Shift+V).
// Saves and restores the previous clipboard content around the paste operation.
// On failure, text remains in clipboard for manual paste.
func (p *paster) Paste(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// Save clipboard before paste.
	saved, saveErr := p.deps.clipboardRead()

	// Copy text to clipboard.
	if err := p.deps.clipboardWrite(text); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}

	p.deps.sleep(50 * time.Millisecond)

	// Simulate paste keystroke based on display server.
	var pasteErr error
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		pasteErr = p.pasteWayland()
	} else if os.Getenv("DISPLAY") != "" {
		pasteErr = p.pasteX11()
	} else {
		// No display server — text remains in clipboard for manual paste.
		return nil
	}

	if pasteErr != nil {
		// Paste keystroke failed — text remains in clipboard.
		return nil
	}

	// Restore clipboard after successful paste.
	if saveErr == nil {
		p.deps.sleep(100 * time.Millisecond)
		_ = p.deps.clipboardWrite(saved) // best-effort
	}

	return nil
}

// pasteWayland simulates Ctrl+Shift+V on Wayland via wtype, falling back to ydotool.
func (p *paster) pasteWayland() error {
	if _, err := p.deps.lookPath("wtype"); err == nil {
		cmd := p.deps.execCommand("wtype", "-M", "ctrl", "-M", "shift", "-k", "v", "-m", "shift", "-m", "ctrl")
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	if p.canUseYdotool() {
		cmd := p.deps.execCommand("ydotool", "key", "29:1", "42:1", "47:1", "47:0", "42:0", "29:0")
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no paste tool available (install wtype or ydotool)")
}

// pasteX11 simulates Ctrl+Shift+V on X11 via xdotool.
func (p *paster) pasteX11() error {
	p.deps.sleep(150 * time.Millisecond)
	cmd := p.deps.execCommand("xdotool", "key", "--clearmodifiers", "ctrl+shift+v")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xdotool paste failed: %w", err)
	}
	return nil
}

// canUseYdotool checks if the ydotool socket and binary are available.
func (p *paster) canUseYdotool() bool {
	socketPath := os.Getenv("YDOTOOL_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/.ydotool_socket"
	}
	if _, err := p.deps.osStat(socketPath); err != nil {
		return false
	}
	if _, err := p.deps.lookPath("ydotool"); err != nil {
		return false
	}
	return true
}
