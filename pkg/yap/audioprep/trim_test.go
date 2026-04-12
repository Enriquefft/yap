package audioprep

import (
	"math"
	"testing"
)

const testSampleRate uint32 = 16000

// silentSamples returns n zero-valued samples.
func silentSamples(n int) []int16 {
	return make([]int16, n)
}

// loudSamples returns n samples of a 440Hz sine at half amplitude.
func loudSamples(n int) []int16 {
	s := make([]int16, n)
	for i := range s {
		t := float64(i) / float64(testSampleRate)
		s[i] = int16(16000 * math.Sin(2*math.Pi*440*t))
	}
	return s
}

func TestTrimSilence_TrimsLeadingAndTrailing(t *testing.T) {
	silence := silentSamples(16000)    // 1s silence
	speech := loudSamples(8000)        // 0.5s speech
	trailing := silentSamples(16000)   // 1s silence

	all := make([]int16, 0, len(silence)+len(speech)+len(trailing))
	all = append(all, silence...)
	all = append(all, speech...)
	all = append(all, trailing...)

	result := trimSilence(all, testSampleRate, 0.01, 200)

	// Expected: ~speech + 2*margin = 8000 + 2*3200 = 14400
	// Allow some tolerance for window alignment.
	expectedMin := 8000
	expectedMax := 8000 + 2*3200 + trimWindowSamples
	if len(result) < expectedMin || len(result) > expectedMax {
		t.Errorf("trimmed length = %d, expected in [%d, %d]",
			len(result), expectedMin, expectedMax)
	}

	// Should be shorter than the original.
	if len(result) >= len(all) {
		t.Errorf("trimmed length %d >= original length %d", len(result), len(all))
	}
}

func TestTrimSilence_PreservesMargin(t *testing.T) {
	// Speech at the very start with trailing silence.
	speech := loudSamples(8000)
	silence := silentSamples(16000)

	all := make([]int16, 0, len(speech)+len(silence))
	all = append(all, speech...)
	all = append(all, silence...)

	marginMS := 200
	marginSamples := int(testSampleRate) * marginMS / 1000 // 3200

	result := trimSilence(all, testSampleRate, 0.01, marginMS)

	// Should include speech + margin after speech.
	expectedMin := 8000
	expectedMax := 8000 + marginSamples + trimWindowSamples
	if len(result) < expectedMin || len(result) > expectedMax {
		t.Errorf("trimmed length = %d, expected in [%d, %d]",
			len(result), expectedMin, expectedMax)
	}
}

func TestTrimSilence_AllSilence(t *testing.T) {
	silence := silentSamples(16000) // 1s silence

	result := trimSilence(silence, testSampleRate, 0.01, 200)

	// Should return margin's worth of samples (200ms = 3200).
	marginSamples := int(testSampleRate) * 200 / 1000
	if len(result) != marginSamples {
		t.Errorf("all-silence: trimmed length = %d, expected %d (margin)",
			len(result), marginSamples)
	}
	if len(result) == 0 {
		t.Error("all-silence: must not return empty slice")
	}
}

func TestTrimSilence_AllSilence_ZeroMargin(t *testing.T) {
	silence := silentSamples(16000)

	result := trimSilence(silence, testSampleRate, 0.01, 0)

	// With zero margin, should return at least one window.
	if len(result) == 0 {
		t.Error("all-silence with zero margin: must not return empty slice")
	}
	if len(result) > trimWindowSamples {
		t.Errorf("all-silence with zero margin: got %d samples, expected <= %d",
			len(result), trimWindowSamples)
	}
}

func TestTrimSilence_AllLoud(t *testing.T) {
	speech := loudSamples(16000) // 1s of speech

	result := trimSilence(speech, testSampleRate, 0.01, 200)

	// All-loud: trimmer should include everything.
	if len(result) != len(speech) {
		t.Errorf("all-loud: trimmed length = %d, expected %d (unchanged)",
			len(result), len(speech))
	}
}

func TestTrimSilence_EmptySamples(t *testing.T) {
	result := trimSilence(nil, testSampleRate, 0.01, 200)
	if len(result) != 0 {
		t.Errorf("empty input: got %d samples, want 0", len(result))
	}

	result = trimSilence([]int16{}, testSampleRate, 0.01, 200)
	if len(result) != 0 {
		t.Errorf("empty slice: got %d samples, want 0", len(result))
	}
}

func TestTrimSilence_ThresholdZero(t *testing.T) {
	// Threshold 0 means everything is "loud" — no trimming.
	all := silentSamples(16000)
	result := trimSilence(all, testSampleRate, 0, 200)

	// RMS of zero samples is 0.0, which is not >= 0.0*32768 = 0.0
	// (the comparison is >=, so 0 >= 0 is true — everything is "loud").
	if len(result) != len(all) {
		t.Errorf("threshold=0: trimmed length = %d, expected %d (unchanged)",
			len(result), len(all))
	}
}

func TestTrimSilence_NonAlignedSampleCount(t *testing.T) {
	// Regression: when len(samples) is not a multiple of windowSize (320),
	// the scanner must still examine the trailing/leading fragment.
	// Here: 500 samples of silence + 180 samples of loud = 680 total.
	// The loud burst falls entirely within samples[500:680], which is
	// a fragment shorter than one window.
	silence := silentSamples(500)
	loud := loudSamples(180)

	all := make([]int16, 0, len(silence)+len(loud))
	all = append(all, silence...)
	all = append(all, loud...)

	result := trimSilence(all, testSampleRate, 0.01, 0)

	// The loud portion must be included in the result.
	if len(result) < 180 {
		t.Errorf("non-aligned trim: got %d samples, expected at least 180 (the loud fragment)", len(result))
	}
}

func TestRmsAmplitude_KnownValues(t *testing.T) {
	// DC signal of 100 → RMS = 100.
	samples := make([]int16, 100)
	for i := range samples {
		samples[i] = 100
	}
	got := rmsAmplitude(samples)
	if math.Abs(got-100.0) > 0.01 {
		t.Errorf("RMS of DC=100: got %f, want 100.0", got)
	}
}

func TestRmsAmplitude_Empty(t *testing.T) {
	if rmsAmplitude(nil) != 0 {
		t.Error("RMS of nil should be 0")
	}
	if rmsAmplitude([]int16{}) != 0 {
		t.Error("RMS of empty should be 0")
	}
}
