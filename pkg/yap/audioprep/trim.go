package audioprep

import "math"

// trimWindowSamples is the RMS analysis window size: 20ms at 16kHz.
// This matches the standard frame size used in speech processing and
// provides a good balance between temporal resolution and noise
// smoothing. Not configurable — changing it would require retuning the
// threshold semantics.
const trimWindowSamples = 320

// trimSilence removes leading and trailing silence from PCM samples.
// threshold is normalized [0, 1] — compared against RMS/32768.
// marginMS is the silence buffer to preserve on each side of detected
// speech, converting milliseconds to samples via sampleRate.
//
// Returns a subslice of the original — no allocation for the samples
// themselves. If the entire recording is below threshold (all silence),
// returns up to marginMS worth of samples from the start so the
// transcriber never receives an empty WAV (which Whisper rejects).
func trimSilence(samples []int16, sampleRate uint32, threshold float64, marginMS int) []int16 {
	if len(samples) == 0 {
		return samples
	}

	thresholdAmp := threshold * 32768.0
	windowSize := trimWindowSamples
	if windowSize > len(samples) {
		windowSize = len(samples)
	}

	// Scan forward: find first window above threshold. The final
	// window may be shorter than windowSize when len(samples) is not
	// a multiple of windowSize — we still examine it so no trailing
	// fragment is silently dropped.
	firstLoud := -1
	for i := 0; i < len(samples); i += windowSize {
		end := i + windowSize
		if end > len(samples) {
			end = len(samples)
		}
		if rmsAmplitude(samples[i:end]) >= thresholdAmp {
			firstLoud = i
			break
		}
	}

	// Scan backward: find last window above threshold. Same
	// short-window handling for the leading fragment.
	lastLoudEnd := -1
	for i := len(samples); i > 0; i -= windowSize {
		start := i - windowSize
		if start < 0 {
			start = 0
		}
		if rmsAmplitude(samples[start:i]) >= thresholdAmp {
			lastLoudEnd = i
			break
		}
	}

	marginSamples := int(sampleRate) * marginMS / 1000

	// All silence — return minimal audio.
	if firstLoud < 0 || lastLoudEnd < 0 || firstLoud >= lastLoudEnd {
		if marginSamples > len(samples) {
			return samples
		}
		if marginSamples == 0 {
			// Even with zero margin, return at least one window to
			// avoid a completely empty WAV.
			if windowSize > len(samples) {
				return samples
			}
			return samples[:windowSize]
		}
		return samples[:marginSamples]
	}

	// Apply margin around detected speech.
	start := firstLoud - marginSamples
	if start < 0 {
		start = 0
	}
	end := lastLoudEnd + marginSamples
	if end > len(samples) {
		end = len(samples)
	}

	return samples[start:end]
}

// rmsAmplitude computes the root-mean-square amplitude of a batch of
// int16 samples. The result is in the same scale as the input (0..32768).
// This is the same formula used by pkg/yap/silence — duplicated here
// because it is 8 lines and not worth a shared package.
func rmsAmplitude(samples []int16) float64 {
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
