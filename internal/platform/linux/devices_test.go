package linux

import (
	"testing"

	"github.com/gen2brain/malgo"
)

// TestNewDeviceLister returns a non-nil DeviceLister suitable for use
// in the Platform composition root. We do not call ListDevices in unit
// tests because malgo enumeration may pop a real audio backend in
// CI environments.
func TestNewDeviceLister(t *testing.T) {
	dl := NewDeviceLister()
	if dl == nil {
		t.Fatal("NewDeviceLister returned nil")
	}
}

// TestPlatformWiresDeviceLister verifies the Linux composition root
// installs the device lister so the CLI's `yap devices` command can
// always reach an enumerator.
func TestPlatformWiresDeviceLister(t *testing.T) {
	p := NewPlatform()
	if p.DeviceLister == nil {
		t.Fatal("NewPlatform did not wire DeviceLister")
	}
}

// TestBackendDisplayName covers every backend the Linux platform can
// realistically return plus the unknown fallback branch. This is a
// pure lookup table so no audio subsystem is touched.
func TestBackendDisplayName(t *testing.T) {
	cases := []struct {
		in   malgo.Backend
		want string
	}{
		{malgo.BackendPulseaudio, "PulseAudio"},
		{malgo.BackendAlsa, "ALSA"},
		{malgo.BackendJack, "JACK"},
		{malgo.BackendNull, "Null"},
	}
	for _, tc := range cases {
		if got := backendDisplayName(tc.in); got != tc.want {
			t.Errorf("backendDisplayName(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// An unexpected backend value must not panic; it must format a
	// distinguishable "unknown (N)" marker so the `yap devices` output
	// still parses unambiguously.
	if got := backendDisplayName(malgo.Backend(0xFFFF)); got == "" {
		t.Errorf("backendDisplayName(unknown) returned empty string")
	}
}

// TestPreferredBackendsOrder pins the backend preference order so
// future edits trip the test instead of silently reshuffling the
// miniaudio backend selection on Linux — critical because the order
// directly controls which backend users get when several are
// available on the same host.
func TestPreferredBackendsOrder(t *testing.T) {
	want := []malgo.Backend{
		malgo.BackendPulseaudio,
		malgo.BackendAlsa,
		malgo.BackendJack,
	}
	if len(preferredBackends) != len(want) {
		t.Fatalf("preferredBackends length = %d, want %d", len(preferredBackends), len(want))
	}
	for i, b := range want {
		if preferredBackends[i] != b {
			t.Errorf("preferredBackends[%d] = %v, want %v", i, preferredBackends[i], b)
		}
	}
}

// TestDeviceListerImplementsBackend is a compile-time pin that the
// Linux device lister satisfies the full platform.DeviceLister
// interface, including the Backend() method added for the `yap
// devices` diagnostic header.
func TestDeviceListerImplementsBackend(t *testing.T) {
	type backendCapable interface {
		Backend() (string, error)
	}
	var _ backendCapable = deviceLister{}
}
