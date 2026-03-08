package audio

import (
	"fmt"
	"io"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

const (
	sampleRate     = 16000
	bitDepth       = 16
	numChannels    = 1
	audioFormatPCM = 1
)

// ReadWriteSeeker is an in-memory io.WriteSeeker backed by a growable []byte slice.
// Required by wav.NewEncoder — bytes.Buffer does NOT implement io.Seeker.
type ReadWriteSeeker struct {
	buf []byte
	pos int
}

func (r *ReadWriteSeeker) Write(p []byte) (int, error) {
	minCap := r.pos + len(p)
	if minCap > len(r.buf) {
		r.buf = append(r.buf, make([]byte, minCap-len(r.buf))...)
	}
	copy(r.buf[r.pos:], p)
	r.pos += len(p)
	return len(p), nil
}

func (r *ReadWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(r.pos) + offset
	case io.SeekEnd:
		abs = int64(len(r.buf)) + offset
	default:
		return 0, fmt.Errorf("ReadWriteSeeker: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("ReadWriteSeeker: negative position %d", abs)
	}
	r.pos = int(abs)
	return abs, nil
}

// Bytes returns the accumulated buffer contents.
func (r *ReadWriteSeeker) Bytes() []byte {
	return r.buf
}

// encodeWAV encodes a []int16 PCM accumulator to a valid WAV []byte entirely in memory.
// Output: valid RIFF/fmt/data headers at 16kHz mono 16-bit PCM (Whisper-compatible).
// No temp files are created.
func encodeWAV(frames []int16) ([]byte, error) {
	ws := &ReadWriteSeeker{}
	enc := wav.NewEncoder(ws, sampleRate, bitDepth, numChannels, audioFormatPCM)

	// audio.IntBuffer.Data is []int (not []int16) — explicit cast required.
	// int16(-32768) must become int(-32768), not a positive overflow.
	data := make([]int, len(frames))
	for i, s := range frames {
		data[i] = int(s)
	}

	buf := &audio.IntBuffer{
		Data: data,
		Format: &audio.Format{
			NumChannels: numChannels,
			SampleRate:  sampleRate,
		},
	}
	if err := enc.Write(buf); err != nil {
		return nil, fmt.Errorf("wav encode write: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("wav encode close: %w", err)
	}
	return ws.Bytes(), nil
}
