---
phase: 01-foundation
plan: "01"
subsystem: infra
tags: [go, cobra, cli, portaudio, toml, xdg, module-init]

# Dependency graph
requires: []
provides:
  - Go module github.com/hybridz/yap with all Phase 1 direct dependencies declared
  - Cobra CLI binary with start/stop/status/toggle/config subcommand stubs
  - Wave 0 test stubs for config and assets packages (compile-clean, all t.Skip)
  - Makefile with build, build-static, verify-static, build-check, test targets
affects:
  - 01-02 (config package depends on go.mod and cmd scaffold)
  - 01-03 (Nix flake builds on top of this module structure)

# Tech tracking
tech-stack:
  added:
    - github.com/spf13/cobra v1.10.2 (CLI framework)
    - github.com/adrg/xdg v0.5.3 (XDG base dirs)
    - github.com/BurntSushi/toml v1.6.0 (config parsing)
    - github.com/gordonklaus/portaudio v0.0.0-20260203164431-765aa7dfa631 (audio CGo bindings)
  patterns:
    - Cobra subcommand pattern via newXxxCmd() factory functions registered in root.go init()
    - Wave 0 test stubs: t.Skip("Wave 0 stub — implemented in Plan NN") for future TDD compliance
    - Makefile static build pattern: CGO_ENABLED=1 CC=musl-gcc with netgo,osusergo tags

key-files:
  created:
    - go.mod
    - go.sum
    - cmd/yap/main.go
    - internal/cmd/root.go
    - internal/cmd/start.go
    - internal/cmd/stop.go
    - internal/cmd/status.go
    - internal/cmd/toggle.go
    - internal/cmd/config.go
    - internal/config/config_test.go
    - internal/assets/assets_test.go
    - Makefile
  modified: []

key-decisions:
  - "Module path github.com/hybridz/yap matches GitHub organization slug"
  - "portaudio@latest used (v0.0.0-20260203164431) as no tagged release exists upstream"
  - "CGo required for portaudio — build needs gcc + portaudio headers in path"
  - "No analytics, telemetry, crash-reporting, or tracking dependencies (NFR-07 enforced from day 1)"

patterns-established:
  - "Wave 0 stub pattern: all test files created upfront with t.Skip, filled in when implementation lands"
  - "Subcommand factory: newXxxCmd() returns *cobra.Command, registered in root.go init()"

requirements-completed:
  - NFR-07

# Metrics
duration: 5min
completed: 2026-03-08
---

# Phase 1 Plan 01: Go module scaffold with Cobra CLI stubs and Wave 0 test stubs

**Compilable Go module github.com/hybridz/yap with Cobra CLI (5 subcommand stubs), portaudio/toml/xdg/cobra dependencies, and Wave 0 test stubs — `go build ./cmd/yap` succeeds; `./yap --help` lists all subcommands**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-03-08T01:56:09Z
- **Completed:** 2026-03-08T02:01:28Z
- **Tasks:** 2
- **Files modified:** 12 (go.mod, go.sum + 10 source files)

## Accomplishments
- Go module initialized at github.com/hybridz/yap with all 4 Phase 1 direct dependencies declared
- Cobra CLI scaffold with 5 registered subcommands (start, stop, status, toggle, config) all exiting 0
- Wave 0 test stubs in internal/config and internal/assets — `go test ./...` compiles clean with no failures
- Makefile with build, build-static (musl-gcc static), verify-static, and test targets

## Task Commits

Each task was committed atomically:

1. **Task 1: Initialize Go module and declare all dependencies** - `26f4d80` (chore)
2. **Task 2: Create Cobra CLI scaffold with all subcommand stubs and Wave 0 test stubs** - `7f9d2fd` (feat)

**Plan metadata:** (docs commit — see below)

## Files Created/Modified
- `go.mod` - Module definition with cobra v1.10.2, xdg v0.5.3, toml v1.6.0, portaudio latest
- `go.sum` - Populated checksum database
- `cmd/yap/main.go` - Binary entry point: calls cmd.Execute(), os.Exit(1) on error
- `internal/cmd/root.go` - rootCmd + Execute() + AddCommand registrations for all 5 subcommands
- `internal/cmd/start.go` - start subcommand stub (exit 0, TODO Phase 3)
- `internal/cmd/stop.go` - stop subcommand stub (exit 0, TODO Phase 3)
- `internal/cmd/status.go` - status subcommand stub (exit 0, TODO Phase 3)
- `internal/cmd/toggle.go` - toggle subcommand stub (exit 0, TODO Phase 3)
- `internal/cmd/config.go` - config subcommand stub (returns Help(), Phase 5 subcommands TBD)
- `internal/config/config_test.go` - Wave 0 stubs: TestConfigPath/Load/Keys/EnvOverrides/MissingDefaults
- `internal/assets/assets_test.go` - Wave 0 stubs: TestAssetsPresent/Size/ListAssets
- `Makefile` - build, build-static, verify-static, build-check, test targets

## Decisions Made
- Module path `github.com/hybridz/yap` matches GitHub organization slug (single source of truth)
- Used `portaudio@latest` since gordonklaus/portaudio has no semver-tagged releases — resolved to v0.0.0-20260203164431
- CGo is required for portaudio; the build environment must provide gcc + portaudio headers (documented in Makefile static build section)
- No analytics, telemetry, or tracking packages — NFR-07 compliance enforced from module initialization

## Deviations from Plan

None - plan executed exactly as written.

Note: Build requires `nix shell nixpkgs#go nixpkgs#gcc nixpkgs#portaudio` on this NixOS system since Go/gcc are not in default PATH. This is expected behavior for a Nix-managed system; the Nix flake (Plan 01-03) will provide a proper devShell.

## Issues Encountered
- Go and gcc not in default PATH on NixOS — used `nix shell nixpkgs#go nixpkgs#gcc nixpkgs#portaudio` for all build/test commands. This is the expected NixOS pattern and not an issue; Plan 01-03 adds the Nix flake with a proper devShell.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Plan 01-02 can proceed: go.mod exists with all dependencies, config/assets test stubs are in place
- Plan 01-03 can proceed: module structure and CGo build pattern are established
- CGo build note: any plan that does `go build` needs gcc + portaudio headers available

---
*Phase: 01-foundation*
*Completed: 2026-03-08*
