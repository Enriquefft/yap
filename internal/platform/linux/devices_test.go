package linux

import (
	"testing"
)

// TestNewDeviceLister returns a non-nil DeviceLister suitable for use
// in the Platform composition root. We do not call ListDevices in unit
// tests because PortAudio enumeration may pop a real audio backend in
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
