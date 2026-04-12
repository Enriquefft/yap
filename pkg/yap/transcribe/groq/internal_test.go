package groq

import (
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// TestNew_DefaultTimeout asserts the C12 fix: when cfg.Timeout is
// zero (the easy mistake) the constructor substitutes
// DefaultTimeout instead of building &http.Client{Timeout: 0}, which
// would disable the timeout entirely and let a stalled response hang
// the caller forever.
func TestNew_DefaultTimeout(t *testing.T) {
	b, err := New(transcribe.Config{
		APIKey: "k",
		Model:  "m",
		// Timeout intentionally omitted (zero value).
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.client == nil {
		t.Fatal("client is nil")
	}
	if b.client.Timeout != DefaultTimeout {
		t.Errorf("client.Timeout = %v, want %v (default fired)", b.client.Timeout, DefaultTimeout)
	}
}

// TestNew_DefaultTimeout_NegativeAlsoDefaults verifies a negative
// timeout is treated as "use the default" rather than passed through
// to http.Client (which would be a bug-shaped behavior — Go's
// http.Client treats negative durations as "no timeout"
// indistinguishably from zero).
func TestNew_DefaultTimeout_NegativeAlsoDefaults(t *testing.T) {
	b, err := New(transcribe.Config{
		APIKey:  "k",
		Model:   "m",
		Timeout: -1 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.client.Timeout != DefaultTimeout {
		t.Errorf("client.Timeout = %v, want %v", b.client.Timeout, DefaultTimeout)
	}
}

// TestNew_ExplicitTimeoutHonored verifies a positive timeout is
// passed through unchanged.
func TestNew_ExplicitTimeoutHonored(t *testing.T) {
	b, err := New(transcribe.Config{
		APIKey:  "k",
		Model:   "m",
		Timeout: 7 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if b.client.Timeout != 7*time.Second {
		t.Errorf("client.Timeout = %v, want 7s", b.client.Timeout)
	}
}
