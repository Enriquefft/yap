package audioprep

import (
	"encoding/binary"
	"testing"
)

// makeTestWAV builds a minimal valid WAV file from the given samples
// at the specified sample rate (16-bit mono PCM).
func makeTestWAV(samples []int16, sampleRate uint32) []byte {
	h := wavHeader{SampleRate: sampleRate, NumChannels: 1, BitsPerSample: 16}
	data, err := buildWAV(h, samples)
	if err != nil {
		panic(err)
	}
	return data
}

func TestParseWAV_ValidInput(t *testing.T) {
	want := []int16{100, -200, 300, -400, 500}
	wav := makeTestWAV(want, 16000)

	h, got, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if h.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", h.SampleRate)
	}
	if h.NumChannels != 1 {
		t.Errorf("NumChannels = %d, want 1", h.NumChannels)
	}
	if h.BitsPerSample != 16 {
		t.Errorf("BitsPerSample = %d, want 16", h.BitsPerSample)
	}
	if len(got) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseWAV_TooShort(t *testing.T) {
	_, _, err := parseWAV([]byte("RIFF"))
	if err == nil {
		t.Fatal("expected error for truncated input")
	}
}

func TestParseWAV_BadRIFF(t *testing.T) {
	wav := makeTestWAV([]int16{0}, 16000)
	copy(wav[0:4], "XXXX")
	_, _, err := parseWAV(wav)
	if err == nil {
		t.Fatal("expected error for bad RIFF magic")
	}
}

func TestParseWAV_BadWAVE(t *testing.T) {
	wav := makeTestWAV([]int16{0}, 16000)
	copy(wav[8:12], "XXXX")
	_, _, err := parseWAV(wav)
	if err == nil {
		t.Fatal("expected error for bad WAVE identifier")
	}
}

func TestParseWAV_NotPCM(t *testing.T) {
	wav := makeTestWAV([]int16{0}, 16000)
	// AudioFormat is at offset 20 (2 bytes).
	binary.LittleEndian.PutUint16(wav[20:22], 3) // IEEE float
	_, _, err := parseWAV(wav)
	if err == nil {
		t.Fatal("expected error for non-PCM format")
	}
}

func TestBuildWAV_RoundTrip(t *testing.T) {
	original := make([]int16, 1000)
	for i := range original {
		original[i] = int16(i*7 - 3500) // spread across int16 range
	}

	h := wavHeader{SampleRate: 48000, NumChannels: 1, BitsPerSample: 16}
	wav, err := buildWAV(h, original)
	if err != nil {
		t.Fatalf("buildWAV: %v", err)
	}

	h2, got, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if h2.SampleRate != h.SampleRate {
		t.Errorf("SampleRate = %d, want %d", h2.SampleRate, h.SampleRate)
	}
	if len(got) != len(original) {
		t.Fatalf("len(samples) = %d, want %d", len(got), len(original))
	}
	for i := range original {
		if got[i] != original[i] {
			t.Fatalf("sample[%d] = %d, want %d", i, got[i], original[i])
		}
	}
}

func TestParseWAV_ExtraChunks(t *testing.T) {
	// Build a WAV with a JUNK chunk between fmt and data.
	// Use odd payload size (11 bytes) to exercise word-alignment padding.
	want := []int16{1000, 2000, 3000}
	clean := makeTestWAV(want, 16000)

	// Split at the data chunk boundary (offset 36).
	fmtPart := clean[:36]          // RIFF header + fmt chunk
	dataPart := clean[36:]         // "data" chunk
	junk := make([]byte, 8+11+1)  // 11-byte JUNK payload + 1 padding byte
	copy(junk[0:4], "JUNK")
	binary.LittleEndian.PutUint32(junk[4:8], 11) // odd size triggers padding

	// Reassemble: RIFF header + fmt + JUNK + data.
	assembled := make([]byte, 0, len(fmtPart)+len(junk)+len(dataPart))
	assembled = append(assembled, fmtPart...)
	assembled = append(assembled, junk...)
	assembled = append(assembled, dataPart...)

	// Fix the RIFF chunk size (total file size - 8).
	binary.LittleEndian.PutUint32(assembled[4:8], uint32(len(assembled)-8))

	h, got, err := parseWAV(assembled)
	if err != nil {
		t.Fatalf("parseWAV with JUNK chunk: %v", err)
	}
	if h.SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", h.SampleRate)
	}
	if len(got) != len(want) {
		t.Fatalf("len(samples) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sample[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestParseWAV_StereoRejected(t *testing.T) {
	wav := makeTestWAV([]int16{0}, 16000)
	// NumChannels at offset 22.
	binary.LittleEndian.PutUint16(wav[22:24], 2)
	_, _, err := parseWAV(wav)
	if err == nil {
		t.Fatal("expected error for stereo input")
	}
}

func TestParseWAV_24BitRejected(t *testing.T) {
	wav := makeTestWAV([]int16{0}, 16000)
	// BitsPerSample at offset 34.
	binary.LittleEndian.PutUint16(wav[34:36], 24)
	_, _, err := parseWAV(wav)
	if err == nil {
		t.Fatal("expected error for 24-bit input")
	}
}

func TestParseWAV_EmptySamples(t *testing.T) {
	wav := makeTestWAV(nil, 16000)
	_, got, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len(samples) = %d, want 0", len(got))
	}
}
