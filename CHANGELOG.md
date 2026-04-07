# Changelog

All notable changes to yap are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

yap is pre-1.0. Until 1.0, breaking changes can land in any release; the
roadmap (see `ROADMAP.md`) is the source of truth for what is planned.

## [Unreleased]

### Phase 4 — Text Injection Overhaul

#### Added
- `internal/platform/linux/inject/` is the deep module that owns
  app-aware text injection on Linux. It detects the active window via
  Sway (`swaymsg -t get_tree`), Hyprland (`hyprctl activewindow -j`),
  or X11 (`xdotool getactivewindow` + `xprop`); classifies the focused
  app against the terminal / Electron / browser allowlists in
  `classify.go`; layers additive `Tmux` and `SSHRemote` bits onto the
  Target from the live environment; and walks a fixed-priority
  strategy list (tmux → osc52 → electron → wayland → x11) until one
  delivers the text.
- `osc52.go` writes `\x1b]52;c;<base64>\x07` directly to the slave
  pseudo-terminal owned by a descendant shell of the focused
  terminal emulator. The strategy walks `/proc/<pid>/task/*/children`
  to find the first descendant whose stdin/stdout/stderr is a
  `/dev/pts/N` and writes there. When `/proc` is unreadable or no
  descendant pts is found, OSC52 returns `ErrStrategyUnsupported`
  and the orchestrator falls through cleanly. This is what makes
  dictation into an SSH-attached terminal work without anything
  installed on the remote.
- `tmux.go` pipes payload bytes into `tmux load-buffer -` over
  stdin and then runs `tmux paste-buffer`, so multi-line shell
  commands dictated inside tmux insert as a single block instead of
  executing line-by-line. Bracketed paste wrapping is applied when
  `injection.bracketed_paste = true` and the payload contains a
  newline.
- `electron.go` saves the clipboard, writes the text, synthesizes
  Ctrl+V via wtype/xdotool, and restores the saved value after a
  bounded wait. The wait is the only sleep in the inject package and
  it routes through `Deps.Sleep` — there are no literal `time.Sleep`
  call sites anywhere under `internal/platform/linux/inject/`.
- `wayland.go` types text directly via `wtype -` (preferred) or
  `ydotool type --file -` (fallback when the ydotool socket is
  present). `x11.go` types text via `xdotool type --clearmodifiers --`
  with focus polling: it polls `xdotool getactivewindow` every 10ms
  until two consecutive samples report the same window, then issues
  the type command. The polling cap is 10 iterations (100ms total)
  and proceeds even when focus never settles, so the strategy never
  hangs on a flaky compositor.
- `injector.go` is the orchestrator. Every `Inject(ctx, text)` call
  emits exactly one structured `slog` audit line on completion with
  `target.display_server`, `target.app_class`, `target.app_type`,
  `target.tmux`, `target.ssh_remote`, `strategy`, `outcome`, `bytes`,
  `duration_ms`, and `attempts`; per-attempt failures emit a
  `WARN`-level `inject attempt failed` line each, while
  `ErrStrategyUnsupported` fall-throughs are demoted to `DEBUG`.
- `internal/platform/linux/inject/noglobals_test.go` is the
  package's structural guard. It allows exactly the three classifier
  allowlists, the bracketed-paste byte constants, the
  `electronRestoreDelay` / `focusPoll*` tuning constants, and the
  `ErrNoDisplay` sentinel — anything else at package scope fails the
  build. A second guard scans every production file for the literal
  stdlib blocking-sleep token and fails when one is found.
- `internal/platform/InjectionOptions` and
  `internal/platform/AppOverride` are the new structural bridge
  between the on-disk `pcfg.InjectionConfig` and the runtime injector.
  The platform package deliberately does not import `pkg/yap/config`,
  mirroring the transcribe / transform separation already in place.
- `pkg/yap/inject.Target` gains `Tmux bool` and `SSHRemote bool`
  fields. These additive modifiers were previously enum members
  (`AppTmux`, `AppSSHRemote`) which made expressing "terminal AND
  tmux" awkward. The bools live alongside the AppType enum and never
  collide with the mutually-exclusive base classification.
- `pkg/yap/inject.AppType.String()` returns the stable lowercase
  identifier (`generic`, `terminal`, `electron`, `browser`) used in
  the audit log fields and in `injection.app_overrides` lookups.
