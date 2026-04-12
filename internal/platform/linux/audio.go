package linux

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
	"github.com/Enriquefft/yap/internal/platform"
)

// preferredBackends is the Linux audio backend preference list used by
// every malgo context yap creates (recorder, device lister, chime).
// The order is PulseAudio → ALSA → JACK because:
//
//   - PulseAudio (or PipeWire's pulse shim) is the standard user-space
//     sound server on modern Linux desktops and is what users expect
//     yap to record from.
//   - ALSA is the reliable fallback when PulseAudio is unavailable
//     (headless servers, stripped-down systems).
//   - JACK is the last resort because it exposes a generic "Default
//     Capture Device" that rarely matches what the user wanted and
//     tends to be what miniaudio falls through to on NixOS when the
//     pulse/alsa runtime libraries are missing — exactly the failure
//     mode this order is designed to surface loudly.
//
// Keeping the order here as a single source of truth means `yap
// devices`, `yap record`, and the chime player all agree on which
// backend they are talking to.
var preferredBackends = []malgo.Backend{
	malgo.BackendPulseaudio,
	malgo.BackendAlsa,
	malgo.BackendJack,
}

// backendDisplayName maps a malgo.Backend constant to the human-readable
// name used by `yap devices`. Mirrors the gBackendInfo[] table inside
// miniaudio.h (see ma_get_backend_name). The Go binding does not
// re-export that table, so we keep a local copy restricted to the
// Linux-relevant backends.
func backendDisplayName(b malgo.Backend) string {
	switch b {
	case malgo.BackendPulseaudio:
		return "PulseAudio"
	case malgo.BackendAlsa:
		return "ALSA"
	case malgo.BackendJack:
		return "JACK"
	case malgo.BackendNull:
		return "Null"
	default:
		return fmt.Sprintf("unknown (%d)", uint32(b))
	}
}

// initLinuxAudioContext walks preferredBackends in order, calling
// malgo.InitContext with a single-backend list for each candidate. The
// first backend that initializes successfully wins; its malgo context
// is returned together with the selected backend. Callers own the
// returned context and must free it with freeMalgoContext.
//
// This is the single source of truth for malgo context creation on
// Linux. Using one helper for the recorder, the device lister, and the
// chime player guarantees all three share the same backend, so the
// output of `yap devices` describes exactly what `yap record` will use.
//
// The returned error aggregates every backend attempt so a total
// failure still tells the user which backends were tried and how each
// one failed — critical on NixOS-style setups where a missing runtime
// library silently downgrades every candidate.
func initLinuxAudioContext() (*malgo.AllocatedContext, malgo.Backend, error) {
	var attemptErrs []error
	for _, backend := range preferredBackends {
		ctx, err := malgo.InitContext([]malgo.Backend{backend}, malgo.ContextConfig{}, nil)
		if err != nil {
			attemptErrs = append(attemptErrs, fmt.Errorf("%s: %w", backendDisplayName(backend), err))
			continue
		}
		return ctx, backend, nil
	}
	return nil, 0, fmt.Errorf("no usable audio backend: %w", errors.Join(attemptErrs...))
}

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
	backend    malgo.Backend
	deviceName string

	mu     sync.Mutex
	frames []int16

	// onFrame is an optional per-frame callback set via SetOnFrame
	// before Start and cleared after Start returns. The data callback
	// invokes it on the malgo worker thread — callers must not block.
	// No mutex is needed because the set/clear happens outside Start's
	// blocking window.
	onFrame func([]int16)

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
	ctx, backend, err := initLinuxAudioContext()
	if err != nil {
		return nil, fmt.Errorf("malgo init context: %w", err)
	}

	devs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		freeMalgoContext(ctx)
		return nil, fmt.Errorf("enumerate audio devices on %s: %w", backendDisplayName(backend), err)
	}
	if len(devs) == 0 {
		freeMalgoContext(ctx)
		return nil, fmt.Errorf(
			"no audio input devices available on %s backend — "+
				"on PipeWire systems enable pipewire-alsa "+
				"(NixOS: services.pipewire.alsa.enable = true)",
			backendDisplayName(backend),
		)
	}

	r := &recorder{
		ctx:        ctx,
		backend:    backend,
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

// SetOnFrame sets a per-frame callback called from the malgo data callback.
// It must be called before Start and cleared (nil) after Start returns.
// The callback fires on the audio worker thread — callers must not block.
func (r *recorder) SetOnFrame(fn func([]int16)) {
	r.onFrame = fn
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

	// When a named device is configured we have to embed a Go pointer
	// (to r.captureDeviceID) inside deviceConfig, which itself is a Go
	// value. Handing a Go pointer that itself contains an unpinned Go
	// pointer to cgo trips runtime.cgoCheckPointer's "Go pointer to
	// unpinned Go pointer" panic. runtime.Pinner exists exactly for
	// this case: it pins the DeviceID in the Go heap so cgo accepts it
	// as a stable address.
	//
	// The pin only needs to survive malgo.InitDevice. ma_device_init in
	// miniaudio.h (see the MA_COPY_MEMORY(&pDevice->capture.id, ...)
	// call) copies the device id into pDevice->capture.id during init,
	// and the PulseAudio/ALSA/JACK onContextInit callbacks only
	// dereference the incoming descriptor pointer in-place (they copy
	// the id by value into backend-local state). None of the Linux
	// backends retain the config-provided pointer past the InitDevice
	// return, so Unpin can happen as soon as InitDevice finishes —
	// well before the device callback starts firing on the audio
	// worker thread.
	var pinner runtime.Pinner
	if r.captureDeviceID != nil {
		pinner.Pin(r.captureDeviceID)
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
		if fn := r.onFrame; fn != nil {
			fn(samples)
		}
	}

	device, err := malgo.InitDevice(r.ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onRecvFrames,
	})
	pinner.Unpin()
	if err != nil {
		if r.deviceName != "" {
			return fmt.Errorf("open audio device %q on %s: %w", r.deviceName, backendDisplayName(r.backend), err)
		}
		return fmt.Errorf("open audio device on %s: %w", backendDisplayName(r.backend), err)
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return fmt.Errorf("start audio device on %s: %w", backendDisplayName(r.backend), err)
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
