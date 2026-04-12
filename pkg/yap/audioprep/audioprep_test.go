package audioprep

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew_BothDisabled_ReturnsNil(t *testing.T) {
	p := New(Options{})
	if p != nil {
		t.Error("New with both features disabled should return nil")
	}
}

func TestNew_HighPassOnly(t *testing.T) {
	p := New(Options{HighPassFilter: true, HighPassCutoff: 80})
	if p == nil {
		t.Error("New with high-pass enabled should return non-nil")
	}
}

func TestNew_TrimOnly(t *testing.T) {
	p := New(Options{TrimSilence: true, TrimThreshold: 0.01, TrimMarginMS: 200})
	if p == nil {
		t.Error("New with trim enabled should return non-nil")
	}
}

func TestProcessWAV_InvalidInput(t *testing.T) {
	p := New(Options{HighPassFilter: true, HighPassCutoff: 80})
	_, err := p.ProcessWAV([]byte("not a wav"))
	if err == nil {
		t.Error("ProcessWAV should error on invalid input")
	}
}

func TestProcessWAV_Integration(t *testing.T) {
	const sr uint32 = 16000

	// Build: 0.5s silence + 0.5s of 440Hz tone + 0.5s silence.
	silence := make([]int16, sr/2)  // 8000 samples
	tone := make([]int16, sr/2)     // 8000 samples
	for i := range tone {
		tSec := float64(i) / float64(sr)
		tone[i] = int16(16000 * math.Sin(2*math.Pi*440*tSec))
	}

	all := make([]int16, 0, len(silence)*2+len(tone))
	all = append(all, silence...)
	all = append(all, tone...)
	all = append(all, silence...)

	inputWAV := makeTestWAV(all, sr)

	p := New(Options{
		HighPassFilter: true,
		HighPassCutoff: 80,
		TrimSilence:    true,
		TrimThreshold:  0.01,
		TrimMarginMS:   200,
	})

	output, err := p.ProcessWAV(inputWAV)
	if err != nil {
		t.Fatalf("ProcessWAV: %v", err)
	}

	// Output should be shorter than input (silence trimmed).
	if len(output) >= len(inputWAV) {
		t.Errorf("output length %d >= input length %d (silence should be trimmed)",
			len(output), len(inputWAV))
	}

	// Parse the output and verify it contains audio.
	_, outSamples, err := parseWAV(output)
	if err != nil {
		t.Fatalf("parseWAV output: %v", err)
	}
	if len(outSamples) == 0 {
		t.Error("output has no samples")
	}

	// The 440Hz tone should still be present and audible.
	outRMS := rmsAmplitude(outSamples)
	if outRMS < 1000 {
		t.Errorf("output RMS = %.1f, expected significant signal (440Hz tone)", outRMS)
	}
}

func TestProcessWAV_HighPassOnly(t *testing.T) {
	const sr uint32 = 16000
	// Pure 40Hz tone — should be heavily attenuated.
	tone := generateSine(40, sr, 0.5)
	inputWAV := makeTestWAV(tone, sr)

	p := New(Options{HighPassFilter: true, HighPassCutoff: 80})
	output, err := p.ProcessWAV(inputWAV)
	if err != nil {
		t.Fatalf("ProcessWAV: %v", err)
	}

	_, outSamples, err := parseWAV(output)
	if err != nil {
		t.Fatalf("parseWAV output: %v", err)
	}

	inRMS := rmsAmplitude(tone)
	outRMS := rmsAmplitude(outSamples)
	ratio := outRMS / inRMS
	if ratio > 0.30 {
		t.Errorf("40Hz signal should be attenuated: %.1f%% survived", ratio*100)
	}
}

func TestProcessWAV_TrimOnly(t *testing.T) {
	const sr uint32 = 16000
	// 1s silence + 0.5s tone + 1s silence.
	silence := make([]int16, sr)
	tone := loudSamples(int(sr / 2))

	all := make([]int16, 0, len(silence)*2+len(tone))
	all = append(all, silence...)
	all = append(all, tone...)
	all = append(all, silence...)

	inputWAV := makeTestWAV(all, sr)

	p := New(Options{TrimSilence: true, TrimThreshold: 0.01, TrimMarginMS: 200})
	output, err := p.ProcessWAV(inputWAV)
	if err != nil {
		t.Fatalf("ProcessWAV: %v", err)
	}

	_, outSamples, err := parseWAV(output)
	if err != nil {
		t.Fatalf("parseWAV output: %v", err)
	}

	// Output should be significantly shorter.
	if len(outSamples) >= len(all) {
		t.Errorf("output samples %d >= input %d", len(outSamples), len(all))
	}
}

// TestProcessWAV_Manual processes real WAV files and writes .processed.wav
// alongside each input for A/B listening. Skipped unless AUDIOPREP_MANUAL=1.
//
// Usage:
//
//	AUDIOPREP_MANUAL=1 go test ./pkg/yap/audioprep/ -run TestProcessWAV_Manual -v
func TestProcessWAV_Manual(t *testing.T) {
	if os.Getenv("AUDIOPREP_MANUAL") != "1" {
		t.Skip("set AUDIOPREP_MANUAL=1 to run")
	}

	glob := os.Getenv("AUDIOPREP_GLOB")
	if glob == "" {
		t.Fatal("set AUDIOPREP_GLOB to a WAV glob pattern, e.g. AUDIOPREP_GLOB=$PWD/pkg/yap/hint/testdata/*.wav")
	}
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no WAV files matched %q", glob)
	}

	proc := New(Options{
		HighPassFilter: true,
		HighPassCutoff: 80,
		TrimSilence:    true,
		TrimThreshold:  0.01,
		TrimMarginMS:   200,
	})

	for _, f := range files {
		if strings.Contains(f, ".processed.") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		out, err := proc.ProcessWAV(raw)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		outPath := strings.TrimSuffix(f, filepath.Ext(f)) + ".processed.wav"
		if err := os.WriteFile(outPath, out, 0o644); err != nil {
			t.Errorf("%s: %v", outPath, err)
			continue
		}
		t.Logf("%s → %s (%d→%d bytes, %.0f%%)",
			filepath.Base(f), filepath.Base(outPath),
			len(raw), len(out), float64(len(out))/float64(len(raw))*100)
	}
}
