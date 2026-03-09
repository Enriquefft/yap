package daemon

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
)

// TestDaemonRunBlocks confirms that Run blocks until SIGTERM.
func TestDaemonRunBlocksStub(t *testing.T) {
	t.Skip("Wave 0 stub — requires injected context for testable signal handling")
}

// TestPIDFileWrittenBeforeAudioInit verifies ordering (DAEMON-01).
func TestPIDFileWrittenBeforeAudioInit(t *testing.T) {
	t.Skip("Wave 0 stub — implement with dependency injection of context")
}

// TestDaemonCleanupOnExit verifies defer cleanup (AUDIO-07, DAEMON-05).
func TestDaemonCleanupOnExit(t *testing.T) {
	t.Skip("Wave 0 stub — requires mock Recorder for deterministic cleanup testing")
}

// TestRecordState verifies recording state machine operations.
func TestRecordState(t *testing.T) {
	var rs recordState

	// Initially not active
	if rs.isActive() {
		t.Error("Record state should be initially inactive")
	}

	// Set active
	rs.setIsActive(true)
	if !rs.isActive() {
		t.Error("Record state should be active after setIsActive(true)")
	}

	// Set cancel function
	cancelCalled := false
	rs.setCancel(func() {
		cancelCalled = true
	})

	// Cancel recording — should invoke the stored cancel function
	rs.cancelRecording()
	if !cancelCalled {
		t.Error("Cancel function should be called by cancelRecording")
	}

	if rs.isActive() {
		t.Error("Record state should be inactive after cancelRecording")
	}

	// Calling cancelRecording again should be safe
	rs.cancelRecording()
}

// TestNew creates a Daemon instance.
func TestNew(t *testing.T) {
	cfg := &config.Config{
		Hotkey:   "KEY_RIGHTCTRL",
		Language: "en",
		APIKey:   "test-key",
	}

	d := New(cfg)
	if d == nil {
		t.Error("New() returned nil")
	}

	if d.cfg != cfg {
		t.Error("Daemon config not set correctly")
	}
}
