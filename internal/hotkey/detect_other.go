//go:build !linux

package hotkey

import (
	"io"
	"time"
)

// DetectKeyPress uses terminal raw mode to detect a key press on non-Linux platforms.
// Can detect regular keys and function keys but not standalone modifier keys
// (Ctrl, Shift, Alt, Super). For those, users must type the evdev name manually.
func DetectKeyPress(output io.Writer, timeout time.Duration) (string, error) {
	return detectViaTerminal(output, timeout)
}
