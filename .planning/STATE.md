---
gsd_state_version: 1.0
milestone: v0.1
milestone_name: milestone
current_plan: Not started
status: unknown
stopped_at: Completed 05-03-PLAN.md
last_updated: "2026-03-08T22:54:25.406Z"
progress:
  total_phases: 5
  completed_phases: 5
  total_plans: 14
  completed_plans: 14
---

# Project State — yap

## Current Status

**Phase:** 05-polish-distribution (in-progress)
**Current Plan:** Not started
**Next action:** Begin Phase 05-03
**Milestone:** v0.1
**Last session:** 2026-03-08T22:53:29.538Z
**Stopped at:** Completed 05-03-PLAN.md

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
| Phase 3 — IPC + Daemon | complete | All 2/2 plans complete |
| Phase 4 — Input + Output | complete | All 3/3 plans complete |
| Phase 5 — Polish + Distribution | in-progress | 2/3 plans complete (CLI config done, wizard pending) |

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

### Phase 4-01: Hotkey Listener

- **NonBlock() then ReadOne() pattern** — never call Fd() after NonBlock() to satisfy INPUT-04 research finding
- **No exclusive grab** — never call Grab() to satisfy INPUT-03 and allow input sharing with other applications
- **Alpha key range detection** — KEY_A-KEY_Z range identifies full keyboards vs numeric keypads/special devices
- **Permission error format** — exact "usermod -aG input $USER" command in error message to satisfy INPUT-06
- **Repeat event handling** — ignore value=2 (repeat) events in hold-to-talk loop to satisfy INPUT-02
- **Warning chime frequency** — 770Hz between start 880Hz and stop 660Hz for clear audio distinction
- **CGO_ENABLED=0 for tests** — avoid musl/glibc mixing issues in test binaries (documented in Phase 2)

### Phase 5-02: Config Management

- **Atomic save pattern: temp file → sync → rename** — prevents config corruption during writes even on crashes (CONFIG-06)
- **Timeout validation: 1-300s range** — balances usability (minimum 1s) with safety (maximum 5 minutes) (AUDIO-08)
- **Warning chime timing: 10s before timeout with 1s minimum** — prevents negative timing for short timeouts (AUDIO-08)
- **Explicit key validation** — whitelist of valid keys prevents typos and provides clear error messages (CONFIG-07)
- **Closure injection for config subcommands** — following existing pattern for passing *config.Config to subcommands (CONFIG-06)

### Phase 4-02: Transcription + Notifications

- **Explicit context cancellation is NOT retryable** — context.Canceled returns immediately to prevent retry storms on user abort
- **HTTP client timeout errors ARE retryable** — transient network issues should retry (vs explicit user cancellation)
- **Status code classification** — 4xx (client errors) fail immediately, 5xx (server errors) retry with exponential backoff
- **Exponential backoff retry** — 500ms/1s/2s delays with 3 retry limit (4 total attempts: initial + 3 retries)
- **30-second HTTP client timeout** — TRANS-03 requirement enforced via clientTimeout package variable
- **beeep.Notify signature uses `any` for icon** — test capture functions must match exact signature (not `string`)
- **Package-level variables for testability** — apiURL, clientTimeout, notifyFn enable test doubles without mocking frameworks
- **Notifications are best-effort** — notification backend errors logged but not propagated to caller
- **"yap: " title prefix** — all notification titles prefixed for easy identification in desktop environment

### Phase 4-03: Hold-to-Talk Integration

- **Display server detection** — WAYLAND_DISPLAY first (Wayland), then DISPLAY (X11), else error (OUTPUT-01)
- **Wayland paste priority** — wtype FIRST, ydotool second, clipboard-only fallback (CONTEXT.md locked decision)
- **X11 paste with 150ms delay** — time.Sleep(150ms) before xdotool for focus acquisition (OUTPUT-03)
- **X11 --clearmodifiers flag** — ensures layout safety regardless of modifier key state
- **ydotool socket check** — verify $YDOTOOL_SOCKET or /tmp/.ydotool_socket exists before invocation (OUTPUT-04)
- **Clipboard save/restore safety** — ReadAll() before paste, WriteAll(saved) 100ms after confirmed success only (OUTPUT-06, OUTPUT-07)
- **Recording context derived from daemon** — context.WithTimeout(ctx, 60s) ensures SIGTERM cancels active recording (NFR-04)
- **50s warning chime** — non-blocking time.AfterFunc(50s) with assets.WarningChime() (770Hz)
- **60s absolute timeout** — context.WithTimeout auto-stops recording at exactly 60s
- **Package-level dependency injection** — execCommand, clipboardRead/Write, lookPath, osStat, sleep for testability
- **IPC callback injection** — SetToggleFn/SetStatusFn allow daemon to provide state callbacks
- **Hold-to-talk state machine** — mutex-protected recordState with active bool and cancel context.CancelFunc

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
| 04-input-output | 01 | 25min | 2 | 7 |
| 04-input-output | 02 | 10min | 2 | 4 |
| 04-input-output | 03 | 35min | 3 | 5 |
| 05-polish-distribution | 01 | 130 | 2 | 3 |
| 05-polish-distribution | 02 | 4min | 4 | 6 |
| Phase 05 P03 | 2 | 3 tasks | 3 files |

## Config

- Granularity: coarse
- Research: enabled
- Plan check: enabled
- Verifier: enabled
- Model profile: balanced
