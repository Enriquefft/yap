package linux

import (
	"fmt"

	"github.com/gordonklaus/portaudio"
	"github.com/hybridz/yap/internal/platform"
)

// deviceLister implements platform.DeviceLister via PortAudio's
// Devices() enumeration. Each call to ListDevices initializes
// PortAudio, walks every input-capable device, marks the system
// default, and tears the host API back down. The CLI uses this
// outside the daemon (no shared audio handle), so we cannot rely on
// the recorder's already-initialized PortAudio session.
type deviceLister struct{}

// NewDeviceLister returns a Linux audio input device lister. It is
// stateless and safe to share across goroutines.
func NewDeviceLister() platform.DeviceLister { return deviceLister{} }

// ListDevices enumerates input-capable audio devices, marking the
// system default. Non-input devices (output-only sinks) are filtered
// out so the resulting list matches what general.audio_device may
// resolve against.
func (deviceLister) ListDevices() ([]platform.Device, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("portaudio init: %w", err)
	}
	defer portaudio.Terminate() //nolint:errcheck

	devs, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("enumerate audio devices: %w", err)
	}

	// Resolve the platform default input device. The default-host
	// API call may fail on systems without an audio backend; in that
	// case we simply do not mark any device as default.
	var defaultDev *portaudio.DeviceInfo
	if hostAPI, hostErr := portaudio.DefaultHostApi(); hostErr == nil && hostAPI != nil {
		defaultDev = hostAPI.DefaultInputDevice
	}

	out := make([]platform.Device, 0, len(devs))
	for _, d := range devs {
		if d == nil || d.MaxInputChannels < 1 {
			continue
		}
		desc := fmt.Sprintf("%dch input", d.MaxInputChannels)
		if d.HostApi != nil && d.HostApi.Name != "" {
			desc = fmt.Sprintf("%s @ %s", desc, d.HostApi.Name)
		}
		out = append(out, platform.Device{
			Name:        d.Name,
			Description: desc,
			IsDefault:   defaultDev != nil && d == defaultDev,
		})
	}
	return out, nil
}
