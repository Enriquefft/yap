package linux

import (
	"context"
	"fmt"

	"github.com/gordonklaus/portaudio"
	"github.com/hybridz/yap/internal/platform"
)

const framesPerBuffer = 512 // ~32ms at 16kHz

// recorder implements platform.Recorder using PortAudio.
// PCM frames accumulate in an in-memory []int16 slice — no temp files.
type recorder struct {
	deviceName string
	frames     []int16
}

// NewRecorder creates a platform.Recorder and validates that at least one
// audio input device is available. Returns a clear error on PipeWire-only
// systems with zero input devices.
func NewRecorder(deviceName string) (platform.Recorder, error) {
	if err := portaudio.Initialize(); err != nil {
		return nil, fmt.Errorf("portaudio init: %w", err)
	}

	devs, err := portaudio.Devices()
	if err != nil {
		portaudio.Terminate() //nolint:errcheck
		return nil, fmt.Errorf("enumerate audio devices: %w", err)
	}
	inputCount := 0
	for _, d := range devs {
		if d.MaxInputChannels > 0 {
			inputCount++
		}
	}
	if inputCount == 0 {
		portaudio.Terminate() //nolint:errcheck
		return nil, fmt.Errorf(
			"no audio input devices available — " +
				"on PipeWire systems enable pipewire-alsa " +
				"(NixOS: services.pipewire.alsa.enable = true)",
		)
	}

	return &recorder{deviceName: deviceName}, nil
}

// Close terminates the PortAudio session.
func (r *recorder) Close() {
	portaudio.Terminate() //nolint:errcheck
}

// Start begins audio capture. Blocks until ctx is cancelled.
// Uses a blocking stream.Read() loop — no Go channel inside PortAudio callback.
func (r *recorder) Start(ctx context.Context) error {
	// Pre-allocate for up to 60s of audio to avoid realloc during capture.
	r.frames = make([]int16, 0, sampleRate*60)

	in := make([]int16, framesPerBuffer)

	var (
		stream *portaudio.Stream
		err    error
	)
	if r.deviceName != "" {
		dev, err := selectInputDevice(r.deviceName)
		if err != nil {
			return fmt.Errorf("select input device: %w", err)
		}
		stream, err = portaudio.OpenStream(portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   dev,
				Channels: 1,
				Latency:  dev.DefaultLowInputLatency,
			},
			SampleRate:      float64(sampleRate),
			FramesPerBuffer: framesPerBuffer,
		}, &in)
		if err != nil {
			return fmt.Errorf("open audio stream (device %q): %w", r.deviceName, err)
		}
	} else {
		stream, err = portaudio.OpenDefaultStream(1, 0, float64(sampleRate), framesPerBuffer, &in)
		if err != nil {
			return fmt.Errorf("open audio stream: %w", err)
		}
	}
	defer stream.Close() //nolint:errcheck

	if err := stream.Start(); err != nil {
		return fmt.Errorf("start audio stream: %w", err)
	}
	defer stream.Stop() //nolint:errcheck

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if err := stream.Read(); err != nil {
			return fmt.Errorf("audio stream read: %w", err)
		}
		r.frames = append(r.frames, in...)
	}
}

// Encode encodes accumulated frames to WAV bytes. Returns error if no audio was captured.
func (r *recorder) Encode() ([]byte, error) {
	if len(r.frames) == 0 {
		return nil, fmt.Errorf("no audio frames to encode")
	}
	return encodeWAV(r.frames)
}

// selectInputDevice finds a named input device from portaudio.Devices().
func selectInputDevice(name string) (*portaudio.DeviceInfo, error) {
	devs, err := portaudio.Devices()
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	for _, d := range devs {
		if d.Name == name && d.MaxInputChannels > 0 {
			return d, nil
		}
	}
	return nil, fmt.Errorf("audio device %q not found or has no input channels", name)
}
