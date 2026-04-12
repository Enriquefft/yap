package audioprep

import "fmt"

// Options configures the audio preprocessing pipeline. Both features
// are independently toggleable; disabling both causes [New] to return
// nil so the engine can skip the preprocessing stage entirely.
type Options struct {
	// HighPassFilter enables the 2nd-order Butterworth high-pass biquad
	// filter that removes sub-speech rumble.
	HighPassFilter bool

	// HighPassCutoff is the filter cutoff frequency in Hz. Speech
	// fundamentals start at ~85Hz (deep male voice); the default 80Hz
	// cutoff removes everything below that range. Must be in [20, 500].
	HighPassCutoff int

	// TrimSilence enables leading/trailing silence removal via
	// windowed RMS amplitude detection.
	TrimSilence bool

	// TrimThreshold is the normalized RMS amplitude threshold for
	// silence detection [0, 1]. A frame window with RMS/32768 below
	// this value is considered silent. Default 0.01.
	TrimThreshold float64

	// TrimMarginMS is the number of milliseconds of silence to
	// preserve on each side of detected speech. Default 200.
	TrimMarginMS int
}

// Processor applies audio preprocessing to WAV data. It satisfies the
// engine.AudioProcessor interface implicitly (ProcessWAV method). The
// engine defines the interface; this package does not import engine.
type Processor struct {
	opts Options
}

// New creates a Processor with the given options. Returns nil when both
// HighPassFilter and TrimSilence are disabled, allowing the caller to
// pass nil to the engine (which treats nil as "skip preprocessing").
func New(opts Options) *Processor {
	if !opts.HighPassFilter && !opts.TrimSilence {
		return nil
	}
	return &Processor{opts: opts}
}

// ProcessWAV applies the configured preprocessing steps to WAV audio
// bytes. The input must be a valid RIFF WAV with PCM data — the format
// yap's recorder always produces (16kHz mono 16-bit). Returns processed
// WAV bytes preserving the same format.
//
// Processing order: high-pass filter first (modifies samples in-place),
// then silence trimming (returns a subslice). This order matters:
// filtering before trimming ensures the trimmer's RMS calculation is
// not skewed by sub-speech rumble that would artificially inflate the
// amplitude of "silent" windows.
func (p *Processor) ProcessWAV(wav []byte) ([]byte, error) {
	header, samples, err := parseWAV(wav)
	if err != nil {
		return nil, fmt.Errorf("parse WAV: %w", err)
	}

	if p.opts.HighPassFilter && len(samples) > 0 {
		applyHighPass(samples, header.SampleRate, p.opts.HighPassCutoff)
	}

	if p.opts.TrimSilence && len(samples) > 0 {
		samples = trimSilence(samples, header.SampleRate,
			p.opts.TrimThreshold, p.opts.TrimMarginMS)
	}

	out, err := buildWAV(header, samples)
	if err != nil {
		return nil, fmt.Errorf("build WAV: %w", err)
	}
	return out, nil
}
