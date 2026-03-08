package audio_test

import (
	"testing"
)

// TestDeviceSelection verifies AUDIO-01: OpenDefaultStream uses config.MicDevice when set,
// or system default when empty.
func TestDeviceSelection(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestPipeWireGuard verifies AUDIO-02: returns clear error when 0 input devices found
// after portaudio.Initialize().
func TestPipeWireGuard(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestNoTempFiles verifies AUDIO-03 + NFR-06: no files created in /tmp or
// $XDG_RUNTIME_DIR during a mock recording session.
func TestNoTempFiles(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestRecorderFrames verifies AUDIO-04: PCM data accumulates in []int16 slice,
// never via a Go channel inside the PortAudio callback.
func TestRecorderFrames(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestEncodeWAV verifies AUDIO-05: WAV bytes begin with "RIFF", are at least 44 bytes,
// and use 16kHz/16-bit/mono parameters.
func TestEncodeWAV(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestInMemoryEncode verifies AUDIO-06: encoding produces []byte with no file I/O;
// no os.File or os.CreateTemp used in the encode path.
func TestInMemoryEncode(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}
