package linux

import (
	"bytes"
	"io"
	"log"

	"github.com/go-audio/wav"
	"github.com/gordonklaus/portaudio"
	"github.com/hybridz/yap/internal/platform"
)

const chimeFrameSize = 512

// chimePlayer implements platform.ChimePlayer using PortAudio.
type chimePlayer struct{}

// NewChimePlayer returns a ChimePlayer backed by PortAudio.
func NewChimePlayer() platform.ChimePlayer {
	return &chimePlayer{}
}

// Play decodes an embedded WAV reader and plays it on the default output device.
// Returns immediately — playback runs in a detached goroutine.
// Safe to call concurrently with recording; owns an independent PortAudio session.
func (c *chimePlayer) Play(r io.Reader) {
	if r == nil {
		return
	}
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return
	}

	go func() {
		// Recover from panics in the PortAudio C library (e.g. ALSA returning
		// invalid device indices on headless systems).
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("chime: recovered panic during playback: %v", rec)
			}
		}()

		dec := wav.NewDecoder(bytes.NewReader(data))
		if !dec.IsValidFile() {
			return
		}
		pcm, err := dec.FullPCMBuffer()
		if err != nil || pcm == nil {
			return
		}

		// Convert audio.IntBuffer.Data ([]int) to []int16 for PortAudio output.
		samples := make([]int16, len(pcm.Data))
		for i, s := range pcm.Data {
			samples[i] = int16(s)
		}

		if err := portaudio.Initialize(); err != nil {
			return
		}
		defer portaudio.Terminate() //nolint:errcheck

		out := make([]int16, chimeFrameSize)
		stream, err := portaudio.OpenDefaultStream(0, numChannels, float64(sampleRate), len(out), &out)
		if err != nil {
			return
		}
		defer stream.Close() //nolint:errcheck

		if err := stream.Start(); err != nil {
			return
		}
		defer stream.Stop() //nolint:errcheck

		for i := 0; i < len(samples); i += chimeFrameSize {
			end := i + chimeFrameSize
			if end > len(samples) {
				end = len(samples)
				copy(out, samples[i:end])
				for j := end - i; j < chimeFrameSize; j++ {
					out[j] = 0
				}
			} else {
				copy(out, samples[i:end])
			}
			if err := stream.Write(); err != nil {
				return
			}
		}
	}()
}
