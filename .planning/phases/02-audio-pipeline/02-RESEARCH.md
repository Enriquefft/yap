# Phase 2: Audio Pipeline - Research

**Researched:** 2026-03-07
**Domain:** PortAudio Go bindings, in-memory WAV encoding, ring buffer pattern, async chime playback
**Confidence:** HIGH

## Summary

Phase 2 builds the audio capture pipeline entirely in isolation from the daemon. The central design is:
`portaudio.Initialize()` → device enumeration (PipeWire guard) → blocking stream loop reading into a fixed `[]int16` buffer → accumulate frames into a growing `[]int16` slice → encode entire slice to WAV in-memory via `go-audio/wav` → return `[]byte` to caller. Chimes play on a separate blocking output stream in a goroutine.

The most important technical constraint discovered in research is that `bytes.Buffer` does NOT implement `io.WriteSeeker`, which `wav.NewEncoder` requires. The WAV encoder seeks back to patch RIFF header sizes after writing the body. A small seekable-buffer helper (either a custom `ReadWriteSeeker` struct or `github.com/orcaman/writerseeker`) is required. A ~50-line custom implementation avoids an external dependency.

The second critical finding is that the blocking stream pattern (the gist/suapapa approach: `OpenDefaultStream` + `stream.Read()` loop + `append` accumulator) is directly applicable to yap. Callback-based streams are NOT needed — the blocking stream is simpler, avoids CGo callback complexity, and produces no goroutine-inside-callback issues.

**Primary recommendation:** Use blocking stream pattern with `OpenDefaultStream(1, 0, 16000, frameSize, &in)`, accumulate `[]int16` frames via `append`, encode to WAV in-memory with a custom `ReadWriteSeeker`, wrap the audio package behind a thin `AudioRecorder` interface for test isolation.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/gordonklaus/portaudio` | v0.0.0-20260203164431 (already in go.mod) | Audio capture via PortAudio C library | Only mature Go audio capture option; sole CGo boundary; already in go.mod |
| `github.com/go-audio/wav` | v1.1.0 | WAV encoding with valid RIFF/fmt/data headers | Pure Go; Whisper-compatible 16kHz 16-bit mono; prevents raw PCM pitfall |
| `github.com/go-audio/audio` | (transitive dep of go-audio/wav) | `audio.IntBuffer` type used by wav encoder | Required by wav.Encoder.Write() |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Custom `ReadWriteSeeker` | stdlib only | In-memory `io.WriteSeeker` for wav.NewEncoder | Always — `bytes.Buffer` does not implement Seek; avoid external dep |
| stdlib `bytes`, `encoding/binary` | stdlib | Buffer construction and PCM manipulation | Always |
| stdlib `sync` | stdlib | Mutex for ring buffer if needed | If goroutine-based drain pattern is chosen |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom `ReadWriteSeeker` | `github.com/orcaman/writerseeker` | Zero-dep external package; ~100 lines; works correctly; prefer custom to avoid new go.mod entry for trivial code |
| Blocking stream loop | Callback-based `OpenStream` with goroutine | Callback approach is harder to reason about and must never use Go channels inside the callback; blocking stream is cleaner for yap's sequential record-then-encode flow |
| `[]int16` accumulator (growable slice) | Pre-allocated ring buffer | Ring buffer caps max recording size; for 60s at 16kHz mono 16-bit = 1,920,000 samples = 3.84MB — pre-alloc is fine but growable slice is simpler and more Go-idiomatic for this size |

**Installation:**
```bash
go get github.com/go-audio/wav@v1.1.0
```
Note: `gordonklaus/portaudio` is already in go.mod.

## Architecture Patterns

### Recommended Project Structure
```
internal/audio/
├── audio.go          # Recorder struct, Start/Stop/Encode, device enumeration
├── audio_test.go     # Interface-injection tests (no real hardware)
├── chime.go          # PlayChime() goroutine-based blocking output stream
├── chime_test.go     # Chime tests
└── wav.go            # ReadWriteSeeker helper + WAV encoding logic
```

### Pattern 1: Blocking Stream Recording Loop
**What:** Use `portaudio.OpenDefaultStream` in blocking mode. Fixed `[]int16` slice passed by pointer to stream. Call `stream.Read()` in a loop; each call fills the slice with new audio frames. Append filled frames to accumulator slice.

**When to use:** Always for yap's record-then-encode flow. Simpler than callbacks; no CGo-inside-callback issues.

**Example:**
```go
// Source: gordonklaus/portaudio examples/record.go + suapapa gist
// Adapted for 16kHz mono int16

