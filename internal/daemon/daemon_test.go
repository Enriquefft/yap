package daemon

import (
	"testing"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/platform"
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

// TestInjectionOptionsFromConfigBridge guards the structural mapping
// the daemon performs between the on-disk pcfg.InjectionConfig and
// the runtime platform.InjectionOptions. The fields are intentionally
// 1:1; the test fails the build if a future schema change forgets to
// extend either side.
func TestInjectionOptionsFromConfigBridge(t *testing.T) {
	in := pcfg.InjectionConfig{
		PreferOSC52:      true,
		BracketedPaste:   false,
		ElectronStrategy: "clipboard",
		AppOverrides: []pcfg.AppOverride{
			{Match: "kitty", Strategy: "osc52"},
			{Match: "code", Strategy: "clipboard"},
		},
	}
	got := InjectionOptionsFromConfig(in)
	want := platform.InjectionOptions{
		PreferOSC52:      true,
		BracketedPaste:   false,
		ElectronStrategy: "clipboard",
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "osc52"},
			{Match: "code", Strategy: "clipboard"},
		},
	}
	if got.PreferOSC52 != want.PreferOSC52 {
		t.Errorf("PreferOSC52 = %v, want %v", got.PreferOSC52, want.PreferOSC52)
	}
	if got.BracketedPaste != want.BracketedPaste {
		t.Errorf("BracketedPaste = %v, want %v", got.BracketedPaste, want.BracketedPaste)
	}
	if got.ElectronStrategy != want.ElectronStrategy {
		t.Errorf("ElectronStrategy = %q, want %q", got.ElectronStrategy, want.ElectronStrategy)
	}
	if len(got.AppOverrides) != len(want.AppOverrides) {
		t.Fatalf("AppOverrides len = %d, want %d", len(got.AppOverrides), len(want.AppOverrides))
	}
	for i := range got.AppOverrides {
		if got.AppOverrides[i] != want.AppOverrides[i] {
			t.Errorf("AppOverrides[%d] = %+v, want %+v", i, got.AppOverrides[i], want.AppOverrides[i])
		}
	}
}

// TestInjectionOptionsFromConfigEmptyOverrides guards the nil-vs-empty
// behavior: an empty config slice produces a nil platform slice (no
// allocation), preserving the zero-cost path for the common case where
// the user has no app overrides configured.
func TestInjectionOptionsFromConfigEmptyOverrides(t *testing.T) {
	in := pcfg.InjectionConfig{
		PreferOSC52:      true,
		BracketedPaste:   true,
		ElectronStrategy: "clipboard",
	}
	got := InjectionOptionsFromConfig(in)
	if got.AppOverrides != nil {
		t.Errorf("AppOverrides = %v, want nil", got.AppOverrides)
	}
}