- `pkg/yap/inject.ErrStrategyUnsupported` is the public sentinel a
  Strategy returns from `Deliver` to signal "this concrete target is
  not mine — try the next one". The orchestrator falls through
  silently on this sentinel and surfaces it as a `DEBUG` log line
  rather than a real failure.

#### Changed
- `internal/engine/engine.go` now depends on
  `pkg/yap/inject.Injector` instead of the deleted
  `internal/platform.Paster`. The old `RecordAndPaste` method is
  renamed to `RecordAndInject` to reflect the deeper guarantees the
  new module provides. Engine constructors and every test were
  updated together.
- `internal/daemon/daemon.go` now bridges `pcfg.Injection` into
  `platform.InjectionOptions` via a new
  `injectionOptionsFromConfig` helper, then constructs the
  per-session injector by calling
  `deps.Platform.NewInjector(opts)`. The bridge is structurally 1:1
  and is guarded by `internal/daemon/daemon_test.go`.
- `internal/platform/platform.go`'s `Platform` struct now exposes a
  `NewInjector NewInjectorFunc` field instead of the deleted
  `Paster` field. The Linux factory in
  `internal/platform/linux/platform.go` registers the new
  `inject.New` constructor against this hook.

#### Removed
- `internal/platform/linux/paster.go` and
  `internal/platform/linux/paster_test.go` are deleted. The old
  global `wtype → ydotool → xdotool` Ctrl+Shift+V chain (with its
  hard-coded 150ms sleep) was the canonical example of "fallback
  everything and hope" — it is replaced by the explicit, audited,
  per-target strategy walk in
  `internal/platform/linux/inject/`.
- `internal/platform.Paster` is deleted from
  `internal/platform/platform.go`. Any future re-introduction
  would be detected by `noglobals_test.go` because the Linux
  package no longer references the symbol anywhere.
- `pkg/yap/inject.AppTmux` and `pkg/yap/inject.AppSSHRemote` are
  removed from the `AppType` const block. Both are now bool fields
  on `Target`.

#### Findings
- **wlroots-generic compositor support is deferred to Phase 4.5.**
  Sway and Hyprland are wired up through their CLI tools; a generic
  wlroots backend would need to speak `ext-foreign-toplevel-list-v1`
  via a wayland-client library, which is out of scope for Phase 4.
  Under a generic wlroots compositor the orchestrator falls through
  to the wayland strategy with no `AppClass` and the wtype path
  delivers the text without per-app targeting. Documented in
  `internal/platform/linux/inject/detect.go`'s `Detect` doc comment
  and tracked in `ROADMAP.md`.
- **Zero literal `time.Sleep` calls inside the package.** The
  electron strategy's bounded clipboard-restore wait routes through
  `Deps.Sleep`, the X11 focus polling loop calls `Deps.Sleep` in
  `Deps`-land, and `NewDeps()` itself binds `Sleep` to a wrapper
  using `<-time.After(d)` so the production source files do not
  contain the forbidden literal token even in comments. The
  `TestNoLiteralStdlibSleep` guard in `noglobals_test.go` enforces
  this on every build.
- **Audit trail uses Go 1.25 `log/slog` exclusively.**
  `slog.Logger` is constructor-injected; tests pass a JSON capture
  handler to assert the field shape; production wires
  `slog.New(discardHandler{})` by default and the daemon will plug
  in a real handler in Phase 7's CLI rework.

### Phase 3 — Library Extraction (`pkg/yap/`)

#### Added
- `pkg/yap/` is now the public library surface for yap's primitives.
  Third-party Go programs can import `github.com/hybridz/yap/pkg/yap`
  and drive transcription end-to-end without touching the daemon or
  the CLI. The top-level `yap.Client` type wraps a `Transcriber` and
  a `Transformer` behind a functional-options API (`WithTranscriber`,
  `WithTransformer`) and exposes both a batch `Transcribe` and a
  streaming `TranscribeStream` entry point.