const sampleRate = 16000
const framesPerBuffer = 512  // ~32ms at 16kHz

func (r *Recorder) record(ctx context.Context) error {
    if err := portaudio.Initialize(); err != nil {
        return fmt.Errorf("portaudio init: %w", err)
    }
    defer portaudio.Terminate()

    // AUDIO-02: enumerate devices and fail explicitly if 0 found (PipeWire compat)
    devs, err := portaudio.Devices()
    if err != nil {
        return fmt.Errorf("portaudio devices: %w", err)
    }
    inputDevs := 0
    for _, d := range devs {
        if d.MaxInputChannels > 0 {
            inputDevs++
        }
    }
    if inputDevs == 0 {
        return fmt.Errorf("no audio input devices found: ensure pipewire-alsa or pulseaudio compat is enabled")
    }

    in := make([]int16, framesPerBuffer)
    stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(in), &in)
    if err != nil {
        return fmt.Errorf("open stream: %w", err)
    }
    defer stream.Close()

    if err := stream.Start(); err != nil {
        return fmt.Errorf("stream start: %w", err)
    }
    defer stream.Stop()

    var frames []int16
    for {
        select {
        case <-ctx.Done():
            r.frames = frames
            return nil
        default:
        }
        if err := stream.Read(); err != nil {
            return fmt.Errorf("stream read: %w", err)
        }
        frames = append(frames, in...)
    }
}
```

### Pattern 2: In-Memory WAV Encoding with ReadWriteSeeker
**What:** `wav.NewEncoder` requires `io.WriteSeeker` to seek back and patch RIFF header after writing body. Custom minimal `ReadWriteSeeker` wraps a `[]byte` slice with a position pointer.

**When to use:** Always — `bytes.Buffer` alone is insufficient.

**Example:**
```go
// Source: verified against go-audio/wav encoder.go behavior (Close() seeks to patch headers)
// ReadWriteSeeker implements io.WriteSeeker over an in-memory byte slice

type ReadWriteSeeker struct {
    buf []byte
    pos int
}

func (r *ReadWriteSeeker) Write(p []byte) (n int, err error) {
    minCap := r.pos + len(p)
    if minCap > len(r.buf) {
        r.buf = append(r.buf[:len(r.buf)], make([]byte, minCap-len(r.buf))...)
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
        return 0, fmt.Errorf("invalid whence: %d", whence)
    }
    if abs < 0 {
        return 0, fmt.Errorf("negative position")
    }
    r.pos = int(abs)
    return abs, nil
}

