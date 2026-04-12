package audioprep

import "math"

// applyHighPass applies a 2nd-order Butterworth high-pass biquad filter
// in-place to the given int16 PCM samples. The filter removes
// frequencies below cutoffHz while preserving everything above it with
// a maximally flat passband. At the default 80Hz cutoff, speech
// fundamentals (~85Hz for a deep male voice) pass unattenuated.
//
// The filter uses the Direct Form II Transposed structure with
// coefficients derived from the Audio EQ Cookbook (Robert
// Bristow-Johnson). The Butterworth Q factor (1/√2 ≈ 0.7071) gives
// the flattest possible passband for a 2nd-order section.
//
// Zero allocations per sample. Four float64 state variables total.
// At 16kHz sample rate with 960,000 samples (60s), this completes in
// well under 10ms on any modern CPU.
func applyHighPass(samples []int16, sampleRate uint32, cutoffHz int) {
	if len(samples) == 0 || cutoffHz <= 0 || sampleRate == 0 {
		return
	}
	// Cutoff must be below Nyquist (sampleRate/2). At or above
	// Nyquist the biquad coefficients become degenerate — silently
	// return rather than producing garbage output.
	if float64(cutoffHz) >= float64(sampleRate)/2 {
		return
	}

	fs := float64(sampleRate)
	fc := float64(cutoffHz)
	w0 := 2.0 * math.Pi * fc / fs
	cosW0 := math.Cos(w0)
	sinW0 := math.Sin(w0)
	alpha := sinW0 / math.Sqrt2 // Q = 1/√2 for Butterworth

	// Unnormalized transfer function coefficients.
	b0 := (1.0 + cosW0) / 2.0
	b1 := -(1.0 + cosW0)
	b2 := (1.0 + cosW0) / 2.0
	a0 := 1.0 + alpha
	a1 := -2.0 * cosW0
	a2 := 1.0 - alpha

	// Normalize by a0.
	b0 /= a0
	b1 /= a0
	b2 /= a0
	a1 /= a0
	a2 /= a0

	// Direct Form II Transposed.
	var z1, z2 float64
	for i, s := range samples {
		x := float64(s)
		y := b0*x + z1
		z1 = b1*x - a1*y + z2
		z2 = b2*x - a2*y

		// Clamp to int16 range, round to nearest (not truncate).
		// Rounding halves worst-case quantization error from ±1 to
		// ±0.5 LSB — the correct DSP quantization operation.
		switch {
		case y > 32767:
			y = 32767
		case y < -32768:
			y = -32768
		}
		samples[i] = int16(math.Round(y))
	}
}
