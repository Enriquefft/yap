package audio

import (
	"bytes"
	"io"
	"log"

	"github.com/go-audio/wav"
	"github.com/gordonklaus/portaudio"
)

const chimeFrameSize = 512

// PlayChime decodes an embedded WAV reader and plays it on the default output device.
// Returns immediately — playback runs in a detached goroutine (ASSETS-03).
// Safe to call concurrently with recording; owns an independent portaudio session.
//
// The r argument is typically assets.StartChime() or assets.StopChime().
// Since assets functions return io.Reader (not io.ReadSeeker), we io.ReadAll first
// to get a seekable bytes.Reader for wav.NewDecoder.
func PlayChime(r io.Reader) {
	if r == nil {
		return
	}
	// Read all bytes up front — assets.StartChime() returns io.Reader (not io.ReadSeeker).
	// bytes.NewReader implements io.ReadSeeker which wav.NewDecoder requires.
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		log.Printf("chime: read error: %v", err)
		return
	}

	go func() {
		// Recover from panics in the portaudio C library (e.g. ALSA returning
		// invalid device indices on headless/no-audio systems — portaudio.hostsAndDevices
		// can panic with index out of range instead of returning an error).
		defer func() {
			if r := recover(); r != nil {
				log.Printf("chime: recovered panic during playback: %v", r)
			}
		}()

		dec := wav.NewDecoder(bytes.NewReader(data))
		if !dec.IsValidFile() {
			log.Printf("chime: invalid WAV data")
			return
		}
		pcm, err := dec.FullPCMBuffer()
		if err != nil || pcm == nil {
			log.Printf("chime: decode error: %v", err)
			return
		}

		// Convert audio.IntBuffer.Data ([]int) back to []int16 for PortAudio output stream.
		samples := make([]int16, len(pcm.Data))
		for i, s := range pcm.Data {
			samples[i] = int16(s)
		}

		// Own an independent PortAudio session — separate from any recording session.
		// portaudio internally ref-counts Initialize/Terminate calls.
		if err := portaudio.Initialize(); err != nil {
			log.Printf("chime: portaudio init: %v", err)
			return
		}
		defer portaudio.Terminate() //nolint:errcheck

		out := make([]int16, chimeFrameSize)
		stream, err := portaudio.OpenDefaultStream(0, numChannels, float64(sampleRate), len(out), &out)
		if err != nil {
			log.Printf("chime: open stream: %v", err)
			return
		}
		defer stream.Close() //nolint:errcheck

		if err := stream.Start(); err != nil {
			log.Printf("chime: start stream: %v", err)
			return
		}
		defer stream.Stop() //nolint:errcheck

		for i := 0; i < len(samples); i += chimeFrameSize {
			end := i + chimeFrameSize
			if end > len(samples) {
				end = len(samples)
				copy(out, samples[i:end])
				// Zero-pad the remainder of the output buffer to avoid stale data.
				for j := end - i; j < chimeFrameSize; j++ {
					out[j] = 0
				}
			} else {
				copy(out, samples[i:end])
			}
			if err := stream.Write(); err != nil {
				log.Printf("chime: write: %v", err)
				return
			}
		}
	}()
}