func (r *ReadWriteSeeker) Bytes() []byte {
    return r.buf
}
```

### Pattern 3: WAV Encode from int16 Accumulator
**What:** Convert accumulated `[]int16` frames to `audio.IntBuffer` (which uses `[]int`) and encode via `wav.Encoder`.

**Example:**
```go
// Source: go-audio/wav pkg.go.dev documentation
func encodeWAV(frames []int16) ([]byte, error) {
    ws := &ReadWriteSeeker{}
    enc := wav.NewEncoder(ws,
        16000,  // sample rate: 16kHz
        16,     // bit depth: 16-bit
        1,      // mono
        1,      // PCM audio format
    )

    // audio.IntBuffer.Data is []int not []int16
    data := make([]int, len(frames))
    for i, s := range frames {
        data[i] = int(s)
    }

    buf := &audio.IntBuffer{
        Data: data,
        Format: &audio.Format{
            NumChannels: 1,
            SampleRate:  16000,
        },
    }
    if err := enc.Write(buf); err != nil {
        return nil, fmt.Errorf("wav write: %w", err)
    }
    if err := enc.Close(); err != nil {
        return nil, fmt.Errorf("wav close: %w", err)
    }
    return ws.Bytes(), nil
}
```

### Pattern 4: Async Chime Playback (Output-Only Blocking Stream)
**What:** Chimes play on a SEPARATE PortAudio output stream opened with `numInputChannels=0`. Runs in a goroutine so it does not block the recording path. The chime WAV bytes from `assets.StartChime()` must be decoded to `[]int16` PCM before passing to PortAudio (which is PCM-only).

**Key insight:** PortAudio cannot directly play WAV bytes — the chime data must be decoded first. Use `go-audio/wav` Decoder (same dep already in go.mod after Phase 2) to decode the embedded WAV to PCM.

**When to use:** ASSETS-03 requires chime playback is async and does not block recording.

**Example:**
```go
// Source: gordonklaus/portaudio examples/play.go pattern adapted for WAV + goroutine
func PlayChime(r io.Reader) {
    go func() {
        // Decode WAV to PCM
        dec := wav.NewDecoder(mustReadSeeker(r))
        if !dec.IsValidFile() {
            return // embedded WAV is always valid; log error and return
        }
        pcm, err := dec.FullPCMBuffer()
        if err != nil || pcm == nil {
            return
        }
        samples := make([]int16, len(pcm.Data))
        for i, s := range pcm.Data {
            samples[i] = int16(s)
        }

        if err := portaudio.Initialize(); err != nil {
            return
        }
        defer portaudio.Terminate()

        out := make([]int16, 512)
        stream, err := portaudio.OpenDefaultStream(0, 1, 16000, len(out), &out)
        if err != nil {
            return
        }
        defer stream.Close()
        stream.Start()
        defer stream.Stop()

        for i := 0; i < len(samples); i += len(out) {
            end := i + len(out)
            if end > len(samples) {
                end = len(samples)
                // zero-pad remaining
                copy(out, samples[i:end])
                for j := end - i; j < len(out); j++ {
                    out[j] = 0
                }
            } else {
                copy(out, samples[i:end])
            }
            stream.Write()
        }
    }()
}
```

Note: `io.Reader` from `assets.StartChime()` must be wrapped as `io.ReadSeeker` for `wav.NewDecoder`. Use `bytes.NewReader` on the full bytes:
```go
data, _ := io.ReadAll(r)
rdr := bytes.NewReader(data)
dec := wav.NewDecoder(rdr)
```

### Pattern 5: Interface Injection for Testability
**What:** Wrap the PortAudio-dependent code behind an interface so tests can inject a mock recorder without real hardware.

**When to use:** All unit tests for audio package logic (WAV encoding, ring buffer behavior, error handling).

```go
// AudioRecorder interface — implemented by real PortAudio recorder and mock
type AudioRecorder interface {
    Start() error
    Stop() error
    Frames() []int16  // accumulated PCM after Stop()
    Err() error
}
```

Real implementation calls PortAudio; mock fills `Frames()` with synthetic sine wave data. Tests for WAV encoding, device error handling, and buffer logic run against the mock.

### Anti-Patterns to Avoid
- **Go channel inside PortAudio callback:** The callback fires in a C thread. Sending on a Go channel from inside the callback violates CGo pointer rules and can panic or deadlock. The blocking stream eliminates this risk entirely.
- **bytes.Buffer as io.WriteSeeker:** The WAV encoder calls Seek() to patch RIFF headers. Passing a plain `*bytes.Buffer` will compile but panic at runtime.
- **Calling portaudio.Terminate() without Initialize():** The source code shows `Terminate()` must NOT be called if `Initialize()` returned an error. Pattern: only defer Terminate after confirming Initialize succeeded.
- **Shared PortAudio session for recording and chime:** Two simultaneous `Initialize()` calls are supported (internally reference-counted), but opening two streams on the default device may fail on some ALSA configurations. Pattern: let chime goroutine call its own `Initialize()`/`Terminate()` pair; record stream is independent.
- **Passing `in` by value (not pointer) to OpenDefaultStream:** The stream needs to write INTO the slice. Must pass `&in` (pointer to slice) not `in` (copy). The `record.go` example passes `in` without `&` but that works only because slices are reference types in Go — however, passing a pointer to the slice header is more explicit and matches the blocking stream documentation.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| WAV RIFF header construction | Custom binary writer for RIFF/fmt/data chunks | `github.com/go-audio/wav` | RIFF header has chunk sizes that must be patched after writing; wrong sizes cause Groq API 400; edge cases include alignment padding |
| PCM sample format conversion | Bit manipulation to convert float32/int32 to 16-bit | Cast `int16(sample)` from `[]int16` accumulator | PortAudio natively produces `int16` when buffer type is `[]int16`; no conversion needed |
| Audio device listing | Direct C ALSA/PipeWire API calls | `portaudio.Devices()` | PortAudio abstracts ALSA, PulseAudio, PipeWire-ALSA behind one call |

**Key insight:** The only custom code needed that has no library equivalent is the `ReadWriteSeeker` helper — ~50 lines — which is simpler to own than the `orcaman/writerseeker` dependency.

## Common Pitfalls

### Pitfall 1: PipeWire Silent Success with 0 Devices (AUDIO-02)
**What goes wrong:** `portaudio.Initialize()` returns `nil` on PipeWire-only systems (no ALSA compat), but `portaudio.Devices()` returns an empty slice. Recording produces silence with no error.
**Why it happens:** PortAudio has no native PipeWire backend; it only sees PipeWire when `pipewire-alsa` or `pipewire-pulse` provides an ALSA or PulseAudio compatibility layer.
**How to avoid:** After `Initialize()`, call `portaudio.Devices()` and count devices with `MaxInputChannels > 0`. If count is 0, return an actionable error: `"no audio input devices found: on PipeWire systems enable pipewire-alsa (NixOS: services.pipewire.alsa.enable = true)"`.
**Warning signs:** Device count is 0 after successful Initialize; all recordings are silent `[]int16` slices with all zeros.

### Pitfall 2: bytes.Buffer Not Seekable (AUDIO-05, AUDIO-06)
**What goes wrong:** `wav.NewEncoder(buf, ...)` where `buf` is a `*bytes.Buffer` compiles fine but panics at `enc.Close()` when the encoder seeks back to patch the RIFF header chunk sizes.
**Why it happens:** `wav.NewEncoder` requires `io.WriteSeeker`. `bytes.Buffer` implements `io.Writer` but not `io.Seeker`. Go's type system does not catch this at compile time when passed as `interface{}`.
**How to avoid:** Use the custom `ReadWriteSeeker` struct (see Pattern 2). Never pass `bytes.Buffer` directly to `wav.NewEncoder`.
**Warning signs:** Panic at `enc.Close()` with "invalid memory address" or "operation not supported"; zero-length WAV output.

### Pitfall 3: Raw PCM to Groq API (AUDIO-05)
**What goes wrong:** Sending raw `[]byte` PCM to Groq Whisper API returns HTTP 400 with "invalid file format".
**Why it happens:** Groq expects a valid WAV file with RIFF/fmt/data headers, not raw PCM bytes.
**How to avoid:** Always encode through `wav.NewEncoder`. Verify output with `ffprobe -i file.wav` before the Groq integration (Phase 4). Phase 2 success criterion #1 explicitly gates on ffprobe verification.
**Warning signs:** HTTP 400 from Groq; `ffprobe` reports "Invalid data found when processing input".

### Pitfall 4: Temp Files Created During Recording (NFR-06, AUDIO-03, Pitfall #7)
**What goes wrong:** Writing PCM to a temp file in `/tmp` or `$XDG_RUNTIME_DIR` instead of keeping it in memory. On crash, temp files orphan and leak disk space.
**Why it happens:** The canonical example `record.go` writes to a file directly; developers copy this pattern.
**How to avoid:** Accumulate frames in `[]int16` in memory. Encode WAV to `ReadWriteSeeker` in memory. Never call `os.CreateTemp` or write to any path during recording.
**Warning signs:** Files appearing in `/tmp/yap-*` or `$XDG_RUNTIME_DIR/yap-*`; `lsof` shows temp file handles during recording.

### Pitfall 5: Terminate Called After Failed Initialize
**What goes wrong:** `defer portaudio.Terminate()` placed before checking `Initialize()` error causes Terminate to be called on a never-initialized PortAudio, which can crash.
**Why it happens:** Typical Go deferred cleanup pattern does not account for conditional initialization.
**How to avoid:**
```go
if err := portaudio.Initialize(); err != nil {
    return err
}
defer portaudio.Terminate()  // Only reached if Initialize succeeded
```
**Warning signs:** Double-free or panic in PortAudio C layer on error paths.

### Pitfall 6: Chime Blocks Recording Start (ASSETS-03)
**What goes wrong:** Calling `PlayChime()` synchronously before starting recording delays the first audio frame capture. User hears chime but their first words are lost.
**Why it happens:** `stream.Write()` loop for chime playback is blocking; if not goroutined, it blocks the caller.
**How to avoid:** `PlayChime` must launch a goroutine immediately and return. The goroutine owns its own PortAudio stream lifecycle.
**Warning signs:** Recording always misses the first ~300ms of speech; chime and first word overlap.

### Pitfall 7: audio.IntBuffer.Data is []int not []int16
**What goes wrong:** `enc.Write(buf)` where `buf.Data` contains int16 values bit-cast into int16 slice instead of `[]int` causes type mismatch or wrong encoding.
**Why it happens:** `audio.IntBuffer.Data` is `[]int` (platform-int, 64-bit on amd64), not `[]int16`. PCM values must be stored as `int` even though they are 16-bit range values.
**How to avoid:** Explicit conversion: `data[i] = int(int16Sample)` when building `audio.IntBuffer.Data` from `[]int16`.
**Warning signs:** WAV file has correct header but garbled audio (values out of range); ffprobe shows correct format but waveform is noise.

## Code Examples

Verified patterns from official sources:

### Device Enumeration with PipeWire Guard
```go
// Source: gordonklaus/portaudio portaudio.go (Devices() function, verified in module cache)
// AUDIO-02 compliance pattern

