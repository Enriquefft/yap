package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hybridz/yap/internal/config"
	"github.com/hybridz/yap/internal/platform"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
	"github.com/hybridz/yap/pkg/yap/transform/fallback"
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

// countingNotifier records how many times Notify was called and
// captures the last payload for assertions.
type countingNotifier struct {
	calls   int32
	title   string
	message string
}

func (c *countingNotifier) Notify(title, message string) {
	atomic.AddInt32(&c.calls, 1)
	c.title = title
	c.message = message
}

var _ platform.Notifier = (*countingNotifier)(nil)

// TestNewTransformer_PassthroughWhenDisabled asserts that a config
// with transform.enabled = false yields the passthrough backend
// regardless of the configured Backend field, and no wrapping takes
// place.
func TestNewTransformer_PassthroughWhenDisabled(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: false,
		Backend: "openai",
		APIURL:  "http://example.invalid/v1",
		Model:   "gpt-4o-mini",
	}
	tr, err := NewTransformer(tc)
	if err != nil {
		t.Fatalf("NewTransformer: %v", err)
	}
	if tr == nil {
		t.Fatal("transformer is nil")
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unexpected fallback wrapping for disabled transform")
	}
}

// TestNewTransformerWithFallback_NoNotifier_NoWrapping asserts that
// a nil notifier returns the primary backend directly, preserving
// the Phase 7 debug-surface semantics of `yap transform`.
func TestNewTransformerWithFallback_NoNotifier_NoWrapping(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "local",
		Model:   "llama3",
	}
	tr, err := NewTransformerWithFallback(tc, nil)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unexpected fallback wrapping when notifier is nil")
	}
}

// TestNewTransformerWithFallback_HealthCheckSuccess_Wraps asserts
// that a healthy backend and live notifier yields a fallback-wrapped
// transformer.
func TestNewTransformerWithFallback_HealthCheckSuccess_Wraps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both /v1/models (openai) and / (local) respond 200.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "openai",
		APIURL:  srv.URL + "/v1",
		Model:   "gpt-4o-mini",
		APIKey:  "sk-test",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if _, ok := tr.(*fallback.Transformer); !ok {
		t.Errorf("transformer type = %T, want *fallback.Transformer", tr)
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 0 {
		t.Errorf("notifier calls = %d, want 0 on healthy check", got)
	}
}

// TestNewTransformerWithFallback_HealthCheckFailure_Notifies asserts
// that a failing health probe fires the notifier exactly once and
// the returned transformer is the passthrough (not wrapped).
func TestNewTransformerWithFallback_HealthCheckFailure_Notifies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "local",
		APIURL:  srv.URL,
		Model:   "llama3",
	}
	notifier := &countingNotifier{}
	tr, err := NewTransformerWithFallback(tc, notifier)
	if err != nil {
		t.Fatalf("NewTransformerWithFallback: %v", err)
	}
	if got := atomic.LoadInt32(&notifier.calls); got != 1 {
		t.Errorf("notifier calls = %d, want 1", got)
	}
	if _, isFallback := tr.(*fallback.Transformer); isFallback {
		t.Errorf("unhealthy backend should yield passthrough, not fallback wrapper")
	}

	// Verify the returned transformer is usable end-to-end: it
	// should forward input verbatim (passthrough semantics).
	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: "hi", IsFinal: true}
	close(in)
	out, err := tr.Transform(context.Background(), in)
	if err != nil {
		t.Fatalf("fallback Transform: %v", err)
	}
	var got []transcribe.TranscriptChunk
	for c := range out {
		got = append(got, c)
	}
	if len(got) != 1 || got[0].Text != "hi" {
		t.Errorf("got = %+v, want passthrough echo", got)
	}
}

// TestNewTransformerWithFallback_UnknownBackend_Errors asserts that a
// bogus backend name is a hard error, not a silent fallback. Users
// who misconfigure the backend name want to know immediately.
func TestNewTransformerWithFallback_UnknownBackend_Errors(t *testing.T) {
	tc := pcfg.TransformConfig{
		Enabled: true,
		Backend: "this-backend-does-not-exist",
		Model:   "x",
	}
	_, err := NewTransformerWithFallback(tc, &countingNotifier{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// Ensure the fallback wrapper still satisfies the transform.Transformer
// interface contract the engine consumes.
func TestFallbackInterfaceSatisfied(t *testing.T) {
	var _ transform.Transformer = (*fallback.Transformer)(nil)
}
