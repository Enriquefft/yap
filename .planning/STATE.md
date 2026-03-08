# Project State — yap

## Current Status

**Phase:** Pre-execution (initialization complete)
**Next action:** `/gsd:plan-phase 1`
**Milestone:** v0.1

## Initialization Summary

- Project initialized: 2026-03-07
- Research completed: STACK, FEATURES, ARCHITECTURE, PITFALLS, SUMMARY
- Requirements defined: REQUIREMENTS.md (5 phases, 60 requirements)
- Roadmap created: ROADMAP.md (5 phases)

## Phase Status

| Phase | Status | Notes |
|-------|--------|-------|
| Phase 1 — Foundation | pending | CGo static build + Nix + config + scaffold |
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

## Config

- Granularity: coarse
- Research: enabled
- Plan check: enabled
- Verifier: enabled
- Model profile: balanced
