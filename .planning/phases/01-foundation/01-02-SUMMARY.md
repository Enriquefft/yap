---
phase: 01-foundation
plan: "02"
subsystem: config
tags: [xdg, toml, embed, wav, cobra, dependency-injection]

requires:
  - phase: 01-01
    provides: go.mod with BurntSushi/toml, adrg/xdg, spf13/cobra dependencies; stub cmd files

provides:
  - "config.Config struct with 5 fields (api_key, hotkey, language, mic_device, timeout_seconds)"
  - "config.Load() with XDG path resolution, TOML decode, env var overrides, and defaults for missing file"
  - "config.ConfigPath() returning XDG-compliant path (reads env via xdg.Reload)"
  - "assets.FS embed.FS with start.wav and stop.wav embedded at compile time"
  - "assets.StartChime() / StopChime() returning io.Reader for embedded WAV bytes"
  - "assets.ListAssets() for debug/test enumeration"
  - "Cobra closure injection: PersistentPreRunE populates rootCfg; all newXxxCmd() factories accept *config.Config"

affects:
  - 01-03
  - 02-audio
  - 03-ipc
  - 04-input

tech-stack:
  added: []
  patterns:
    - "XDG config path via adrg/xdg with xdg.Reload() call to respect runtime env changes"
    - "Closure injection: PersistentPreRunE populates unexported rootCfg; subcommand factories close over *config.Config"
    - "Missing config file returns safe defaults (no error) — first-run friendly"
    - "Env var overrides applied after TOML decode: GROQ_API_KEY, YAP_HOTKEY"
    - "embed.FS for zero-runtime-IO asset access"

key-files:
  created:
    - internal/config/config.go
    - internal/config/config_test.go
    - internal/config/testhelpers_test.go
    - internal/assets/assets.go
    - internal/assets/assets_test.go
    - internal/assets/start.wav
    - internal/assets/stop.wav
    - internal/cmd/root_test.go
  modified:
    - internal/cmd/root.go
    - internal/cmd/start.go
    - internal/cmd/stop.go
    - internal/cmd/status.go
    - internal/cmd/toggle.go
    - internal/cmd/config.go

key-decisions:
  - "xdg.Reload() called inside ConfigPath() so tests using t.Setenv override the cached baseDirs value (adrg/xdg caches in init)"
  - "rootCfg is unexported package-level var — injection point without exported global mutable state"
  - "WAV files generated via ffmpeg at 16kHz mono PCM (9.5KB each, well under 100KB limit)"

patterns-established:
  - "Config injection: all cmd factories accept *config.Config, no package-level exported config"
  - "Test isolation: t.Setenv + t.TempDir for XDG env; call xdg.Reload before test assertions"

requirements-completed: [CONFIG-01, CONFIG-02, CONFIG-03, CONFIG-04, CONFIG-05, ASSETS-01, ASSETS-02]

duration: 3min
completed: 2026-03-08
---

# Phase 01 Plan 02: Config and Assets Package Summary

**XDG TOML config with env var overrides and cobra closure injection; embed.FS WAV chimes at 9.5KB each**

## Performance

- **Duration:** 3 min
- **Started:** 2026-03-08T02:02:29Z
- **Completed:** 2026-03-08T02:06:20Z
- **Tasks:** 2
- **Files modified:** 14

## Accomplishments

- Config package with XDG path, TOML decode, defaults for missing file, and GROQ_API_KEY/YAP_HOTKEY env overrides
- Cobra closure injection wired in root.go: PersistentPreRunE populates unexported rootCfg, all 5 subcommand factories accept *config.Config
- Assets package with embed.FS containing 880Hz start chime and 660Hz stop chime (9.5KB each, under 100KB limit)
- 11 total tests passing: 6 config, 4 assets, 1 cmd closure injection

## Task Commits

Each task was committed atomically:

1. **Task 1: Config package and closure injection** - `0ee7805` (feat)
2. **Task 2: Assets package with embedded WAV chimes** - `680349e` (feat)

**Plan metadata:** (docs commit follows)

## Files Created/Modified

- `internal/config/config.go` - Config struct, Load(), ConfigPath(), applyEnvOverrides()
- `internal/config/config_test.go` - 6 unit tests replacing Wave 0 stubs
- `internal/config/testhelpers_test.go` - LoadHelper / ConfigPathHelper for test isolation
- `internal/assets/assets.go` - embed.FS, StartChime(), StopChime(), ListAssets()
- `internal/assets/assets_test.go` - 4 unit tests replacing Wave 0 stubs
- `internal/assets/start.wav` - 880Hz 0.3s beep, 16kHz mono PCM, 9.5KB
- `internal/assets/stop.wav` - 660Hz 0.3s beep, 16kHz mono PCM, 9.5KB
- `internal/cmd/root.go` - PersistentPreRunE with closure injection, init() passes &rootCfg
- `internal/cmd/root_test.go` - TestCmdClosureInjection verifying CONFIG-05
- `internal/cmd/start.go` - Updated to accept *config.Config
- `internal/cmd/stop.go` - Updated to accept *config.Config
- `internal/cmd/status.go` - Updated to accept *config.Config
- `internal/cmd/toggle.go` - Updated to accept *config.Config
- `internal/cmd/config.go` - Updated to accept *config.Config

## Decisions Made

- **xdg.Reload() in ConfigPath():** adrg/xdg caches base dirs in package init(). Tests call t.Setenv to change XDG_CONFIG_HOME, but without Reload() the cached value is used. Added xdg.Reload() before xdg.ConfigFile() to re-read current env.
- **rootCfg is unexported:** Keeps the injection pointer private to the cmd package; no external mutation possible.
- **WAV generation via ffmpeg:** Available on the system (NixOS). Generated 0.3s sine waves at 880Hz (start) and 660Hz (stop), 16kHz mono PCM. Both 9.5KB.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Added xdg.Reload() to ConfigPath() to fix env isolation in tests**
- **Found during:** Task 1 (TestConfigLoad failing — returned defaults instead of TOML values)
- **Issue:** adrg/xdg calls initDirs() once in package init(), caching XDG_CONFIG_HOME. Tests set the env via t.Setenv after init() runs, so xdg.ConfigFile() returned the cached (pre-test) path. Config was loaded from the real user config dir (or non-existent path) instead of the test temp dir.
- **Fix:** Added `xdg.Reload()` at the start of ConfigPath() to force re-read of current env vars before resolving the path.
- **Files modified:** internal/config/config.go
- **Verification:** All 6 config tests pass with correct values after fix.
- **Committed in:** `0ee7805` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - bug)
**Impact on plan:** Required for test correctness and runtime correctness when XDG_CONFIG_HOME is set dynamically. No scope creep.

## Issues Encountered

- adrg/xdg init() caching caused TestConfigLoad to silently return defaults. Caught immediately by TDD RED step. Fixed with xdg.Reload().

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Config package ready for use in all subsequent phases via `config.Load()`
- Embedded WAV assets accessible via `assets.StartChime()` / `assets.StopChime()` — ready for Phase 2 audio pipeline
- Closure injection pattern established — Phase 3 daemon can receive populated config via the same *config.Config pointer
- Plan 01-03 can proceed: Nix devShell, static build, and CI setup

## Self-Check: PASSED

All created files verified on disk. All task commits confirmed in git history.

---
*Phase: 01-foundation*
*Completed: 2026-03-08*
