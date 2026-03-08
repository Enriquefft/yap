package paste

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/atotto/clipboard"
)

// Package-level variables for testability
var (
	execCommand    = exec.Command
	clipboardRead  = clipboard.ReadAll
	clipboardWrite = clipboard.WriteAll
	lookPath     = exec.LookPath
	osStat       = os.Stat
	sleep        = time.Sleep
)

// Paste types text at the cursor using the appropriate display server method.
// Clipboard is saved before paste and restored 100ms after confirmed success (OUTPUT-06, OUTPUT-07).
// On failure, clipboard retains the transcript text for manual paste.
func Paste(text string) error {
	// Save clipboard before paste attempt (OUTPUT-06)
	saved, saveErr := clipboardRead()

	var pasteErr error
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		// Wayland session
		pasteErr = pasteWayland(text)
	} else if os.Getenv("DISPLAY") != "" {
		// X11 session
		pasteErr = pasteX11(text)
	} else {
		// No display server detected
		pasteErr = fmt.Errorf("no display server detected")
	}

	// Clipboard restoration logic (OUTPUT-07)
	if pasteErr == nil && saveErr == nil {
		// Only restore if paste succeeded AND save succeeded
		sleep(100 * time.Millisecond)
		if err := clipboardWrite(saved); err != nil {
			// Log but don't fail - this is best-effort
			fmt.Printf("paste: failed to restore clipboard: %v\n", err)
		}
	}

	// On failure, text is already in clipboard from WriteAll during fallback
	// Or from user re-recording. No restoration needed.
	return pasteErr
}

// pasteWayland tries wtype first, then ydotool (if socket exists), then clipboard-only
func pasteWayland(text string) error {
	// Try wtype first (CONTEXT.md: wtype FIRST, ydotool second)
	if _, err := lookPath("wtype"); err == nil {
		cmd := execCommand("wtype", "--", text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try ydotool if socket exists (OUTPUT-04)
	if canUseYdotool() {
		cmd := execCommand("ydotool", "type", "--", text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Clipboard-only fallback (OUTPUT-02)
	// This is not an error - text is available for manual paste
	if err := clipboardWrite(text); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}
	return nil
}

// pasteX11 uses xdotool with 150ms delay and --clearmodifiers (OUTPUT-03)
func pasteX11(text string) error {
	// 150ms delay for xdotool focus acquisition
	sleep(150 * time.Millisecond)

	// Use --clearmodifiers for layout safety
	cmd := execCommand("xdotool", "type", "--clearmodifiers", "--", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xdotool paste failed: %w", err)
	}
	return nil
}

// canUseYdotool checks if ydotool socket exists and binary is found (OUTPUT-04)
func canUseYdotool() bool {
	// Check socket path (default or env override)
	socketPath := os.Getenv("YDOTOOL_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/.ydotool_socket"
	}

	// Check if socket exists
	if _, err := osStat(socketPath); err != nil {
		return false
	}

	// Check if ydotool binary is available
	if _, err := lookPath("ydotool"); err != nil {
		return false
	}

	return true
}
