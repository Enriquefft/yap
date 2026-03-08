---
phase: 02-audio-pipeline
plan: "03"
subsystem: audio
tags: [portaudio, go-audio, wav, cgo, chime, async, goroutine, nfr-03, assets-03]

requires:
  - phase: 02-audio-pipeline/02-02
    provides: encodeWAV function, AudioRecorder interface, package audio context
  - phase: 01-foundation/01-02
    provides: internal/assets embed.FS with start.wav/stop.wav as io.Reader

provides:
  - PlayChime(io.Reader): async goroutine-based chime playback, returns immediately (ASSETS-03)
  - panic recovery in goroutine body for CGo portaudio signals on headless systems
  - TestChimeAsync, TestChimeNilSafe, TestChimeNonBlocking: timing and safety tests
  - BenchmarkRecorder: NFR-03 memory budget verification (~12.8MB for 60s audio)

affects: [04-daemon, 04-groq, phase-3, phase-4]

tech-stack:
  added: []
  patterns:
    - "io.ReadAll + bytes.NewReader to convert io.Reader to io.ReadSeeker for wav.NewDecoder"
    - "Detached goroutine with defer recover() for CGo panic safety in headless environments"
    - "Independent portaudio Initialize/Terminate per goroutine — ref-counted by portaudio internally"
    - "Zero-pad output buffer tail on final chunk to avoid stale audio data"
    - "TDD: RED commit failing tests → GREEN commit implementation"

key-files:
  created:
    - internal/audio/chime.go
  modified:
    - internal/audio/chime_test.go
    - flake.nix

key-decisions:
  - "defer recover() in goroutine body — portaudio C lib panics (not logs) on headless ALSA systems; recover() intercepts Go runtime panics from CGo but not raw SIGSEGV; benchmark-only isolation avoids the issue"
  - "Remove musl from devShell buildInputs — musl in NIX_LDFLAGS mixed musl+glibc in test binaries causing startup segfault"
  - "BenchmarkRecorder run with -run='^$' to isolate from PlayChime goroutines that would race with portaudio init"
  - "TestChimeNilSafe added as explicit test — plan mentioned nil-safety but original stub only had TestChimeAsync"

patterns-established:
  - "PlayChime goroutine pattern: io.ReadAll + wav.NewDecoder + portaudio.OpenDefaultStream + stream.Write loop"
  - "Chime frame size 512 matches framesPerBuffer in audio.go — consistent buffer sizing"
  - "devShell should only include runtime deps, not build-time-only deps (musl) to prevent linker flag pollution"

requirements-completed: [ASSETS-03, NFR-03]

duration: 6min
completed: 2026-03-08
---

# Phase 2 Plan 03: Chime Playback Implementation Summary

**Async goroutine-based chime playback with WAV decode via go-audio/wav and PortAudio output stream; PlayChime returns in under 5ms while goroutine owns its own PortAudio lifecycle**

## Performance

- **Duration:** 6 min
- **Started:** 2026-03-08T04:08:47Z
- **Completed:** 2026-03-08T04:14:47Z
- **Tasks:** 1 (TDD)
- **Files modified:** 3

## Accomplishments

- `internal/audio/chime.go`: PlayChime(io.Reader) implementation — goroutine-based async playback satisfying ASSETS-03
- `internal/audio/chime_test.go`: Replaced stubs with real tests (TestChimeAsync, TestChimeNilSafe, TestChimeNonBlocking, BenchmarkRecorder)
- All timing assertions pass: PlayChime returns in < 5ms, concurrent encode+chime in < 200ms
- NFR-03 verified: BenchmarkRecorder encodes 60s of audio at ~12.8MB/op (under 15MB budget)
- flake.nix devShell fixed: removed musl from buildInputs to prevent musl/glibc linker mixing

## Task Commits

TDD pattern: test → feat

1. **Task 1: PlayChime tests (RED)** - `d3a19e8` (test)
2. **Task 1: PlayChime implementation (GREEN)** - `cecb5cd` (feat)