func checkAudioDevices() error {
    devs, err := portaudio.Devices()
    if err != nil {
        return fmt.Errorf("enumerate audio devices: %w", err)
    }
    inputCount := 0
    for _, d := range devs {
        if d.MaxInputChannels > 0 {
            inputCount++
        }
    }
    if inputCount == 0 {
        return fmt.Errorf(
            "no audio input devices available\n" +
            "On PipeWire systems: ensure pipewire-alsa is enabled\n" +
            "NixOS: services.pipewire.alsa.enable = true")
    }
    return nil
}
```

### Named Device Selection (for config.MicDevice)
```go
// Source: gordonklaus/portaudio portaudio.go (DeviceInfo.Name field, verified in module cache)
// AUDIO-01: use config.MicDevice if set, otherwise default

func selectInputDevice(deviceName string) (*portaudio.DeviceInfo, error) {
    if deviceName == "" {
        return portaudio.DefaultInputDevice()
    }
    devs, err := portaudio.Devices()
    if err != nil {
        return nil, err
    }
    for _, d := range devs {
        if d.Name == deviceName && d.MaxInputChannels > 0 {
            return d, nil
        }
    }
    return nil, fmt.Errorf("audio device %q not found", deviceName)
}
```

### Ring Buffer Size Calculation
```
60 seconds recording at 16kHz mono 16-bit:
  - 16,000 samples/sec × 60 sec = 960,000 samples
  - 960,000 × 2 bytes (int16) = 1,920,000 bytes = ~1.88 MB
  - Plus audio.IntBuffer.Data []int conversion = additional ~7.5MB ([]int64 on amd64)
  - Total peak RAM during encode: ~10MB (within NFR-03 15MB idle limit but only during active recording)

