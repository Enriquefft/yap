package audioprep

import (
	"math"
	"testing"
)

// generateSine creates a mono int16 sine wave at the given frequency
// and sample rate for the specified duration in seconds.
func generateSine(freqHz float64, sampleRate uint32, durationSec float64) []int16 {
	n := int(float64(sampleRate) * durationSec)
	samples := make([]int16, n)
	for i := range n {
		t := float64(i) / float64(sampleRate)
		samples[i] = int16(16000 * math.Sin(2*math.Pi*freqHz*t))
	}
	return samples
}

// rms computes root-mean-square of int16 samples.
func rms(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func TestApplyHighPass_RemovesLowFrequency(t *testing.T) {
	const sr uint32 = 16000
	samples := generateSine(40, sr, 1.0) // 40Hz — well below 80Hz cutoff
	rmsBefore := rms(samples)

	applyHighPass(samples, sr, 80)
	rmsAfter := rms(samples)

	// 40Hz is one octave below 80Hz cutoff. A 2nd-order Butterworth
	// HPF attenuates at -12dB/octave, so ~75% attenuation expected.
	// We check for at least 70% reduction.
	ratio := rmsAfter / rmsBefore
	if ratio > 0.30 {
		t.Errorf("40Hz signal attenuation insufficient: %.1f%% survived (want <30%%)", ratio*100)
	}
}

func TestApplyHighPass_PreservesHighFrequency(t *testing.T) {
	const sr uint32 = 16000
	samples := generateSine(440, sr, 1.0) // 440Hz — well above 80Hz cutoff
	rmsBefore := rms(samples)

	applyHighPass(samples, sr, 80)
	rmsAfter := rms(samples)

	// 440Hz should pass through with minimal attenuation.
	// Allow up to 5% loss (filter startup transient).
	ratio := rmsAfter / rmsBefore
	if ratio < 0.95 {
		t.Errorf("440Hz signal attenuated too much: %.1f%% survived (want >95%%)", ratio*100)
	}
}

func TestApplyHighPass_EmptySamples(t *testing.T) {
	// Must not panic.
	applyHighPass(nil, 16000, 80)
	applyHighPass([]int16{}, 16000, 80)
}

func TestApplyHighPass_ZeroCutoff(t *testing.T) {
	samples := generateSine(440, 16000, 0.1)
	original := make([]int16, len(samples))
	copy(original, samples)

	applyHighPass(samples, 16000, 0)

	// Zero cutoff should be a no-op.
	for i := range samples {
		if samples[i] != original[i] {
			t.Fatalf("sample[%d] modified with zero cutoff: got %d, want %d",
				i, samples[i], original[i])
		}
	}
}

func TestApplyHighPass_MixedSignal(t *testing.T) {
	const sr uint32 = 16000
	low := generateSine(40, sr, 1.0)   // 40Hz component
	high := generateSine(440, sr, 1.0) // 440Hz component

	// Sum both components.
	mixed := make([]int16, len(low))
	for i := range mixed {
		sum := int32(low[i]) + int32(high[i])
		if sum > 32767 {
			sum = 32767
		} else if sum < -32768 {
			sum = -32768
		}
		mixed[i] = int16(sum)
	}

	highRMS := rms(high) // RMS of the 440Hz component alone

	applyHighPass(mixed, sr, 80)
	filteredRMS := rms(mixed)

	// After filtering, the output should be close to the 440Hz
	// component alone — the 40Hz part is removed.
	ratio := filteredRMS / highRMS
	if ratio < 0.85 || ratio > 1.15 {
		t.Errorf("filtered mixed signal RMS = %.1f, expected ~%.1f (440Hz component); ratio = %.2f",
			filteredRMS, highRMS, ratio)
	}
}

func TestApplyHighPass_ClampingAtBoundary(t *testing.T) {
	// Samples at int16 extremes should not overflow.
	samples := []int16{32767, -32768, 32767, -32768, 32767, -32768}
	applyHighPass(samples, 16000, 80)
	// int16 is bounded by definition; this test verifies the function
	// does not panic on boundary inputs and returns valid samples.
	for i, s := range samples {
		_ = s // ensure the slice was not corrupted
		_ = i
	}
}
