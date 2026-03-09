package linux

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

// readWriteSeeker is an in-memory io.ReadWriteSeeker backed by a growable []byte.
// Required by wav.NewEncoder — bytes.Buffer does NOT implement io.Seeker.
type readWriteSeeker struct {
	buf []byte
	pos int
}

func (r *readWriteSeeker) Write(p []byte) (int, error) {
	minCap := r.pos + len(p)
	if minCap > len(r.buf) {
		r.buf = append(r.buf, make([]byte, minCap-len(r.buf))...)
	}
	copy(r.buf[r.pos:], p)
	r.pos += len(p)
	return len(p), nil
}

func (r *readWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(r.pos) + offset
	case io.SeekEnd:
		abs = int64(len(r.buf)) + offset
	default:
		return 0, fmt.Errorf("readWriteSeeker: invalid whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("readWriteSeeker: negative position %d", abs)
	}
	r.pos = int(abs)
	return abs, nil
}

func (r *readWriteSeeker) bytes() []byte {
	return r.buf
}

// encodeWAV encodes a []int16 PCM slice to a valid WAV []byte entirely in memory.
// Output format: RIFF/fmt/data at 16kHz mono 16-bit PCM (Whisper-compatible).
func encodeWAV(frames []int16) ([]byte, error) {
	ws := &readWriteSeeker{}
	enc := wav.NewEncoder(ws, sampleRate, bitDepth, numChannels, audioFormatPCM)

	// audio.IntBuffer.Data is []int (not []int16) — explicit conversion required.
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
	return ws.bytes(), nil
}
