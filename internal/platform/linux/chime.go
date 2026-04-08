package linux

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"sync"

	"github.com/gen2brain/malgo"
	"github.com/go-audio/wav"
	"github.com/hybridz/yap/internal/platform"
)

// chimePlayer implements platform.ChimePlayer using miniaudio (malgo).
//
// Stateless by design: each Play call decodes the WAV in a goroutine,
// initializes a short-lived malgo context for the playback, and tears
// it down when the buffer is fully drained. Per-Play context init is
// cheap (a single ALSA/PulseAudio handshake) and matches the
// fire-and-forget chime UX, while keeping the chime player free of any
// process-lifetime resources that have no teardown hook.
type chimePlayer struct{}

// NewChimePlayer returns a ChimePlayer backed by miniaudio.
func NewChimePlayer() platform.ChimePlayer {
	return &chimePlayer{}
}

// Play decodes an embedded WAV reader and plays it on the default
// output device. Returns immediately — playback runs in a detached
// goroutine. Safe to call concurrently with recording; each Play owns
// an independent malgo context and device handle.
func (c *chimePlayer) Play(r io.Reader) {
	if r == nil {
		return
	}
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return
	}

	go func() {
		// Recover from panics in the malgo C library (e.g. ALSA
		// returning invalid device indices on headless systems).
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("chime: recovered panic during playback: %v", rec)
			}
		}()

		samples, channels, rate, ok := decodeChimeWAV(data)
		if !ok || len(samples) == 0 {
			return
		}
		playPCM(samples, channels, rate)
	}()
}

// decodeChimeWAV decodes an in-memory WAV byte slice to a flat
// []int16, returning the channel count and sample rate from the file
// header. Returns ok=false if the data is not a valid PCM WAV.
func decodeChimeWAV(data []byte) (samples []int16, channels uint32, sampleRateOut uint32, ok bool) {
	dec := wav.NewDecoder(bytes.NewReader(data))
	if !dec.IsValidFile() {
		return nil, 0, 0, false
	}
	pcm, err := dec.FullPCMBuffer()
	if err != nil || pcm == nil || pcm.Format == nil {
		return nil, 0, 0, false
	}
	if pcm.Format.NumChannels < 1 || pcm.Format.SampleRate < 1 {
		return nil, 0, 0, false
	}
	out := make([]int16, len(pcm.Data))
	for i, s := range pcm.Data {
		out[i] = int16(s)
	}
	return out, uint32(pcm.Format.NumChannels), uint32(pcm.Format.SampleRate), true
}

// playPCM streams int16 PCM samples to the default output device using
// a short-lived malgo session. Blocks until the buffer is drained.
func playPCM(samples []int16, channels, rate uint32) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return
	}
	defer freeMalgoContext(ctx)

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = channels
	deviceConfig.SampleRate = rate
	deviceConfig.Alsa.NoMMap = 1

	var (
		mu       sync.Mutex
		offset   int
		done     = make(chan struct{})
		doneOnce sync.Once
		bytesPer = int(channels) * 2 // S16 = 2 bytes per sample
	)
	signalDone := func() {
		doneOnce.Do(func() { close(done) })
	}

	onSendFrames := func(pOutput, _ /*pInput*/ []byte, framecount uint32) {
		if pOutput == nil || framecount == 0 {
			return
		}
		want := int(framecount) * bytesPer
		mu.Lock()
		remaining := len(samples)*2 - offset
		if remaining <= 0 {
			mu.Unlock()
			// Zero-fill so the device sees clean silence while it
			// drains the final period, then signal completion.
			for i := range pOutput {
				pOutput[i] = 0
			}
			signalDone()
			return
		}
		copyBytes := want
		if copyBytes > remaining {
			copyBytes = remaining
		}
		// Encode samples in-place into the output buffer; this avoids
		// allocating a temporary byte slice on the audio worker thread.
		startSample := offset / 2
		endSample := startSample + copyBytes/2
		for i, s := range samples[startSample:endSample] {
			binary.LittleEndian.PutUint16(pOutput[i*2:], uint16(s))
		}
		// Pad any remaining bytes in the requested period with silence.
		for i := copyBytes; i < want && i < len(pOutput); i++ {
			pOutput[i] = 0
		}
		offset += copyBytes
		mu.Unlock()
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{
		Data: onSendFrames,
	})
	if err != nil {
		return
	}
	defer device.Uninit()

	if err := device.Start(); err != nil {
		return
	}

	<-done
}