- `pkg/yap/transcribe` declares the stable `Transcriber` interface.
  It emits chunks on a `<-chan TranscriptChunk`, so batch backends
  wrap their single result as one `IsFinal` chunk and streaming
  backends (landing in Phase 5/6) can emit incrementally without
  breaking the contract. The package ships a `Config` struct, a
  `Factory` type, a sentinel `ErrUnknownBackend` error, and a
  `Register`/`Get`/`Backends` registry so backends self-register in
  their own `init()` functions.
- `pkg/yap/transcribe/groq` ports the former `internal/transcribe`
  Groq client behind the new `Backend` type with constructor
  injection only — zero package-level var state. Retry semantics,
  multipart form shape, and APIError behavior are preserved exactly.
- `pkg/yap/transcribe/openai` provides a generic OpenAI-compatible
  backend for any server that speaks `/v1/audio/transcriptions`
  (vLLM, llama.cpp server, litellm, Fireworks, OpenAI itself).
- `pkg/yap/transcribe/mock` provides a deterministic test backend
  that drains the supplied audio reader and emits a caller-configurable
  chunk sequence on the channel.
- `pkg/yap/transform` declares the `Transformer` interface, the
  transform-specific `Config` type, and a registry identical in shape
  to the transcribe package's. `pkg/yap/transform/passthrough` is
  the default identity transformer and is always available in the
  registry so the engine can run with the transform stage disabled.
- `pkg/yap/inject` declares the `Injector`, `Target`, `AppType`, and
  `Strategy` types that Phase 4 will implement. The interfaces
  unblock Phase 4 without wiring any concrete strategy in Phase 3.
- AST-level no-globals guards cover every new production file.
  `pkg/yap/transcribe` and `pkg/yap/transform` allow exactly
  `registryMu`, `registry`, and `ErrUnknownBackend` with documented
  rationale; all other packages forbid package-level `var`
  declarations outright.
- `pkg/yap/yap_test.go` is an external-package (`package yap_test`)
  integration test that stands up a fake Groq server, builds a
  backend through the public API, wraps it in a `yap.Client`, and
  verifies `client.Transcribe` returns the expected text. It is the
  proof-of-consumability demanded by ROADMAP Phase 3 "Done when".

#### Changed
- `internal/engine/engine.go` no longer defines its own local
  `Transcriber` interface. The engine imports
  `pkg/yap/transcribe.Transcriber` directly and routes the chunk
  channel through a `pkg/yap/transform.Transformer` (defaulting to
  passthrough when nil). The engine constructor no longer takes an
  `apiKey` — credentials are owned by the backend and injected at
  backend-construction time. This is a breaking call-site shift
  that ripples through every test and the daemon.
- `internal/daemon/daemon.go` now looks transcribers and
  transformers up by name via `transcribe.Get`/`transform.Get` and
  bridges the on-disk `pcfg.TranscriptionConfig` /
  `pcfg.TransformConfig` into the runtime `transcribe.Config` /
  `transform.Config` structs. The `Deps.NewTranscriber` field and
  the `transcribeAdapter` helper are gone; backends are wired
  purely through the registry. The daemon imports every backend
  sub-package for its side-effect registration
  (`_ "github.com/hybridz/yap/pkg/yap/transcribe/groq"`, etc.).

#### Removed
- `internal/transcribe/` is deleted in its entirety. The Groq
  client, its test suite, and its AST no-globals guard all live
  under `pkg/yap/transcribe/groq/` now, ported to the streaming
  channel API. The import path
  `github.com/hybridz/yap/internal/transcribe` no longer exists and
  must not be re-introduced.
- `engine.Transcriber` (the former local interface), the
  `transcribeAdapter` bridge in the daemon, and the
  `Deps.NewTranscriber` injection hook are all gone — the registry
  is now the single source of truth for backend selection.

### Phase 0 — Cleanup & Debt

#### Added
- `CHANGELOG.md` (this file).
- Static `install.sh` is now at the repository root so the release workflow
  and the `curl | bash` install URL share a single source of truth.

#### Changed
- Rewrote `README.md` against the nested config schema and the
  `yap listen` CLI surface defined in `ARCHITECTURE.md`. Removed the stale
  flat-config examples and corrected the privacy claims so they reflect
  the current Groq-only bootstrap.
- Corrected `ROADMAP.md` Phase 0 entry that claimed `github.com/tadvi/systray`
  was orphan. It is a required transitive dep through `gen2brain/beeep`'s
  Windows-only toast code and must stay in `go.mod`.

