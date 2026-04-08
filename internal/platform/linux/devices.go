package linux

import (
	"fmt"

	"github.com/gen2brain/malgo"
	"github.com/hybridz/yap/internal/platform"
)

// deviceLister implements platform.DeviceLister via miniaudio's
// context.Devices(Capture) enumeration. Each call to ListDevices
// initializes a malgo context, walks every input device, and tears the
// context back down. The CLI uses this outside the daemon (no shared
// audio handle), so we cannot rely on the recorder's
// already-initialized malgo session.
type deviceLister struct{}

// NewDeviceLister returns a Linux audio input device lister. It is
// stateless and safe to share across goroutines.
func NewDeviceLister() platform.DeviceLister { return deviceLister{} }

// ListDevices enumerates input-capable audio devices, marking the
// system default. malgo's DeviceInfo.IsDefault flag is the source of
// truth for the default; the lister forwards it without
// reinterpreting.
func (deviceLister) ListDevices() ([]platform.Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("malgo init context: %w", err)
	}
	defer freeMalgoContext(ctx)

	devs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, fmt.Errorf("enumerate audio devices: %w", err)
	}

	out := make([]platform.Device, 0, len(devs))
	for i := range devs {
		// Probe the device for full info to surface a useful
		// description (channel count). Failures here are non-fatal —
		// we still list the device, just without the extra detail.
		desc := "input device"
		if full, fullErr := ctx.DeviceInfo(malgo.Capture, devs[i].ID, malgo.Shared); fullErr == nil {
			if maxCh := maxChannelCount(full.Formats); maxCh > 0 {
				desc = fmt.Sprintf("%dch input", maxCh)
			}
		}
		out = append(out, platform.Device{
			Name:        devs[i].Name(),
			Description: desc,
			IsDefault:   devs[i].IsDefault != 0,
		})
	}
	return out, nil
}

// maxChannelCount returns the largest channel count among the given
// native data formats, or zero if the slice is empty.
func maxChannelCount(formats []malgo.DataFormat) uint32 {
	var max uint32
	for _, f := range formats {
		if f.Channels > max {
			max = f.Channels
		}
	}
	return max
}
