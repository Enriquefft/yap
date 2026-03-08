---
phase: 2
slug: audio-pipeline
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-08
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — Wave 0 creates test stubs |
| **Quick run command** | `go test ./internal/audio/... ./internal/assets/...` |
| **Full suite command** | `go test ./...` |
| **Estimated runtime** | ~5 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/audio/...`
- **After every plan wave:** Run `go test ./...`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 10 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 2-01-01 | 01 | 1 | AUDIO-01 | unit | `go test ./internal/audio/... -run TestDeviceEnumeration` | ❌ W0 | ⬜ pending |
| 2-01-02 | 01 | 1 | AUDIO-02 | unit | `go test ./internal/audio/... -run TestRingBuffer` | ❌ W0 | ⬜ pending |
| 2-01-03 | 01 | 1 | AUDIO-03 | unit | `go test ./internal/audio/... -run TestPipeWireGuard` | ❌ W0 | ⬜ pending |
| 2-02-01 | 02 | 2 | AUDIO-04 | unit | `go test ./internal/audio/... -run TestWAVEncoding` | ❌ W0 | ⬜ pending |
| 2-02-02 | 02 | 2 | AUDIO-05 | unit | `go test ./internal/audio/... -run TestNoTempFiles` | ❌ W0 | ⬜ pending |
| 2-02-03 | 02 | 2 | NFR-03 | unit | `go test ./internal/audio/... -run TestInt16Conversion` | ❌ W0 | ⬜ pending |
| 2-03-01 | 03 | 3 | ASSETS-03 | unit | `go test ./internal/assets/... -run TestChimePlayback` | ❌ W0 | ⬜ pending |
| 2-03-02 | 03 | 3 | AUDIO-06 | unit | `go test ./internal/audio/... -run TestChimeNonBlocking` | ❌ W0 | ⬜ pending |
| 2-03-03 | 03 | 3 | NFR-06 | manual | record 3s audio, verify WAV with ffprobe | N/A | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/audio/audio_test.go` — stubs for AUDIO-01 through AUDIO-06, NFR-03
- [ ] `internal/audio/ringbuffer_test.go` — stubs for ring buffer unit tests
- [ ] `internal/assets/chime_test.go` — stubs for ASSETS-03 chime playback tests

*Framework `go test` is stdlib — no install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Records 3s WAV, valid ffprobe output | NFR-06 | Requires real microphone | Run `yap capture --duration 3s -o /tmp/test.wav && ffprobe /tmp/test.wav` |
| Chimes don't block recording goroutine | AUDIO-06 | Timing-sensitive | Record while playing chime; verify no gap/delay in audio |
| PipeWire-only shows actionable error | AUDIO-03 | Requires PipeWire env | Run on PipeWire-only system without pipewire-alsa |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 10s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
