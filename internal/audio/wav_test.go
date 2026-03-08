package audio_test

import (
	"testing"
)

// TestWAVHeader verifies the RIFF/fmt/data chunk layout produced by encodeWAV().
// Checks: bytes 0-3 == "RIFF", bytes 8-11 == "WAVE", fmt chunk present, sample rate 16000.
func TestWAVHeader(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestReadWriteSeeker verifies the custom ReadWriteSeeker helper:
// Write appends bytes, Seek moves position, re-Write overwrites (not appends),
// Bytes() returns full accumulated buffer.
func TestReadWriteSeeker(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}

// TestInt16Conversion verifies that audio.IntBuffer.Data []int correctly stores
// int16 sample values without sign loss: int16(-32768) → int(-32768).
func TestInt16Conversion(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 02")
}
