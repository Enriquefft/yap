package notify

import (
	"testing"
)

// TestNotifyError verifies that notify.Error sends notifications with proper formatting
func TestNotifyError(t *testing.T) {
	// Capture the notification calls
	var gotTitle, gotMessage string

	// Swap the notify function with a capture function
	oldNotifyFn := notifyFn
	notifyFn = func(title, message string, icon any) error {
		gotTitle = title
		gotMessage = message
		return nil
	}
	defer func() { notifyFn = oldNotifyFn }()

	Error("test error", "test detail message")

	if gotTitle != "yap: test error" {
		t.Errorf("got title %q, want %q", gotTitle, "yap: test error")
	}
	if gotMessage != "test detail message" {
		t.Errorf("got message %q, want %q", gotMessage, "test detail message")
	}
}

// TestOnTranscriptionError verifies transcription error formatting
func TestOnTranscriptionError(t *testing.T) {
	var gotTitle, gotMessage string

	oldNotifyFn := notifyFn
	notifyFn = func(title, message string, icon any) error {
		gotTitle = title
		gotMessage = message
		return nil
	}
	defer func() { notifyFn = oldNotifyFn }()

	OnTranscriptionError(testError{msg: "API rate limit exceeded"})

	if gotTitle != "yap: transcription failed" {
		t.Errorf("got title %q, want %q", gotTitle, "yap: transcription failed")
	}
	if gotMessage != "API rate limit exceeded" {
		t.Errorf("got message %q, want %q", gotMessage, "API rate limit exceeded")
	}
}

// TestOnPermissionError verifies permission error includes usermod command
func TestOnPermissionError(t *testing.T) {
	var gotTitle, gotMessage string

	oldNotifyFn := notifyFn
	notifyFn = func(title, message string, icon any) error {
		gotTitle = title
		gotMessage = message
		return nil
	}
	defer func() { notifyFn = oldNotifyFn }()

	OnPermissionError()

	if gotTitle != "yap: hotkey setup failed" {
		t.Errorf("got title %q, want %q", gotTitle, "yap: hotkey setup failed")
	}
	expectedDetail := "permission denied on /dev/input/event* — fix with: usermod -aG input $USER"
	if gotMessage != expectedDetail {
		t.Errorf("got message %q, want %q", gotMessage, expectedDetail)
	}
}

// TestOnDeviceError verifies device error formatting with detail
func TestOnDeviceError(t *testing.T) {
	var gotTitle, gotMessage string

	oldNotifyFn := notifyFn
	notifyFn = func(title, message string, icon any) error {
		gotTitle = title
		gotMessage = message
		return nil
	}
	defer func() { notifyFn = oldNotifyFn }()

	OnDeviceError(testError{msg: "no audio device found"})

	if gotTitle != "yap: audio device error" {
		t.Errorf("got title %q, want %q", gotTitle, "yap: audio device error")
	}
	if gotMessage != "no audio device found" {
		t.Errorf("got message %q, want %q", gotMessage, "no audio device found")
	}
}

// TestNotifyNoPanic verifies that notification errors don't panic
func TestNotifyNoPanic(t *testing.T) {
	oldNotifyFn := notifyFn
	notifyFn = func(title, message string, icon any) error {
		return testError{msg: "notification backend failed"}
	}
	defer func() { notifyFn = oldNotifyFn }()

	// This should not panic
	Error("test", "detail")
	OnTranscriptionError(testError{msg: "error"})
	OnPermissionError()
	OnDeviceError(testError{msg: "error"})

	// If we reach here, no panic occurred
	t.Log("No panic occurred with failing notification backend")
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e testError) Error() string {
	return e.msg
}
