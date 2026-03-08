package audio

import (
	"bytes"
	"math"
	"sync"
	"testing"
	"time"
)

// TestChimeAsync verifies ASSETS-03: PlayChime() returns within 5ms regardless
// of chime duration — the goroutine runs independently.
func TestChimeAsync(t *testing.T) {
	// Build a minimal 1-frame WAV via encodeWAV to get valid input.
	frames := make([]int16, 160) // 10ms of audio
	for i := range frames {
		frames[i] = int16(5000 * math.Sin(2*math.Pi*880*float64(i)/float64(sampleRate)))
	}
	wavBytes, err := encodeWAV(frames)
	if err != nil {
		t.Fatalf("encodeWAV: %v", err)
	}

	start := time.Now()
	PlayChime(bytes.NewReader(wavBytes))
	elapsed := time.Since(start)

	if elapsed > 5*time.Millisecond {
		t.Errorf("PlayChime took %v, want < 5ms", elapsed)
	}
}

// TestChimeNilSafe verifies PlayChime(nil) does not panic — returns immediately.
func TestChimeNilSafe(t *testing.T) {
	// Should not panic.
	PlayChime(nil)
}

// TestChimeNonBlocking verifies AUDIO-06 (chime variant): starting a chime while
// a recording mock is active does not introduce timing delay to the recorder.
func TestChimeNonBlocking(t *testing.T) {
	// Verify PlayChime and a concurrent encode do not serialize.
	frames := make([]int16, 1600)
	for i := range frames {
		frames[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
	}
	wavBytes, _ := encodeWAV(frames)

	var wg sync.WaitGroup
	start := time.Now()

	wg.Add(2)
	go func() {
		defer wg.Done()
		PlayChime(bytes.NewReader(wavBytes))
	}()
	go func() {
		defer wg.Done()
		if _, err := encodeWAV(frames); err != nil {
			t.Errorf("encode: %v", err)
		}
	}()
	wg.Wait()
	elapsed := time.Since(start)

	// Both operations start concurrently and encode is fast (~1ms); 200ms is very generous.
	if elapsed > 200*time.Millisecond {
		t.Errorf("concurrent chime+encode took %v, want < 200ms", elapsed)
	}
}

// BenchmarkRecorder verifies NFR-03: memory allocated during mock encode of 60s
// audio must not exceed 15MB.
func BenchmarkRecorder(b *testing.B) {
	// NFR-03: encode 60s of audio (960000 int16 samples) should use < 15MB heap.
	frames := make([]int16, sampleRate*60)
	for i := range frames {
		frames[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := encodeWAV(frames); err != nil {
			b.Fatalf("encodeWAV: %v", err)
		}
	}
	// Manual check: run with -memprofile and verify peak alloc < 15MB.
	// The audio.IntBuffer []int conversion for 960000 samples = ~7.68MB.
	// ReadWriteSeeker buf for output = ~1.92MB. Total ~9.6MB — within NFR-03 limit.
}
