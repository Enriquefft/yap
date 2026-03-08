---
phase: 02-audio-pipeline
plan: "02"
subsystem: audio
tags: [portaudio, go-audio, wav, cgo, pcm, ring-buffer, in-memory]

requires:
  - phase: 02-audio-pipeline/02-01
    provides: go-audio/wav dependency pinned, Wave 0 test stubs with skip labels

provides:
  - ReadWriteSeeker: in-memory io.WriteSeeker for wav.NewEncoder (no bytes.Buffer workaround needed)
  - encodeWAV([]int16) ([]byte, error): 16kHz/16-bit/mono WAV encoding entirely in memory
  - AudioRecorder interface: Start/Stop/Frames/Encode contract
  - Recorder struct: PortAudio blocking stream, PipeWire guard, named device selection
  - All AUDIO-01 through AUDIO-06 + NFR-06 test stubs replaced with passing tests

affects: [03-chime, 04-daemon, 04-groq, phase-3, phase-4]

tech-stack:
  added: []
  patterns:
    - "package audio (not audio_test) for test files — unexported symbol access"
    - "fakeRecorder test double — interface testing without real PortAudio"
    - "TDD: RED commit test stubs → GREEN commit implementation"
    - "Context cancellation as primary stop signal for blocking streams"
    - "Pre-allocate 60s PCM capacity to avoid realloc during capture"

key-files:
  created:
    - internal/audio/wav.go
    - internal/audio/audio.go
  modified:
    - internal/audio/wav_test.go
    - internal/audio/audio_test.go

key-decisions:
  - "package audio (not audio_test) for test files — encodeWAV and ReadWriteSeeker are unexported"
  - "TestEncodeWAV/TestInMemoryEncode renamed in audio_test.go to avoid name collision with wav_test.go"
  - "fakeRecorder implements AudioRecorder inline in test file — no mock framework needed"
  - "Recorder.Stop() is no-op — context cancellation is the primary stop mechanism"
  - "CGo build requires gcc-wrapper (not bare gcc) + portaudio pkgconfig from Nix store"

patterns-established:
  - "In-memory WAV encoding: ReadWriteSeeker + wav.NewEncoder pattern — no os.File at any step"
  - "PipeWire guard: check MaxInputChannels > 0 count after portaudio.Initialize()"
  - "Blocking stream loop: select ctx.Done() + stream.Read() — no goroutine channel in callback"

requirements-completed: [AUDIO-01, AUDIO-02, AUDIO-03, AUDIO-04, AUDIO-05, AUDIO-06, NFR-06]

duration: 12min
completed: 2026-03-07
---

# Phase 2 Plan 02: Audio Recorder Implementation Summary

**In-memory PCM capture and WAV encoding via ReadWriteSeeker + go-audio/wav, PortAudio blocking stream with PipeWire guard and named device selection**

## Performance

- **Duration:** 12 min
- **Started:** 2026-03-08T04:00:03Z
- **Completed:** 2026-03-08T04:12:00Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- ReadWriteSeeker: growable in-memory io.WriteSeeker satisfying wav.NewEncoder's io.WriteSeeker requirement
- encodeWAV: 16kHz/16-bit/mono WAV in-memory encoding — RIFF/WAVE headers verified by tests
- AudioRecorder interface + Recorder struct: blocking PortAudio stream loop, PipeWire input-device guard, named device lookup, context-cancel stop signal
- All 9 test stubs replaced with passing tests (0 skips in audio package, 2 future Plan 03 skips unchanged)

## Task Commits

Each task was committed atomically (TDD pattern: test → feat):

1. **Task 1: ReadWriteSeeker + WAV encoder (RED)** - `214153f` (test)
2. **Task 1: ReadWriteSeeker + WAV encoder (GREEN)** - `01f215d` (feat)
3. **Task 2: AudioRecorder + Recorder (RED)** - `ff15414` (test)
4. **Task 2: AudioRecorder + Recorder (GREEN)** - `12b0316` (feat)

## Files Created/Modified

- `internal/audio/wav.go` — ReadWriteSeeker struct + encodeWAV() in-memory WAV encoder
- `internal/audio/audio.go` — AudioRecorder interface + Recorder struct with PortAudio integration
- `internal/audio/wav_test.go` — Tests for ReadWriteSeeker Write/Seek/overwrite, WAV header, in-memory encoding
- `internal/audio/audio_test.go` — Tests for PipeWire guard, device selection, frame accumulation, no-temp-file encoding

## Decisions Made

- **package audio (not audio_test)** in test files: encodeWAV and ReadWriteSeeker are unexported; test package must match to access them
- **Renamed TestEncodeWAV/TestInMemoryEncode in audio_test.go** to TestRecorderEncodeWAV/TestRecorderInMemoryEncode: both test files in same package caused redeclaration errors
- **fakeRecorder pattern**: implements AudioRecorder inline in test — no mock framework, direct encodeWAV delegation
- **Recorder.Stop() is no-op**: context cancellation is the authoritative stop mechanism; Stop() exists for interface satisfaction only
- **CGo toolchain**: bare `gcc` from nix store lacks crt1.o/crti.o; must use `gcc-wrapper` package which includes proper library search paths

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Renamed conflicting test function names**
- **Found during:** Task 2 RED phase (audio_test.go compilation)
- **Issue:** Both wav_test.go and audio_test.go declared TestEncodeWAV and TestInMemoryEncode in package audio — Go does not allow redeclaration in the same package
- **Fix:** Renamed audio_test.go versions to TestRecorderEncodeWAV and TestRecorderInMemoryEncode (more descriptive anyway)
- **Files modified:** internal/audio/audio_test.go
- **Verification:** go test ./internal/audio/... builds and all tests PASS
- **Committed in:** ff15414 (Task 2 RED commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - naming collision bug)
**Impact on plan:** Required for correctness. Renamed tests are more descriptive. No scope creep.

## Issues Encountered

- CGo build failed with bare `gcc` (no crt1.o/crti.o) — resolved by using `gcc-wrapper-15.2.0` from Nix store which includes proper C runtime library paths. This is the expected Nix pattern for CGo builds outside a devShell.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- AudioRecorder interface ready for Plan 03 (chime playback + async integration)
- encodeWAV produces Whisper-compatible 16kHz mono WAV bytes — ready for Phase 4 Groq API
- All AUDIO-01 through AUDIO-06 requirements satisfied
- NFR-06 (no file I/O in audio path) verified by TestNoTempFiles and TestInMemoryEncode

---
*Phase: 02-audio-pipeline*
*Completed: 2026-03-07*
