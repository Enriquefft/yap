package cli_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/platform"
)

// fakeDeviceLister implements platform.DeviceLister for tests.
type fakeDeviceLister struct {
	devices    []platform.Device
	backend    string
	backendErr error
	err        error
}

func (f fakeDeviceLister) ListDevices() ([]platform.Device, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.devices, nil
}

func (f fakeDeviceLister) Backend() (string, error) {
	if f.backendErr != nil {
		return "", f.backendErr
	}
	if f.backend == "" {
		return "TestBackend", nil
	}
	return f.backend, nil
}

func withCleanConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
}

func TestDevices_HappyPath(t *testing.T) {
	withCleanConfig(t)
	p := platform.Platform{
		DeviceLister: fakeDeviceLister{
			backend: "PulseAudio",
			devices: []platform.Device{
				{Name: "default", Description: "1ch input @ pulse", IsDefault: true},
				{Name: "USB Mic", Description: "2ch input @ alsa"},
			},
		},
	}
	stdout, _, err := runCLIWithPlatform(t, p, "devices")
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if !strings.Contains(stdout, "BACKEND: PulseAudio") {
		t.Errorf("expected BACKEND header, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "DEFAULT") || !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "DESCRIPTION") {
		t.Errorf("expected table headers, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "default") || !strings.Contains(stdout, "USB Mic") {
		t.Errorf("expected device names, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "*") {
		t.Errorf("expected default-marker asterisk, got:\n%s", stdout)
	}
	// BACKEND header must precede the table so users see the
	// diagnostic context before the device list.
	backendIdx := strings.Index(stdout, "BACKEND:")
	defaultIdx := strings.Index(stdout, "DEFAULT")
	if backendIdx < 0 || defaultIdx < 0 || backendIdx > defaultIdx {
		t.Errorf("expected BACKEND header before table, got:\n%s", stdout)
	}
}

func TestDevices_NilDeviceLister(t *testing.T) {
	withCleanConfig(t)
	p := platform.Platform{} // no DeviceLister
	_, _, err := runCLIWithPlatform(t, p, "devices")
	if err == nil {
		t.Fatal("expected error when DeviceLister is nil")
	}
	if !strings.Contains(err.Error(), "enumeration") {
		t.Errorf("expected enumeration error, got %v", err)
	}
}

func TestDevices_ListDevicesError(t *testing.T) {
	withCleanConfig(t)
	p := platform.Platform{
		DeviceLister: fakeDeviceLister{err: errors.New("audio enumeration failed")},
	}
	_, _, err := runCLIWithPlatform(t, p, "devices")
	if err == nil {
		t.Fatal("expected error from device lister")
	}
	if !strings.Contains(err.Error(), "audio enumeration failed") {
		t.Errorf("expected lister error to surface, got %v", err)
	}
}

func TestDevices_BackendError(t *testing.T) {
	withCleanConfig(t)
	p := platform.Platform{
		DeviceLister: fakeDeviceLister{backendErr: errors.New("no usable audio backend")},
	}
	_, _, err := runCLIWithPlatform(t, p, "devices")
	if err == nil {
		t.Fatal("expected error when Backend() fails")
	}
	if !strings.Contains(err.Error(), "no usable audio backend") {
		t.Errorf("expected backend error to surface, got %v", err)
	}
	if !strings.Contains(err.Error(), "devices: backend:") {
		t.Errorf("expected 'devices: backend:' prefix, got %v", err)
	}
}
