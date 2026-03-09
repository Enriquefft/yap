package hotkey

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// termKeyToEvdev maps terminal raw bytes/sequences to evdev key names.
// This covers the most common keys detectable through terminal raw mode.
var termKeyToEvdev = map[byte]string{
	// Printable keys (lowercase)
	'a': "KEY_A", 'b': "KEY_B", 'c': "KEY_C", 'd': "KEY_D",
	'e': "KEY_E", 'f': "KEY_F", 'g': "KEY_G", 'h': "KEY_H",
	'i': "KEY_I", 'j': "KEY_J", 'k': "KEY_K", 'l': "KEY_L",
	'm': "KEY_M", 'n': "KEY_N", 'o': "KEY_O", 'p': "KEY_P",
	'q': "KEY_Q", 'r': "KEY_R", 's': "KEY_S", 't': "KEY_T",
	'u': "KEY_U", 'v': "KEY_V", 'w': "KEY_W", 'x': "KEY_X",
	'y': "KEY_Y", 'z': "KEY_Z",
	// Numbers
	'0': "KEY_0", '1': "KEY_1", '2': "KEY_2", '3': "KEY_3",
	'4': "KEY_4", '5': "KEY_5", '6': "KEY_6", '7': "KEY_7",
	'8': "KEY_8", '9': "KEY_9",
	// Special keys
	' ':  "KEY_SPACE",
	'\t': "KEY_TAB",
	'\r': "KEY_ENTER",
	'\n': "KEY_ENTER",
	127:  "KEY_BACKSPACE", // DEL
	8:    "KEY_BACKSPACE", // BS
	// Punctuation
	'-': "KEY_MINUS", '=': "KEY_EQUAL",
	'[': "KEY_LEFTBRACE", ']': "KEY_RIGHTBRACE",
	';': "KEY_SEMICOLON", '\'': "KEY_APOSTROPHE",
	'`': "KEY_GRAVE", '\\': "KEY_BACKSLASH",
	',': "KEY_COMMA", '.': "KEY_DOT",
	'/': "KEY_SLASH",
}

// escSeqToEvdev maps terminal escape sequences to evdev key names.
var escSeqToEvdev = map[string]string{
	// Arrow keys
	"[A": "KEY_UP", "[B": "KEY_DOWN",
	"[C": "KEY_RIGHT", "[D": "KEY_LEFT",
	// Function keys (common xterm sequences)
	"OP": "KEY_F1", "OQ": "KEY_F2", "OR": "KEY_F3", "OS": "KEY_F4",
	"[15~": "KEY_F5", "[17~": "KEY_F6", "[18~": "KEY_F7", "[19~": "KEY_F8",
	"[20~": "KEY_F9", "[21~": "KEY_F10", "[23~": "KEY_F11", "[24~": "KEY_F12",
	// Navigation
	"[2~": "KEY_INSERT", "[3~": "KEY_DELETE",
	"[H": "KEY_HOME", "[F": "KEY_END",
	"[5~": "KEY_PAGEUP", "[6~": "KEY_PAGEDOWN",
}

// detectViaTerminal uses terminal raw mode to detect a key press.
// Works cross-platform (Linux, macOS, Windows) but cannot detect
// standalone modifier keys (Ctrl, Shift, Alt, Super).
func detectViaTerminal(output io.Writer, timeout time.Duration) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("stdin is not a terminal")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	// Set read deadline via goroutine
	buf := make([]byte, 32)
	ch := make(chan int, 1)
	errCh := make(chan error, 1)

	go func() {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			errCh <- err
			return
		}
		ch <- n
	}()

	select {
	case n := <-ch:
		return mapTerminalKey(buf[:n])
	case err := <-errCh:
		return "", fmt.Errorf("read key: %w", err)
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for key press")
	}
}

// mapTerminalKey maps raw terminal bytes to an evdev key name.
func mapTerminalKey(buf []byte) (string, error) {
	if len(buf) == 0 {
		return "", fmt.Errorf("empty input")
	}

	// Single byte — check direct mapping
	if len(buf) == 1 {
		b := buf[0]

		// Escape key itself
		if b == 27 {
			return "KEY_ESC", nil
		}

		// Ctrl+letter (0x01-0x1A maps to Ctrl+A through Ctrl+Z)
		// We map these to the letter key since the user pressed it
		if b >= 1 && b <= 26 {
			return termKeyToEvdev[b+'a'-1], nil
		}

		if name, ok := termKeyToEvdev[b]; ok {
			return name, nil
		}

		// Uppercase — still map to the key
		if b >= 'A' && b <= 'Z' {
			return termKeyToEvdev[b+'a'-'A'], nil
		}

		return "", fmt.Errorf("unrecognized key (byte %d)", b)
	}

	// Multi-byte: escape sequence (starts with ESC=0x1B)
	if buf[0] == 27 && len(buf) > 1 {
		seq := string(buf[1:])
		if name, ok := escSeqToEvdev[seq]; ok {
			return name, nil
		}
		return "", fmt.Errorf("unrecognized escape sequence: %q", seq)
	}

	return "", fmt.Errorf("unrecognized key sequence: %v", buf)
}
