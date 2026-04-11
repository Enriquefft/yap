package silence_test

import (
	"math"
	"testing"

	"github.com/hybridz/yap/pkg/yap/silence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// silenceBatch generates N samples of zero-amplitude PCM (pure silence).
func silenceBatch(n int) []int16 {
	return make([]int16, n)
}

// loudBatch generates N samples of full-scale PCM (max amplitude).
func loudBatch(n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = 32767
	}
	return out
}

// quietBatch generates N samples at a specific amplitude level.
func quietBatch(n int, amp int16) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = amp
	}
	return out
}

func TestDetector_LoudInputNeverTriggers(t *testing.T) {
	var silenceFired, warningFired bool
	d := silence.New(0.02, 1.0, 0.5,
		func() { warningFired = true },
		func() { silenceFired = true },
	)

	// Feed 5 seconds of loud audio — 5 × 16000 samples.
	for i := 0; i < 5; i++ {
		d.Process(loudBatch(16000))
	}
	assert.False(t, warningFired, "warning should not fire on loud input")
	assert.False(t, silenceFired, "silence should not fire on loud input")
}

func TestDetector_PureSilenceTriggersWarningThenSilence(t *testing.T) {
	var order []string
	d := silence.New(0.02, 2.0, 1.0,
		func() { order = append(order, "warning") },
		func() { order = append(order, "silence") },
	)

	// Feed 2 seconds of silence — 2 × 16000 samples.
	// Warning should fire at silenceDuration - warningBefore = 1.0s.
	// Silence should fire at 2.0s.
	for i := 0; i < 2; i++ {
		d.Process(silenceBatch(16000))
	}

	require.Len(t, order, 2, "both callbacks should fire")
	assert.Equal(t, "warning", order[0], "warning must fire before silence")
	assert.Equal(t, "silence", order[1], "silence must fire after warning")
}

func TestDetector_ResetClearsState(t *testing.T) {
	var count int
	d := silence.New(0.02, 1.0, 0.5,
		func() { count++ },
		func() { count++ },
	)

	// Trigger both callbacks.
	d.Process(silenceBatch(16000))
	assert.Equal(t, 2, count)

	// Reset and trigger again — should fire again.
	d.Reset()
	d.Process(silenceBatch(16000))
	assert.Equal(t, 4, count, "callbacks should fire again after Reset")
}

func TestDetector_SingleLoudFrameResetsCounter(t *testing.T) {
	var silenceFired bool
	d := silence.New(0.02, 1.0, 0.5,
		nil,
		func() { silenceFired = true },
	)

	// 0.9s of silence — not enough to trigger.
	d.Process(silenceBatch(int(0.9 * 16000)))
	assert.False(t, silenceFired)

	// One loud frame resets the counter.
	d.Process(loudBatch(160))
	assert.False(t, silenceFired)

	// Another 0.9s of silence — still not enough since counter was reset.
	d.Process(silenceBatch(int(0.9 * 16000)))
	assert.False(t, silenceFired)

	// Feed the remaining silence to push past 1.0s cumulative after the loud frame.
	d.Process(silenceBatch(int(0.2 * 16000)))
	assert.True(t, silenceFired, "silence should fire after sustained silence post-loud frame")
}

func TestDetector_CallbacksFireAtMostOnce(t *testing.T) {
	var warningCount, silenceCount int
	d := silence.New(0.02, 1.0, 0.5,
		func() { warningCount++ },
		func() { silenceCount++ },
	)

	// Feed 5 seconds of silence — way past the thresholds.
	for i := 0; i < 5; i++ {
		d.Process(silenceBatch(16000))
	}
	assert.Equal(t, 1, warningCount, "warning should fire exactly once")
	assert.Equal(t, 1, silenceCount, "silence should fire exactly once")
}

func TestDetector_ThresholdBoundary(t *testing.T) {
	// threshold = 0.5 means the boundary amplitude is 0.5 * 32768 = 16384.
	// An RMS of exactly 16384 is NOT silent (>= threshold).
	// An RMS of 16383 IS silent (< threshold).
	tests := []struct {
		name    string
		amp     int16
		silent  bool
	}{
		{"at threshold", 16384, false},
		{"above threshold", 20000, false},
		{"below threshold", 16383, true},
		{"zero", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var silenceFired bool
			d := silence.New(0.5, 0.1, 0.0,
				nil,
				func() { silenceFired = true },
			)
			// Feed 0.2s of constant-amplitude audio.
			n := int(0.2 * 16000)
			d.Process(quietBatch(n, tt.amp))
			assert.Equal(t, tt.silent, silenceFired, "silence trigger mismatch for amp=%d", tt.amp)
		})
	}
}

func TestDetector_ShortSilenceDuration_ClampsWarning(t *testing.T) {
	// silenceDuration = 0.5s, warningBefore = 2.0s (larger than duration).
	// warningBefore should be clamped to 0.5 * 0.5 = 0.25s internally.
	var order []string
	d := silence.New(0.02, 0.5, 2.0,
		func() { order = append(order, "warning") },
		func() { order = append(order, "silence") },
	)

	// Feed 0.5s of silence.
	d.Process(silenceBatch(int(0.5 * 16000)))

	require.Len(t, order, 2)
	assert.Equal(t, "warning", order[0])
	assert.Equal(t, "silence", order[1])
}

func TestDetector_ZeroWarningBefore(t *testing.T) {
	// warningBefore = 0 means no warning callback — only silence fires.
	var warningFired, silenceFired bool
	d := silence.New(0.02, 1.0, 0.0,
		func() { warningFired = true },
		func() { silenceFired = true },
	)

	d.Process(silenceBatch(16000))
	assert.False(t, warningFired, "warning should not fire when warningBefore = 0")
	assert.True(t, silenceFired, "silence should still fire")
}

func TestDetector_EmptyBatchIsNoop(t *testing.T) {
	var silenceFired bool
	d := silence.New(0.02, 0.001, 0.0,
		nil,
		func() { silenceFired = true },
	)

	// Feed empty batch — should be a no-op.
	d.Process([]int16{})
	assert.False(t, silenceFired)

	// Now feed enough silence to trigger.
	d.Process(silenceBatch(16000))
	assert.True(t, silenceFired)
}

func TestRMSAmplitude_KnownValues(t *testing.T) {
	assert.InDelta(t, 32767.0, silence.RmsAmplitude(loudBatch(100)), 1.0)
	assert.InDelta(t, 0.0, silence.RmsAmplitude(silenceBatch(100)), 0.001)

	n := 16000
	sine := make([]int16, n)
	for i := range sine {
		sine[i] = int16(32767 * math.Sin(2*math.Pi*float64(i)/float64(n)))
	}
	expectedRMS := 32767.0 / math.Sqrt(2)
	assert.InDelta(t, expectedRMS, silence.RmsAmplitude(sine), 100.0)
}


func TestDetector_NilCallbacksAreSafe(t *testing.T) {
	d := silence.New(0.02, 0.001, 0.0, nil, nil)
	// Should not panic.
	d.Process(silenceBatch(16000))
}
