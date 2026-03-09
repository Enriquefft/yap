package paste

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"
)

// Package-level variables for testability
var (
	execCommand    = exec.Command
	clipboardRead  = clipboard.ReadAll
	clipboardWrite = clipboard.WriteAll
	lookPath       = exec.LookPath
	osStat         = os.Stat
	sleep          = time.Sleep
)

// Paste copies text to clipboard, then simulates a paste keystroke.
// Uses Ctrl+Shift+V which works in both terminals and most GUI apps.
// Clipboard is saved before and restored after paste.
// On failure, text remains in clipboard for manual paste.
func Paste(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil // Nothing to paste
	}

	// Save clipboard before paste attempt
	saved, saveErr := clipboardRead()

	// Copy text to clipboard
	if err := clipboardWrite(text); err != nil {
		return fmt.Errorf("clipboard write failed: %w", err)
	}

	// Small delay for clipboard to settle
	sleep(50 * time.Millisecond)

	// Simulate paste keystroke
	var pasteErr error
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		pasteErr = pasteWayland()
	} else if os.Getenv("DISPLAY") != "" {
		pasteErr = pasteX11()
	} else {
		// No display server — text is in clipboard for manual paste
		return nil
	}

	if pasteErr != nil {
		// Paste keystroke failed — text remains in clipboard for manual paste
		return nil
	}

	// Restore clipboard after successful paste
	if saveErr == nil {
		sleep(100 * time.Millisecond)
		_ = clipboardWrite(saved) // best-effort
	}

	return nil
}

// pasteWayland simulates Ctrl+Shift+V via wtype.
// Works in terminals and most GUI apps on Wayland.
func pasteWayland() error {
	if _, err := lookPath("wtype"); err == nil {
		cmd := execCommand("wtype", "-M", "ctrl", "-M", "shift", "-k", "v", "-m", "shift", "-m", "ctrl")
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Try ydotool as fallback
	if canUseYdotool() {
		cmd := execCommand("ydotool", "key", "29:1", "42:1", "47:1", "47:0", "42:0", "29:0") // Ctrl+Shift+V
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("no paste tool available (install wtype or ydotool)")
}

// pasteX11 simulates Ctrl+Shift+V via xdotool.
func pasteX11() error {
	sleep(150 * time.Millisecond)
	cmd := execCommand("xdotool", "key", "--clearmodifiers", "ctrl+shift+v")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xdotool paste failed: %w", err)
	}
	return nil
}

// canUseYdotool checks if ydotool socket exists and binary is found.
func canUseYdotool() bool {
	socketPath := os.Getenv("YDOTOOL_SOCKET")
	if socketPath == "" {
		socketPath = "/tmp/.ydotool_socket"
	}
	if _, err := osStat(socketPath); err != nil {
		return false
	}
	if _, err := lookPath("ydotool"); err != nil {
		return false
	}
	return true
}
