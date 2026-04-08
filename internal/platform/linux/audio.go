package linux

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
	"github.com/hybridz/yap/internal/platform"
)

// recorder implements platform.Recorder using miniaudio (malgo).
//
// Per-recorder context lifecycle: each recorder owns a malgo
// AllocatedContext, created in NewRecorder and freed in Close. The
// context is never re-created across Start/Encode cycles, matching the
// portaudio Init/Terminate scope it replaces. The data callback runs
// on a malgo worker thread; PCM samples are decoded from S16 little
// endian byte buffers and accumulated in r.frames under r.mu.
type recorder struct {
	ctx        *malgo.AllocatedContext
	deviceName string

	mu     sync.Mutex
	frames []int16

	// captureDeviceID holds the malgo device id for the configured
	// input when deviceName is non-empty. Storing it on the recorder
	// keeps the backing memory alive across malgo.InitDevice, where the
	// device config struct points to it via unsafe.Pointer.
	captureDeviceID *malgo.DeviceID
}

// NewRecorder creates a platform.Recorder and validates that at least one
// audio input device is available. Returns a clear error on PipeWire-only
// systems with zero input devices.
func NewRecorder(deviceName string) (platform.Recorder, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("malgo init context: %w", err)
	}

	devs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		freeMalgoContext(ctx)
		return nil, fmt.Errorf("enumerate audio devices: %w", err)
	}
	if len(devs) == 0 {
		freeMalgoContext(ctx)
		return nil, fmt.Errorf(
			"no audio input devices available — " +
				"on PipeWire systems enable pipewire-alsa " +
				"(NixOS: services.pipewire.alsa.enable = true)",
		)
	}

	r := &recorder{
		ctx:        ctx,
		deviceName: deviceName,
	}

	if deviceName != "" {
		id, err := lookupCaptureDeviceID(ctx, devs, deviceName)
		if err != nil {
			freeMalgoContext(ctx)
			return nil, err
		}
		r.captureDeviceID = id
	}

	return r, nil
}

// Close releases the malgo context owned by this recorder.
func (r *recorder) Close() {
	if r.ctx == nil {
		return
	}
	freeMalgoContext(r.ctx)
	r.ctx = nil
}

// Start begins audio capture. Blocks until ctx is cancelled.
//
// Captured samples flow through malgo's data callback (driven by a
// worker thread) into r.frames. The capture loop is event-driven: this
// goroutine sleeps on ctx.Done while malgo delivers frames in the
// background. On cancellation the device is uninitialized cleanly.
func (r *recorder) Start(ctx context.Context) error {
	if r.ctx == nil {
		return fmt.Errorf("recorder closed")
	}

	r.mu.Lock()
	// Pre-allocate for up to 60s of audio to avoid realloc during capture.
	r.frames = make([]int16, 0, sampleRate*60)
	r.mu.Unlock()

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = numChannels
	deviceConfig.SampleRate = sampleRate
	// Disable ALSA mmap to favour the more compatible read/write path.
	// PipeWire's ALSA shim does not implement mmap reliably for capture.
	deviceConfig.Alsa.NoMMap = 1
	if r.captureDeviceID != nil {
		// Pass the address of the stored DeviceID to malgo. The
		// recorder owns the backing memory until Close, so the pointer
		// stays valid for the entire device lifetime. malgo's
		// DeviceID.Pointer helper allocates fresh C memory and never
		// frees it, so we deliberately bypass it here.
		deviceConfig.Capture.DeviceID = unsafe.Pointer(r.captureDeviceID)
	}

	onRecvFrames := func(_ /*pOutput*/, pInput []byte, framecount uint32) {
		if len(pInput) == 0 || framecount == 0 {
			return
		}
		samples := decodeS16LE(pInput)
		r.mu.Lock()
		r.frames = append(r.frames, samples...)
		r.mu.Unlock()
	}

	device, err := malgo.InitDevice(r.ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onRecvFrames,
	})
	if err != nil {
		if r.deviceName != "" {
			return fmt.Errorf("open audio device %q: %w", r.deviceName, err)
		}
		return fmt.Errorf("open audio device: %w", err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return fmt.Errorf("start audio device: %w", err)
	}

	<-ctx.Done()
	return nil
}

// Encode encodes accumulated frames to WAV bytes. Returns error if no audio was captured.
func (r *recorder) Encode() ([]byte, error) {
	r.mu.Lock()
	frames := r.frames
	r.mu.Unlock()
	if len(frames) == 0 {
		return nil, fmt.Errorf("no audio frames to encode")
	}
	return encodeWAV(frames)
}

// lookupCaptureDeviceID finds a named input device in the malgo capture
// device list. Returns a heap-allocated copy of the device id so its
// backing memory is owned by the recorder for the life of the device.
func lookupCaptureDeviceID(ctx *malgo.AllocatedContext, devs []malgo.DeviceInfo, name string) (*malgo.DeviceID, error) {
	for i := range devs {
		if devs[i].Name() != name {
			continue
		}
		// DeviceInfo from Context.Devices may not include the full
		// native format list; query DeviceInfo with the id to confirm
		// the device is selectable. Failures here surface as a clear
		// "device not selectable" error rather than an opaque malgo
		// status from InitDevice.
		if _, err := ctx.DeviceInfo(malgo.Capture, devs[i].ID, malgo.Shared); err != nil {
			return nil, fmt.Errorf("audio device %q not selectable: %w", name, err)
		}
		id := devs[i].ID
		return &id, nil
	}
	return nil, fmt.Errorf("audio device %q not found among capture devices", name)
}

// freeMalgoContext uninitializes and frees a malgo context. Errors from
// Uninit are intentionally ignored — there is no recovery path on
// teardown and panicking would mask the original failure.
func freeMalgoContext(ctx *malgo.AllocatedContext) {
	if ctx == nil {
		return
	}
	_ = ctx.Uninit()
	ctx.Free()
}

// decodeS16LE converts a byte slice of little-endian signed 16-bit PCM
// samples to a fresh []int16. The returned slice has its own backing
// array — the caller is free to retain it after the malgo data
// callback returns and reclaims the input buffer.
func decodeS16LE(buf []byte) []int16 {
	count := len(buf) / 2
	out := make([]int16, count)
	for i := 0; i < count; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(buf[i*2:]))
	}
	return out
}
