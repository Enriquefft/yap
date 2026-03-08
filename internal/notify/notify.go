package notify

import "github.com/gen2brain/beeep"

// notifyFn is the notification backend; swappable in tests.
var notifyFn = beeep.Notify

// Error sends a desktop error notification. Never panics.
// Title is prefixed with "yap: " per CONTEXT.md specifics.
func Error(title, detail string) {
	if err := notifyFn("yap: "+title, detail, ""); err != nil {
		// Log warning but don't propagate — notifications are best-effort
		_ = err
	}
}

// OnTranscriptionError notifies user of a transcription API failure (TRANS-06, NOTIFY-02).
// detail is the exact API error message.
func OnTranscriptionError(err error) {
	Error("transcription failed", err.Error())
}

// OnPermissionError notifies user that /dev/input/event* is not accessible (NOTIFY-02).
func OnPermissionError() {
	Error("hotkey setup failed",
		"permission denied on /dev/input/event* — fix with: usermod -aG input $USER")
}

// OnDeviceError notifies user that no audio input device was found (NOTIFY-02).
func OnDeviceError(err error) {
	Error("audio device error", err.Error())
}
