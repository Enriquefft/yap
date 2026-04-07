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
	devices []platform.Device
	err     error
}

func (f fakeDeviceLister) ListDevices() ([]platform.Device, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.devices, nil
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
	if !strings.Contains(stdout, "DEFAULT") || !strings.Contains(stdout, "NAME") || !strings.Contains(stdout, "DESCRIPTION") {
		t.Errorf("expected table headers, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "default") || !strings.Contains(stdout, "USB Mic") {
		t.Errorf("expected device names, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "*") {
		t.Errorf("expected default-marker asterisk, got:\n%s", stdout)
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
		DeviceLister: fakeDeviceLister{err: errors.New("portaudio is sleeping")},
	}
	_, _, err := runCLIWithPlatform(t, p, "devices")
	if err == nil {
		t.Fatal("expected error from device lister")
	}
	if !strings.Contains(err.Error(), "portaudio is sleeping") {
		t.Errorf("expected lister error to surface, got %v", err)
	}
}