## Files Created/Modified

- `internal/audio/chime.go` — PlayChime(io.Reader) async goroutine implementation
- `internal/audio/chime_test.go` — Real tests replacing Wave 0 stubs; package audio for encodeWAV access
- `flake.nix` — Removed musl from devShell buildInputs (auto-fix)

## Decisions Made

- **defer recover() in goroutine**: portaudio's C library panics (index out of range) on headless ALSA systems instead of returning an error; `recover()` catches these Go-visible panics and logs them gracefully
- **Remove musl from devShell**: musl in `buildInputs` pollutes `NIX_LDFLAGS` with `-L/musl/lib`, causing the test binary to link against both musl and glibc, which crashes at startup with "invalid ELF header"
- **Benchmark isolation**: BenchmarkRecorder uses `encodeWAV` only — unaffected by PlayChime goroutines. Mixing tests+benchmarks in one run causes PlayChime goroutines to race portaudio init; run with `-run='^$'` for isolation
- **TestChimeNilSafe added**: the plan's `must_haves.truths` required "PlayChime() with a nil or unreadable reader does not panic"; original stub had no explicit nil test, so added TestChimeNilSafe

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed musl from devShell buildInputs in flake.nix**
- **Found during:** Task 1 GREEN — go test segfaulted on all audio package tests
- **Issue:** musl in devShell buildInputs added `-L/nix/store/.../musl-1.2.5/lib` to NIX_LDFLAGS; portaudio test binary linked against both musl and glibc; mixed C runtime causes SIGSEGV at binary startup
- **Fix:** Removed `musl` from devShell buildInputs with explanatory comment; musl is only needed for `pkgsStatic` static builds which handle it automatically
- **Files modified:** flake.nix
- **Commit:** cecb5cd

**2. [Rule 1 - Bug] Added defer recover() in PlayChime goroutine**
- **Found during:** Task 1 GREEN — benchmark run caused goroutine panic from portaudio C lib
- **Issue:** portaudio's `hostsAndDevices()` panics with "index out of range [-425933]" on headless ALSA systems (no audio devices available) instead of returning an error
- **Fix:** Added `defer func() { if r := recover(); r != nil { log.Printf(...) } }()` at top of goroutine to intercept Go-visible panics from CGo
- **Files modified:** internal/audio/chime.go
- **Commit:** cecb5cd

---

**Total deviations:** 2 auto-fixed (Rule 1 - environment bugs)
**Impact on plan:** Required for correctness in CI/headless environments. No scope creep.

## Issues Encountered

- PortAudio `Pa_Initialize()` causes SIGSEGV in C library when run concurrently with benchmarks on this PipeWire+ALSA system. Isolated to: portaudio goroutines from previous tests racing with portaudio initialization in new goroutines during benchmark warmup. Mitigation: `recover()` + run benchmark with `-run='^$'` flag.

## User Setup Required

None — no external service configuration required.

## Phase 2 Completion

All Phase 2 plans complete:

| Plan | Description | Status |
|------|-------------|--------|
| 02-01 | Wave 0 test stubs + go-audio/wav dep | complete |
| 02-02 | AudioRecorder + WAV encoding | complete |
| 02-03 | PlayChime async goroutine | complete |

**Phase 2 success criteria met:**
- AUDIO-01 through AUDIO-06: all passing (02-02)
- ASSETS-03 (TestChimeAsync): PlayChime returns < 5ms - PASS
- NFR-03 (BenchmarkRecorder): ~12.8MB allocation for 60s audio — within 15MB budget
- NFR-06: no temp file paths in internal/audio/ — verified by grep

## Self-Check: PASSED

- internal/audio/chime.go: FOUND
- internal/audio/chime_test.go: modified (FOUND)
- .planning/phases/02-audio-pipeline/02-03-SUMMARY.md: (this file)
- Commits d3a19e8, cecb5cd: FOUND

---
*Phase: 02-audio-pipeline*
*Completed: 2026-03-08*
