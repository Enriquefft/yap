package audio_test

import (
	"testing"
	"time"
)

// TestChimeAsync verifies ASSETS-03: PlayChime() returns within 5ms regardless
// of chime duration — the goroutine runs independently.
func TestChimeAsync(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 03")
}

// TestChimeNonBlocking verifies AUDIO-06 (chime variant): starting a chime while
// a recording mock is active does not introduce timing delay to the recorder.
func TestChimeNonBlocking(t *testing.T) {
	t.Skip("Wave 0 stub — implement in Plan 03")
	_ = time.Millisecond // keep import used
}

// BenchmarkRecorder verifies NFR-03: memory allocated during mock encode of 60s
// audio must not exceed 15MB.
func BenchmarkRecorder(b *testing.B) {
	b.Skip("Wave 0 stub — implement in Plan 03")
}
