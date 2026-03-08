---
gsd_state_version: 1.0
milestone: v0.1
milestone_name: milestone
current_plan: 03-02-complete
status: executing
stopped_at: Completed 03-ipc-daemon/03-02-PLAN.md
last_updated: "2026-03-08T04:50:00.000Z"
progress:
  total_phases: 5
  completed_phases: 2
  total_plans: 7
  completed_plans: 7
---

# Project State — yap

## Current Status

**Phase:** 03-ipc-daemon (in progress)
**Current Plan:** 03-01 (daemon-core) — COMPLETE
**Next action:** Begin Phase 3-02 — IPC server + CLI integration
**Milestone:** v0.1
**Last session:** 2026-03-08T04:35:00.000Z
**Stopped at:** Completed 03-ipc-daemon/03-01-PLAN.md

## Initialization Summary

- Project initialized: 2026-03-07
- Research completed: STACK, FEATURES, ARCHITECTURE, PITFALLS, SUMMARY
- Requirements defined: REQUIREMENTS.md (5 phases, 60 requirements)
- Roadmap created: ROADMAP.md (5 phases)

## Phase Status

| Phase | Status | Notes |
|-------|--------|-------|
| Phase 1 — Foundation | complete | All 3/3 plans complete |
| Phase 2 — Audio Pipeline | complete | All 3/3 plans complete |
| Phase 3 — IPC + Daemon | pending | Unix socket + daemon lifecycle |
| Phase 4 — Input + Output | pending | evdev + Groq + paste fallback |
| Phase 5 — Polish + Distribution | pending | Wizard + curl install + NixOS module |

## Key Decisions

### Phase 3-01: Daemon Core

- **Daemon.Run() uses signal.NotifyContext** for clean SIGTERM handling (no os.Exit calls)
- **PID file uses O_EXCL flag** for atomic creation (prevents DAEMON-05 race condition)
- **IsLive uses Signal(0)** for correct Unix liveness test instead of FindProcess
- **daemonRun flag added to root command** for Phase 3-02 daemon spawning logic

### Previous Phases

- **Stack diverges from PRD** in 4 places (research-confirmed):
  - `holoplot/go-evdev` (pure Go) over `gvalkov/golang-evdev`
  - `adrg/xdg` over `os.UserConfigDir()` (bug in stdlib)
  - `atotto/clipboard` over `golang-design/clipboard`
  - `gen2brain/beeep` over generic "libnotify"
- **Phase 1 is critical path** — CGo/musl-gcc static build must work before anything else
- **Highest risk:** Wayland paste fallback chain (Phase 4)
- **Module path** `github.com/hybridz/yap` matches GitHub org slug (01-01)
- **portaudio@latest** used (no semver tags upstream); resolved to v0.0.0-20260203164431 (01-01)
- **CGo required** for portaudio; build needs gcc + portaudio headers; Nix devShell in Plan 01-03 (01-01)
- **NFR-07 enforced** from day 1 — no analytics/telemetry/tracking in go.mod (01-01)
- **xdg.Reload() in ConfigPath()** — required because adrg/xdg caches dirs in init(); call Reload before ConfigFile to respect runtime env changes (01-02)
- **rootCfg is unexported** — closure injection via PersistentPreRunE; all newXxxCmd() factories accept *config.Config (01-02)
- **WAV chimes generated at 9.5KB each** — ffmpeg 880Hz/660Hz sine at 16kHz mono PCM; embedded via embed.FS (01-02)
- **env.CGO_ENABLED not top-level CGO_ENABLED** — newer nixpkgs raises "overlapping attributes" error; must use env attrset in buildGoModule (01-03)
- **pkgsStatic for musl variant** — compiles portaudio against musl automatically; no manual per-dep linker flags needed (01-03)
- **Static binary gate passed** — 2.64MB, ldd=not-a-dynamic-executable, make build-check PASS (01-03)
- **go-audio/wav v1.1.0 pinned explicitly** — go-audio/riff v1.0.0 pulled as transitive alongside go-audio/audio v1.0.0 (02-01)
- **Wave 0 stubs use t.Skip labeling** — "Wave 0 stub — implement in Plan 0N" pattern enables grep to identify pending vs implemented tests (02-01)
- **package audio (not audio_test) for test files** — encodeWAV and ReadWriteSeeker are unexported; test package must match to access them (02-02)
- **fakeRecorder test double** — implements AudioRecorder inline in test with encodeWAV delegation; no mock framework needed (02-02)
- **Recorder.Stop() is no-op** — context cancellation is the primary stop mechanism for blocking PortAudio streams (02-02)
- **CGo requires gcc-wrapper (not bare gcc) from Nix store** — includes proper C runtime library paths (crt1.o/crti.o) needed by the linker (02-02)
- **defer recover() in PlayChime goroutine** — portaudio C lib panics on headless ALSA systems (index out of range in hostsAndDevices); recover() intercepts Go-visible panics and logs them (02-03)
- **Remove musl from devShell buildInputs** — musl in NIX_LDFLAGS caused musl+glibc mixing in test binaries resulting in SIGSEGV at startup; musl only needed by pkgsStatic (02-03)

### Phase 3-02: IPC Server + CLI Integration

- **NDJSON protocol via json.Encoder.Encode** — automatically appends \n, no custom framing needed (IPC-02)
- **Stale socket auto-removed at startup** — defensive cleanup in NewServer() before net.Listen (IPC-04)
- **CLI timeouts**: 5s for stop/toggle, 1s for status (from CONTEXT.md)
- **stop/status are idempotent** — exit 0 if daemon not running (safe for scripts)

## Performance Metrics

| Phase | Plan | Duration | Tasks | Files |
|-------|------|----------|-------|-------|
| 01-foundation | 01 | 5min | 2 | 12 |
| 01-foundation | 02 | 3min | 2 | 14 |
| 01-foundation | 03 | 8min | 2 | 3 |
| 02-audio-pipeline | 01 | 4min | 1 | 5 |
| 02-audio-pipeline | 02 | 12min | 2 | 4 |
| 02-audio-pipeline | 03 | 6min | 1 | 3 |
| 03-ipc-daemon | 01 | 15min | 3 | 7 |
| 03-ipc-daemon | 02 | 25min | 4 | 9 |

## Config

- Granularity: coarse
- Research: enabled
- Plan check: enabled
- Verifier: enabled
- Model profile: balanced
