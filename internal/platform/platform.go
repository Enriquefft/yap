// Package platform defines interfaces for all OS-specific behavior in yap.
// Each platform (Linux, macOS, Windows) provides a concrete implementation
// of these interfaces. Adding a new platform means implementing these contracts
// in a new platform/<os>/ package — no changes to existing code required.
package platform

import (
	"context"
	"io"
)

// KeyCode is a platform-independent key identifier.
// Maps to evdev.EvCode (uint16) on Linux, CGKeyCode on macOS, WORD on Windows.
type KeyCode uint16

// Key represents a named key with its platform code.
type Key struct {
	Name string
	Code KeyCode
}

// Recorder captures audio from a microphone.
type Recorder interface {
	// Start begins audio capture. Blocks until ctx is cancelled.
	// Accumulated audio is available via Encode() after return.
	Start(ctx context.Context) error

	// Encode returns the captured audio as WAV bytes (16kHz mono 16-bit PCM).
	Encode() ([]byte, error)

	// Close releases all audio resources.
	Close()
}

// NewRecorderFunc creates a Recorder for the given device name.
// Empty string means system default input device.
type NewRecorderFunc func(deviceName string) (Recorder, error)

// ChimePlayer plays short audio feedback sounds.
type ChimePlayer interface {
	// Play plays the given WAV data. Returns immediately; playback is async.
	Play(r io.Reader)
}

// Hotkey listens for keyboard hotkey events at the OS level.
type Hotkey interface {
	// Listen blocks until ctx is cancelled. Calls onPress when the key
	// matching code is pressed, onRelease when it is released.
	// Repeat events (key held down) are ignored.
	Listen(ctx context.Context, key KeyCode, onPress, onRelease func())

	// Close releases any open device handles.
	Close()
}

// NewHotkeyFunc creates a Hotkey listener by scanning for available
// keyboard devices. Returns an error if no keyboards are found or
// permissions are insufficient.
type NewHotkeyFunc func() (Hotkey, error)

// HotkeyConfig provides hotkey name/code resolution for config management
// and the first-run wizard. Separated from Hotkey to allow the wizard to
// use it without needing a full keyboard listener.
type HotkeyConfig interface {
	// ValidKey returns true if name is a valid key identifier on this platform.
	// Example valid names: "KEY_RIGHTCTRL", "KEY_SPACE"
	ValidKey(name string) bool

	// ParseKey converts a key name to its platform KeyCode.
	// Returns an error with example valid names if name is unrecognized.
	ParseKey(name string) (KeyCode, error)

	// DetectKey waits for a physical key press and returns its name.
	// Used by the first-run wizard for interactive hotkey selection.
	// Cancellation via ctx (e.g. timeout).
	DetectKey(ctx context.Context) (string, error)
}

// Paster outputs text at the current cursor position.
// Implementations handle clipboard save/restore internally.
type Paster interface {
	// Paste inserts text at the current cursor position.
	// On failure, text is left in the clipboard for manual paste.
	Paste(text string) error
}

// Notifier sends OS-native desktop notifications. All calls are best-effort.
type Notifier interface {
	// Notify sends a desktop notification with the given title and message.
	// Never panics; errors are silently dropped.
	Notify(title, message string)
}

// Platform bundles all platform-specific implementations.
// Constructed once at startup by the platform factory (e.g. linux.NewPlatform())
// and injected into the daemon and engine.
type Platform struct {
	// NewRecorder is a factory — called with the configured device name at
	// daemon startup. Separated from Recorder so the device name (from config)
	// is not needed when constructing the Platform.
	NewRecorder NewRecorderFunc

	// Chime plays audio feedback sounds (start/stop/warning).
	Chime ChimePlayer

	// NewHotkey is a factory — called at daemon startup to scan for keyboards.
	// Separated from Hotkey so permission errors surface at startup, not construction.
	NewHotkey NewHotkeyFunc

	// HotkeyCfg resolves key names and detects physical key presses.
	// Used by config management commands and the first-run wizard.
	HotkeyCfg HotkeyConfig

	// Paster outputs transcribed text at the cursor.
	Paster Paster

	// Notifier sends desktop error notifications.
	Notifier Notifier
}