Pre-allocation strategy:
  frames := make([]int16, 0, 16000*60) // 1.88MB upfront, no realloc during recording
```

### Test Pattern: Mock Recorder
```go
// Source: standard Go interface injection pattern (no library citation needed)
// Enables testing WAV encoding logic without hardware

type fakeRecorder struct {
    frames []int16
    err    error
}

func (f *fakeRecorder) Start() error  { return f.err }
func (f *fakeRecorder) Stop() error   { return f.err }
func (f *fakeRecorder) Frames() []int16 { return f.frames }

func TestEncodeWAV(t *testing.T) {
    // 0.1 sec of 1kHz sine wave at 16kHz
    frames := make([]int16, 1600)
    for i := range frames {
        frames[i] = int16(10000 * math.Sin(2*math.Pi*1000*float64(i)/16000))
    }
    wavBytes, err := encodeWAV(frames)
    if err != nil {
        t.Fatalf("encodeWAV: %v", err)
    }
    // Verify RIFF magic
    if string(wavBytes[0:4]) != "RIFF" {
        t.Errorf("missing RIFF header")
    }
    // Verify WAV size > 44 bytes (header minimum)
    if len(wavBytes) < 44 {
        t.Errorf("WAV too small: %d bytes", len(wavBytes))
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Callback-based stream with channels | Blocking stream with `stream.Read()` loop | Established pattern | Eliminates CGo callback/goroutine conflict; simpler code |
| Write PCM to disk, pass path to encoder | Encode WAV in-memory to `[]byte` | Required since AUDIO-03/06 | No temp files, no cleanup needed |
| `bytes.Buffer` as write target | Custom `ReadWriteSeeker` with seek support | go-audio/wav requirement | Correct RIFF header patching |

**Deprecated/outdated:**
- Callback with `cgo.Handle`: Valid but unnecessary for yap's sequential flow; blocking stream is simpler.
- `io.Pipe` for streaming PCM to encoder: Overcomplicated; yap records bounded audio then encodes — not streaming.

## Open Questions

1. **Multiple portaudio.Initialize() calls in same process**
   - What we know: The gordonklaus/portaudio source uses an `initialized` ref-count counter; `Terminate()` decrements it; final Terminate closes all streams.
   - What's unclear: Whether opening two simultaneous PortAudio streams (recording + chime playback) on the same default device causes device conflict on ALSA backends.
   - Recommendation: In Phase 2, test chime playback while recording is active. If device conflict occurs, serialize: stop recording → play chime → re-open for recording. PipeWire handles multiple streams natively; ALSA may not.

2. **wav.NewDecoder requires io.ReadSeeker, assets.StartChime() returns io.Reader**
   - What we know: `wav.NewDecoder` signature is `NewDecoder(r io.ReadSeeker)`. `assets.StartChime()` returns `io.Reader` (from `bytes.NewReader`).
   - What's unclear: Does `bytes.NewReader` (returned internally) implement `io.ReadSeeker`? Yes — `bytes.NewReader` returns `*bytes.Reader` which implements `io.ReadSeeker`. But `assets.StartChime()` returns `io.Reader` (interface), losing the `Seek` method.
   - Recommendation: Change `assets.StartChime()` and `assets.StopChime()` return type from `io.Reader` to `io.ReadSeeker`, OR do `data, _ := io.ReadAll(r); bytes.NewReader(data)` at the call site. The latter is simpler and avoids a Phase 1 interface change.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (no external test framework) |
| Config file | none — `go test ./...` uses Go toolchain directly |
| Quick run command | `go test ./internal/audio/...` |
| Full suite command | `go test ./...` |

### Phase Requirements to Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| AUDIO-01 | OpenDefaultStream uses config.MicDevice or default input device | unit (mock) | `go test ./internal/audio/... -run TestDeviceSelection` | Wave 0 |
| AUDIO-02 | Returns clear error when 0 input devices enumerated after Initialize | unit (mock) | `go test ./internal/audio/... -run TestPipeWireGuard` | Wave 0 |
| AUDIO-03 | No temp files in /tmp or $XDG_RUNTIME_DIR during or after recording | unit (mock) | `go test ./internal/audio/... -run TestNoTempFiles` | Wave 0 |
| AUDIO-04 | PCM data not passed via Go channel inside PortAudio callback | code review / unit | `go test ./internal/audio/... -run TestRecorderFrames` | Wave 0 |
| AUDIO-05 | WAV output has valid RIFF/fmt/data headers (16kHz 16-bit mono) | unit | `go test ./internal/audio/... -run TestEncodeWAV` | Wave 0 |
| AUDIO-06 | WAV encoding is in-memory only (no disk I/O) | unit | `go test ./internal/audio/... -run TestInMemoryEncode` | Wave 0 |
| ASSETS-03 | Chime playback goroutine returns immediately without blocking | unit (mock) | `go test ./internal/audio/... -run TestChimeAsync` | Wave 0 |
| NFR-03 | Idle RAM < 15MB — audio package allocates predictably | benchmark | `go test ./internal/audio/... -bench=BenchmarkRecorder -memprofile` | Wave 0 |
| NFR-06 | No temp files written to disk | unit (see AUDIO-03) | combined | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/audio/...`
- **Per wave merge:** `go test ./...`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `internal/audio/audio_test.go` — covers AUDIO-01 through AUDIO-06, NFR-03, NFR-06
- [ ] `internal/audio/chime_test.go` — covers ASSETS-03
- [ ] `internal/audio/wav_test.go` — covers AUDIO-05 WAV header validation in detail
- [ ] `go get github.com/go-audio/wav@v1.1.0` — not yet in go.mod

## Sources

### Primary (HIGH confidence)
- `gordonklaus/portaudio` Go module cache at `/home/hybridz/go/pkg/mod/github.com/gordonklaus/portaudio@v0.0.0-20260203164431-765aa7dfa631/portaudio.go` — complete API (Initialize, Terminate, Devices, DefaultInputDevice, OpenStream, OpenDefaultStream, stream.Start/Stop/Close/Read)
- `gordonklaus/portaudio` examples `/examples/record.go` and `/examples/play.go` — verified blocking stream pattern for recording and playback
- [pkg.go.dev/github.com/go-audio/wav](https://pkg.go.dev/github.com/go-audio/wav) — NewEncoder signature, audio.IntBuffer type, Close() behavior, v1.1.0 confirmed latest

### Secondary (MEDIUM confidence)
- [suapapa gist: raw audio recording with portaudio](https://gist.github.com/suapapa/d598d99360497252433af430902bb49e) — 16kHz int16 recording pattern with `OpenDefaultStream(1, 0, 16000, ...)` confirmed correct
- [github.com/orcaman/writerseeker](https://github.com/orcaman/writerseeker) — confirmed zero-dep in-memory WriteSeeker exists; custom impl preferred to avoid dependency
- [Go stdlib proposal: bytes.Buffer Seek #21899](https://github.com/golang/go/issues/21899) — confirms bytes.Buffer lacks Seek; custom helper required

### Tertiary (LOW confidence)
- WebSearch findings on blocking stream playback cutoff (gordonklaus issue #38) — drain pattern may need extra Write() calls; verify empirically during Phase 2

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — portaudio API read directly from source in module cache; go-audio/wav API verified on pkg.go.dev
- Architecture: HIGH — blocking stream pattern confirmed from multiple examples; ReadWriteSeeker requirement verified from go-audio/wav API
- Pitfalls: HIGH — PipeWire pitfall documented in PITFALLS.md with prevention; bytes.Buffer/WriteSeeker verified from stdlib proposal; others confirmed by source inspection

**Research date:** 2026-03-07
**Valid until:** 2026-04-07 (portaudio and go-audio/wav are stable; no breaking changes expected)

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| AUDIO-01 | Audio capture via gordonklaus/portaudio using system default input device (configurable) | OpenDefaultStream + named device enumeration via Devices(); config.MicDevice → DeviceInfo.Name lookup |
| AUDIO-02 | After Pa_Initialize(), enumerate devices and fail with actionable error if count is 0 (PipeWire compat check) | portaudio.Devices() returns []*DeviceInfo; count MaxInputChannels > 0; return formatted error with NixOS fix |
| AUDIO-03 | PCM samples held in in-memory ring buffer during capture; no temp files written to disk | Blocking stream + []int16 accumulator with append; zero file I/O in recording path |
| AUDIO-04 | Ring buffer drained by goroutine; PCM data never passed via Go channel inside PortAudio callback | Blocking stream (not callback stream) eliminates this risk entirely; no callback → no channel-in-callback |
| AUDIO-05 | WAV encoding to 16kHz 16-bit mono PCM via go-audio/wav; full RIFF/fmt/data headers | wav.NewEncoder(ws, 16000, 16, 1, 1); enc.Write(audio.IntBuffer); enc.Close() patches headers |
| AUDIO-06 | WAV encoding performed in-memory (to bytes.Buffer); no disk I/O in the recording path | Custom ReadWriteSeeker as target for wav.NewEncoder; .Bytes() returns []byte |
| ASSETS-03 | Chime playback is async (does not block recording start/stop path) | PlayChime() launches goroutine; owns independent PortAudio output stream; returns immediately |
| NFR-03 | Idle RAM usage under 15MB | Pre-alloc []int16 accumulator 1.88MB; audio.IntBuffer conversion ~7.5MB only during active encode; no idle allocation |
| NFR-06 | No temp files written to disk during normal operation | In-memory accumulator + in-memory WAV encoding; no os.Create, os.CreateTemp in audio path |
</phase_requirements>
