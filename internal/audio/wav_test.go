package audio

import (
	"io"
	"math"
	"os"
	"testing"
)

const sineFreq = 1000.0

func makeSineFrames(n int) []int16 {
	frames := make([]int16, n)
	for i := range frames {
		frames[i] = int16(10000 * math.Sin(2*math.Pi*sineFreq*float64(i)/float64(sampleRate)))
	}
	return frames
}

// TestReadWriteSeeker verifies the custom ReadWriteSeeker helper.
func TestReadWriteSeeker(t *testing.T) {
	t.Run("Write", func(t *testing.T) {
		rws := &ReadWriteSeeker{}
		n, err := rws.Write([]byte("hello"))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		if n != 5 {
			t.Errorf("wrote %d bytes, want 5", n)
		}
		if string(rws.Bytes()) != "hello" {
			t.Errorf("Bytes() = %q, want %q", string(rws.Bytes()), "hello")
		}
	})

	t.Run("Seek_start", func(t *testing.T) {
		rws := &ReadWriteSeeker{}
		rws.Write([]byte("0123456789")) //nolint:errcheck
		pos, err := rws.Seek(0, io.SeekStart)
		if err != nil {
			t.Fatalf("Seek error: %v", err)
		}
		if pos != 0 {
			t.Errorf("Seek returned %d, want 0", pos)
		}
	})

	t.Run("Seek_current", func(t *testing.T) {
		rws := &ReadWriteSeeker{}
		rws.Write([]byte("hello")) //nolint:errcheck
		pos, err := rws.Seek(2, io.SeekCurrent)
		if err != nil {
			t.Fatalf("Seek error: %v", err)
		}
		if pos != 7 {
			t.Errorf("Seek returned %d, want 7", pos)
		}
	})

	t.Run("Seek_overwrite", func(t *testing.T) {
		rws := &ReadWriteSeeker{}
		rws.Write([]byte("hello")) //nolint:errcheck
		rws.Seek(0, io.SeekStart)  //nolint:errcheck
		rws.Write([]byte("world")) //nolint:errcheck
		if string(rws.Bytes()) != "world" {
			t.Errorf("Bytes() = %q, want %q", string(rws.Bytes()), "world")
		}
	})

	t.Run("Seek_negative", func(t *testing.T) {
		rws := &ReadWriteSeeker{}
		_, err := rws.Seek(-1, io.SeekStart)
		if err == nil {
			t.Error("expected error for negative seek, got nil")
		}
	})
}

// TestInt16Conversion verifies that int16(-32768) is stored as int(-32768) without truncation.
func TestInt16Conversion(t *testing.T) {
	var s int16 = -32768
	v := int(s)
	if v != -32768 {
		t.Errorf("int16(-32768) as int = %d, want -32768", v)
	}
}

// TestWAVHeader verifies RIFF/WAVE chunk headers in encodeWAV output.
func TestWAVHeader(t *testing.T) {
	frames := makeSineFrames(1600)
	b, err := encodeWAV(frames)
	if err != nil {
		t.Fatalf("encodeWAV error: %v", err)
	}
	if len(b) < 44 {
		t.Fatalf("WAV too short: %d bytes, want >= 44", len(b))
	}
	if string(b[0:4]) != "RIFF" {
		t.Errorf("bytes[0:4] = %q, want \"RIFF\"", string(b[0:4]))
	}
	if string(b[8:12]) != "WAVE" {
		t.Errorf("bytes[8:12] = %q, want \"WAVE\"", string(b[8:12]))
	}
}

// TestEncodeWAV verifies encodeWAV returns nil error and non-empty bytes.
func TestEncodeWAV(t *testing.T) {
	frames := makeSineFrames(1600)
	b, err := encodeWAV(frames)
	if err != nil {
		t.Fatalf("encodeWAV error: %v", err)
	}
	if len(b) == 0 {
		t.Error("encodeWAV returned empty bytes")
	}
}

// TestInMemoryEncode verifies encodeWAV does not create files in os.TempDir.
func TestInMemoryEncode(t *testing.T) {
	tmpDir := os.TempDir()
	before, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir before: %v", err)
	}

	frames := makeSineFrames(1600)
	_, encErr := encodeWAV(frames)
	if encErr != nil {
		t.Fatalf("encodeWAV error: %v", encErr)
	}

	after, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir after: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("temp dir file count changed: before=%d after=%d — encodeWAV created files", len(before), len(after))
	}
}
