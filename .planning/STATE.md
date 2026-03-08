# Project State — yap

## Current Status

**Phase:** 01-foundation
**Current Plan:** 3/3 in Phase 01-foundation
**Next action:** Execute 01-03-PLAN.md
**Milestone:** v0.1
**Last session:** 2026-03-08T02:06:20Z
**Stopped at:** Completed 01-foundation/01-02-PLAN.md

## Initialization Summary

- Project initialized: 2026-03-07
- Research completed: STACK, FEATURES, ARCHITECTURE, PITFALLS, SUMMARY
- Requirements defined: REQUIREMENTS.md (5 phases, 60 requirements)
- Roadmap created: ROADMAP.md (5 phases)

## Phase Status

| Phase | Status | Notes |
|-------|--------|-------|
| Phase 1 — Foundation | in-progress | 2/3 plans complete |
| Phase 2 — Audio Pipeline | pending | PortAudio + WAV + chimes |
| Phase 3 — IPC + Daemon | pending | Unix socket + daemon lifecycle |
| Phase 4 — Input + Output | pending | evdev + Groq + paste fallback |
| Phase 5 — Polish + Distribution | pending | Wizard + curl install + NixOS module |

## Key Decisions

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

## Performance Metrics

| Phase | Plan | Duration | Tasks | Files |
|-------|------|----------|-------|-------|
| 01-foundation | 01 | 5min | 2 | 12 |
| 01-foundation | 02 | 3min | 2 | 14 |

## Config

- Granularity: coarse
- Research: enabled
- Plan check: enabled
- Verifier: enabled
- Model profile: balanced
