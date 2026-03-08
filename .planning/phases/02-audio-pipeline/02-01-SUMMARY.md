---
phase: 02-audio-pipeline
plan: "01"
subsystem: testing
tags: [go-audio, wav, audio, test-stubs, nyquist]

# Dependency graph
requires:
  - phase: 01-foundation
    provides: go.mod scaffold, module path github.com/hybridz/yap, portaudio dependency
provides:
  - Wave 0 test stubs for all audio requirements (AUDIO-01 through AUDIO-06, ASSETS-03, NFR-03, NFR-06)
  - go-audio/wav v1.1.0 and go-audio/audio v1.0.0 in go.mod
  - internal/audio package with 11 named test stubs that compile and skip
affects:
  - 02-audio-pipeline/02-02
  - 02-audio-pipeline/02-03

# Tech tracking
tech-stack:
  added:
    - github.com/go-audio/wav v1.1.0
    - github.com/go-audio/audio v1.0.0 (transitive)
    - github.com/go-audio/riff v1.0.0 (transitive)
  patterns:
    - Wave 0 / Nyquist rule — test stubs committed before implementation
    - package audio_test external test package pattern for audio package

key-files:
  created:
    - internal/audio/audio_test.go
    - internal/audio/wav_test.go
    - internal/audio/chime_test.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "go-audio/wav v1.1.0 pinned explicitly; go-audio/riff v1.0.0 pulled as transitive alongside go-audio/audio v1.0.0"
  - "Test stubs use t.Skip with wave/plan label so grep can identify pending vs implemented tests"

patterns-established:
  - "Wave 0 stubs: all new packages get test stubs before any implementation — enables `go test ./...` as universal verify command"
  - "package audio_test external test package: forces clean API surface, no access to unexported internals"

requirements-completed: [AUDIO-01, AUDIO-02, AUDIO-03, AUDIO-04, AUDIO-05, AUDIO-06, ASSETS-03, NFR-03, NFR-06]

# Metrics
duration: 4min
completed: 2026-03-08
---

# Phase 2 Plan 01: Audio Test Stubs and go-audio/wav Dependency Summary

**Wave 0 test stubs for all 11 audio requirements using go-audio/wav v1.1.0, enabling `go test ./internal/audio/...` as the automated verify command for Plans 02 and 03**

## Performance

- **Duration:** 4 min
- **Started:** 2026-03-08T03:54:31Z
- **Completed:** 2026-03-08T03:57:57Z
- **Tasks:** 1
- **Files modified:** 5

## Accomplishments
- Added github.com/go-audio/wav v1.1.0 (plus go-audio/audio, go-audio/riff transitives) to go.mod
- Created internal/audio/audio_test.go with 6 stubs covering AUDIO-01 through AUDIO-06 and NFR-06
- Created internal/audio/wav_test.go with 3 stubs covering WAV header validation and ReadWriteSeeker
- Created internal/audio/chime_test.go with 2 test stubs and 1 benchmark covering ASSETS-03 and NFR-03
- All 11 tests compile clean and skip correctly; go build ./... passes

## Task Commits

Each task was committed atomically:

1. **Task 1: Add go-audio/wav dependency and create audio package scaffold** - `bb2b0f7` (test)

## Files Created/Modified
- `internal/audio/audio_test.go` - 6 Wave 0 stubs: TestDeviceSelection, TestPipeWireGuard, TestNoTempFiles, TestRecorderFrames, TestEncodeWAV, TestInMemoryEncode
- `internal/audio/wav_test.go` - 3 Wave 0 stubs: TestWAVHeader, TestReadWriteSeeker, TestInt16Conversion
- `internal/audio/chime_test.go` - 2 test + 1 benchmark stubs: TestChimeAsync, TestChimeNonBlocking, BenchmarkRecorder
- `go.mod` - Added go-audio/wav v1.1.0, go-audio/audio v1.0.0, go-audio/riff v1.0.0
- `go.sum` - Updated with new dependency checksums

## Decisions Made
- go-audio/wav v1.1.0 pinned explicitly; go-audio/riff v1.0.0 pulled as transitive alongside go-audio/audio v1.0.0 — all three present in go.mod as expected
- Test stubs use `t.Skip("Wave 0 stub — implement in Plan 0N")` labeling so it's immediately clear which plan implements each stub

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
- Go binary not in PATH by default in shell environment; resolved by using explicit nix store path `/nix/store/qpblhiv8rdnw45j0ghbbswcckcgsa6i0-go-1.25.6/bin/go`. This is expected in a Nix-managed environment without direnv active.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Wave 0 stubs are in place; `go test ./internal/audio/...` shows 11 SKIPs and exits cleanly
- Plan 02-02 can now implement the audio recorder and WAV encoder against these stubs
- Plan 02-03 can implement the chime player against the chime stubs

---
*Phase: 02-audio-pipeline*
*Completed: 2026-03-08*
