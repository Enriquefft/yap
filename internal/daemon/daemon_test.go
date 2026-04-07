package daemon

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// TestRecordState verifies recording state machine operations.
func TestRecordState(t *testing.T) {
	var rs recordState

	if rs.isActive() {
		t.Error("Record state should be initially inactive")
	}

	rs.setIsActive(true)
	if !rs.isActive() {
		t.Error("Record state should be active after setIsActive(true)")
	}

	cancelCalled := false
	rs.setCancel(func() {
		cancelCalled = true
	})

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

// TestNew creates a Daemon instance with a nested config.
func TestNew(t *testing.T) {
	cfg := pcfg.DefaultConfig()
	cfg.General.Hotkey = "KEY_RIGHTCTRL"
	cfg.Transcription.Language = "en"
	cfg.Transcription.APIKey = "test-key"

	c := config.Config(cfg)
	d := New(&c)
	if d == nil {
		t.Error("New() returned nil")
	}
	if d.cfg != &c {
		t.Error("Daemon config not set correctly")
	}
}
