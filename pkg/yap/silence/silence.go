// Package silence provides an amplitude-threshold voice activity detector
// (VAD) for PCM audio streams. It is designed to run on the audio capture
// thread: Process does zero allocations and returns in O(N) where N is the
// batch size. Silence duration is tracked via sample counts at a fixed
// sample rate so no time.Now calls are needed in the audio callback.
//
// A Detector fires two callbacks:
//
//   - onWarning after (silenceDuration - warningBefore) seconds of sustained
//     silence, giving the user an audible cue that auto-stop is imminent.
//   - onSilence after silenceDuration seconds of sustained silence, cancelling
//     the recording.
//
// Both callbacks fire at most once per recording session. Call Reset between
// sessions to clear internal state.
package silence

import (
	"math"
	"sync"
)

const sampleRate = 16000

// Detector monitors a stream of int16 PCM frames for sustained silence.
// Create one with New, feed frames via Process, and call Reset between
// recording sessions.
//
// The zero value is not usable — always construct via New.
type Detector struct {
	threshold       float64 // amplitude threshold (0..1), compared against RMS/32768
	silenceDuration float64 // seconds of silence before onSilence fires
	warningAt       float64 // seconds of silence before onWarning fires

	onWarning func()
	onSilence func()

	mu           sync.Mutex
	silentSamples int    // consecutive silent samples accumulated so far
	warningFired bool   // onWarning has fired this session
	triggered    bool   // onSilence has fired this session
}

// New creates a Detector that fires onWarning after (silenceDuration -
// warningBefore) seconds of sustained silence and onSilence after
// silenceDuration seconds. Both callbacks are optional (nil is safe).
//
// threshold is a normalised amplitude in [0, 1]. A frame is "silent" when
// its RMS amplitude divided by 32768.0 falls below threshold.
//
// warningBefore is clamped to silenceDuration*0.5 when silenceDuration is
// short, ensuring the warning always fires before the silence trigger.
// When warningBefore is 0, the warning callback is never fired.
func New(threshold, silenceDuration, warningBefore float64, onWarning, onSilence func()) *Detector {
	if warningBefore > silenceDuration*0.9 {
		warningBefore = silenceDuration * 0.5
	}
	if warningBefore < 0 {
		warningBefore = 0
	}
	// warningAt is the elapsed-silence threshold for firing the warning.
	// When warningBefore is 0 (user doesn't want a warning), warningAt
	// equals silenceDuration and the warning would fire simultaneously
	// with silence — set it to 0 to disable the warning entirely.
	var warningAt float64
	if warningBefore > 0 {
		warningAt = silenceDuration - warningBefore
	}
	return &Detector{
		threshold:       threshold,
		silenceDuration: silenceDuration,
		warningAt:       warningAt,
		onWarning:       onWarning,
		onSilence:       onSilence,
	}
}

// Process analyses one batch of int16 PCM samples. It must be called for
// every frame batch the audio capture delivers. The method is safe to call
// from the audio worker thread: it does zero allocations and the only lock
// is a short mutex hold around the optional callback invocations.
func (d *Detector) Process(samples []int16) {
	if len(samples) == 0 {
		return
	}

	rms := rmsAmplitude(samples)
	thresholdAmp := d.threshold * 32768.0

	if rms >= thresholdAmp {
		// Loud frame — reset silence tracking.
		d.mu.Lock()
		d.silentSamples = 0
		d.warningFired = false
		d.mu.Unlock()
		return
	}

	// Silent frame — accumulate.
	d.mu.Lock()
	d.silentSamples += len(samples)
	elapsed := float64(d.silentSamples) / float64(sampleRate)

	if !d.warningFired && d.warningAt > 0 && elapsed >= d.warningAt {
		d.warningFired = true
		if d.onWarning != nil {
			d.onWarning()
		}
	}

	if !d.triggered && elapsed >= d.silenceDuration {
		d.triggered = true
		if d.onSilence != nil {
			d.onSilence()
		}
	}
	d.mu.Unlock()
}

// Reset clears all internal tracking so the detector can be reused for a new
// recording session. Both callbacks are retained.
func (d *Detector) Reset() {
	d.mu.Lock()
	d.silentSamples = 0
	d.warningFired = false
	d.triggered = false
	d.mu.Unlock()
}

// rmsAmplitude computes the root-mean-square amplitude of a batch of int16
// samples. The result is in the same scale as the input (0..32768).
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