#### Removed
- `.planning/` directory: legacy phase notes superseded by `ARCHITECTURE.md`
  and `ROADMAP.md`.
- `TODO.md`: its only outstanding item (multi-key hotkey combos) is
  tracked as Phase 10 in `ROADMAP.md`.

### Phase 2 — Config Rework

#### Added
- `pkg/yap/config/` is the single source of truth for the configuration
  schema, validation, environment-override rules, and dot-notation Get/Set
  walkers. Every downstream surface (daemon, CLI, wizard, NixOS module)
  derives from this one package.
- `internal/config/migrate.go` transparently loads pre-Phase-2 flat TOML
  files and maps the legacy fields (`api_key`, `hotkey`, `language`,
  `mic_device`, `timeout_seconds`) into their nested homes. A one-line
  deprecation notice prints at most once per process; the on-disk file is
  left untouched until the next `yap config set` or wizard save.
- `YAP_API_KEY` is the primary transcription API key override;
  `GROQ_API_KEY` is the legacy alias consulted only when `YAP_API_KEY` is
  unset. `YAP_TRANSFORM_API_KEY` populates `transform.api_key`.
  `YAP_HOTKEY` overrides `general.hotkey`. `YAP_CONFIG` selects an
  alternate config file path (used by tests and alternate profiles).
- `yap config get` and `yap config set` accept dot-notation paths over
  the nested schema, e.g. `yap config set transform.enabled true`,
  `yap config get general.hotkey`, `yap config get
  injection.app_overrides.0.match`.
- `yap config overrides list|add|remove|clear` manages
  `injection.app_overrides` entries without exposing users to
  slice-index dot-notation writes.
- First-run wizard now walks sections (`[transcription]`, `[general]`)
  and writes a nested TOML file. Offered transcription backend is
  gated by a one-line `wizardOfferedBackends` constant so Phase 6 can
  add `whisperlocal` by flipping a single literal. The validator
  already accepts every backend.
- `internal/config/ConfigPath()` falls back to `/etc/yap/config.toml`
  when the user XDG file is absent, so NixOS installs can deliver a
  system-managed config via `environment.etc."yap/config.toml".source`.
  Precedence: `$YAP_CONFIG` > user XDG file > `/etc/yap/config.toml`
  > default. `Save()` always writes to the user path.
- `nixosModules.nix` is now generated from the `pkg/yap/config` struct
  tags. The `internal/cmd/gen-nixos` tool reads `yap:"..."` metadata
  via reflection, renders the module via `text/template`, and is
  protected by a golden-file drift guard in
  `internal/cmd/gen-nixos/main_test.go`. Regenerate with
  `go generate ./pkg/yap/config/...`.
- `services.yap.settings.<section>.<field>` NixOS options cover every
  leaf in the schema, with enum types, default values, and
  descriptions derived from the Go struct tags.

#### Changed
- `Config` is now a nested struct with `General`, `Transcription`,
  `Transform`, `Injection`, and `Tray` sections. The legacy flat field
  names are gone from production code; they survive only inside
  `internal/config/migrate.go` for migration.
- `timeout_seconds` has been renamed to `general.max_duration` and
  `mic_device` to `general.audio_device`. Legacy files still load
  via the Phase 2 migration path.
- `internal/transcribe/transcribe.go` no longer has any package-level
  mutable state. `Transcribe` takes an explicit `Options{APIURL, Model,
  Timeout, Client}` struct so every knob is constructor-injected. A
  new `internal/transcribe/noglobals_test.go` AST guard fails the
  build if `apiURL`, `model`, `clientTimeout`, or `notifyFn` ever
  reappears as a package-level var.
- Wizard output now includes `[transcription]` and `[general]` section
  headers before their prompts. Hotkey manual entry validates every
  segment of a plus-delimited combo.
- `internal/cli/root.go` builds a fresh command tree per invocation
  via `newRootCmd(platform)`. Tests use the new `ExecuteForTest`
  helper with writer-injected stdout/stderr.

#### Removed
- Every hand-maintained reference to the flat config schema in
  `internal/config/`, `internal/cli/`, `internal/daemon/`,
  `internal/engine/`, and `internal/transcribe/`. The nested schema in
  `pkg/yap/config` is the only source of truth.

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
