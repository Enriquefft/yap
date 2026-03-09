package audio

import (
	"context"
	"fmt"

	"github.com/gordonklaus/portaudio"
)

const framesPerBuffer = 512 // ~32ms at 16kHz

// AudioRecorder is the interface implemented by Recorder (real PortAudio) and fakeRecorder (tests).
// Callers record audio via Start/Stop then encode via Encode.
type AudioRecorder interface {
	Start(ctx context.Context) error
	Stop() error
	Frames() []int16
	Encode() ([]byte, error)
}

// Recorder captures audio from the system default input device (or config-named device)
// using a blocking PortAudio stream. PCM frames accumulate in an in-memory []int16 slice.
// No temp files are created at any point in the recording or encoding path.
type Recorder struct {
	deviceName string  // empty = use system default (AUDIO-01)
	frames     []int16 // accumulated PCM — never written to disk (AUDIO-03, NFR-06)
}

// NewRecorder creates a Recorder and validates that at least one audio input device is
// available after portaudio.Initialize(). Returns a clear error on PipeWire-only systems
// with 0 input devices (AUDIO-02).
func NewRecorder(deviceName string) (*Recorder, error) {
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

	return &Recorder{deviceName: deviceName}, nil
}

// Close terminates the PortAudio session. Must be called when done with the Recorder.
func (r *Recorder) Close() {
	portaudio.Terminate() //nolint:errcheck
}

// Start begins audio capture. Blocks the current goroutine reading from the PortAudio
// blocking stream until ctx is cancelled. Accumulated frames available via Frames().
// AUDIO-04: uses blocking stream.Read() loop — no Go channel inside PortAudio callback.
func (r *Recorder) Start(ctx context.Context) error {
	// Pre-allocate for up to 60 seconds of audio to avoid realloc during capture.
	// 16000 samples/sec * 60s = 960000 samples = ~1.88MB (AUDIO-03: in memory only)
	r.frames = make([]int16, 0, sampleRate*60)

	in := make([]int16, framesPerBuffer)

	var stream *portaudio.Stream
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
		var err error
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

	// Blocking read loop — AUDIO-04: no channel, no callback, plain loop.
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

// Stop is a no-op: callers cancel the context passed to Start.
// Exists to satisfy the AudioRecorder interface.
func (r *Recorder) Stop() error {
	return nil
}

// Frames returns the accumulated PCM samples after recording.
func (r *Recorder) Frames() []int16 {
	return r.frames
}

// Encode encodes the accumulated frames to a valid WAV []byte entirely in memory.
// No files are created. Returns error if frames is empty or encoding fails.
func (r *Recorder) Encode() ([]byte, error) {
	if len(r.frames) == 0 {
		return nil, fmt.Errorf("no audio frames to encode")
	}
	return encodeWAV(r.frames)
}

// selectInputDevice finds a named input device from portaudio.Devices().
// Returns error if the device is not found or has no input channels.
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
