# Changelog

All notable changes to yap are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

yap is pre-1.0. Until 1.0, breaking changes can land in any release; the
roadmap (see `ROADMAP.md`) is the source of truth for what is planned.

## [Unreleased]

### Added
- `CHANGELOG.md` (this file).
- Static `install.sh` is now at the repository root so the release workflow
  and the `curl | bash` install URL share a single source of truth.

### Changed
- Rewrote `README.md` against the nested config schema and the
  `yap listen` CLI surface defined in `ARCHITECTURE.md`. Removed the stale
  flat-config examples and corrected the privacy claims so they reflect
  the current Groq-only bootstrap.
- Corrected `ROADMAP.md` Phase 0 entry that claimed `github.com/tadvi/systray`
  was orphan. It is a required transitive dep through `gen2brain/beeep`'s
  Windows-only toast code and must stay in `go.mod`.

### Removed
- `.planning/` directory: legacy phase notes superseded by `ARCHITECTURE.md`
  and `ROADMAP.md`.
- `TODO.md`: its only outstanding item (multi-key hotkey combos) is
  tracked as Phase 10 in `ROADMAP.md`.

## [Phase 1 — Platform Abstraction] — 2026-03-08

Phase 1 of the roadmap landed in commit `770edee`. It established the
platform interfaces, the Linux adapters that satisfy them, and an
explicit `Deps`-injection layout for the daemon. All tests pass with no
behavior change for end users.

### Added
- `internal/platform/platform.go` declares the OS-resource interfaces:
  `Recorder`, `ChimePlayer`, `Hotkey`, `HotkeyConfig`, `Notifier`, `Paster`,
  plus a `KeyCode` type that maps directly onto evdev codes.
- `internal/platform/linux/` contains the full Linux implementation set:
  `audio.go`, `chime.go`, `wav.go`, `hotkey.go`, `paster.go`, `notifier.go`,
  `detect_terminal.go`, and a `NewPlatform()` factory.
- `internal/engine/engine.go` extracts the record→transcribe→paste
  pipeline into a platform-agnostic orchestrator with a `Transcriber`
  interface and a `ChimeSource` type.
- `internal/cli/` (renamed from `internal/cmd/`) wires `linux.NewPlatform()`
  into `daemon.DefaultDeps` and the wizard at the entry point.

### Changed
- `internal/daemon/daemon.go` now takes a `Deps` struct so every external
  collaborator (audio, chime, hotkey, transcription, paste, notifier,
  PID file management) is injected. There are no package-level mutable
  variables anywhere in the daemon.
- `internal/config/wizard.go` accepts a `platform.HotkeyConfig` instead
  of importing the old `internal/hotkey` package directly.

### Removed
- Old packages `internal/audio/`, `internal/hotkey/`, `internal/paste/`,
  and `internal/notify/` are deleted; their responsibilities now live
  in `internal/platform/linux/`.

### Deferred to later phases
- `internal/platform/darwin/` and `internal/platform/windows/` adapters
  remain unimplemented. They land with the macOS work in Phase 13 and
  the Windows work in Phase 14 of `ROADMAP.md`.

### Inherited debt (closed in later phases)
- `internal/transcribe/transcribe.go` still has package-level mutable
  state (`apiURL`, `clientTimeout`, `notifyFn`). It is rewritten in
  Phase 3 when the package moves to `pkg/yap/transcribe/groq/` with
  constructor injection only.
- `internal/platform/linux/paster.go` is a global `wtype → ydotool →
  xdotool` fallback chain with a hard-coded sleep. It is replaced in
  Phase 4 by the app-aware injection module described in
  `ARCHITECTURE.md`.

[Unreleased]: https://github.com/hybridz/yap/compare/770edee...HEAD
