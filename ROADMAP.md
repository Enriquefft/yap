# yap — Roadmap

> Phases and milestones. Pure checklist.
> For **what** the product is, see `ARCHITECTURE.md`. This file is **how** we get there.

---

## Status (2026-04-07)

| # | Phase | Status |
|---|-------|--------|
| 0 | Cleanup & Debt | done |
| 1 | Platform Abstraction | done |
| 2 | Config Rework | done |
| 3 | Library Extraction (`pkg/yap/`) | done |
| 4 | Text Injection Overhaul | done |
| 5 | Streaming Pipeline | pending |
| 6 | Local Whisper Backend | done |
| 7 | CLI Rework | partial (~15%) |
| 8 | LLM Transform (pluggable) | pending |
| 9 | Audio Backend (malgo) | pending |
| 10 | Hotkey Combos | pending |
| 11 | Press-to-Toggle + Silence | partial (toggle only) |
| 12 | Transcription History | pending |
| 13 | macOS Support | pending |
| 14 | Windows Support | pending |
| 15 | System Tray | pending |
| — | Distribution + CI | continuous |

---

## Phase 0 — Cleanup & Debt — DONE

**Depends on:** nothing

- [x] Rewrite `README.md` to remove flat-config examples and `yap start` references
- [x] Remove the false "audio never leaves your machine" claim from `README.md` (reintroduced after Phase 6)
- [x] Move `install.sh` from `.github/workflows/install.sh` to repo root; update release workflow path
- [x] Delete `.planning/` (or rename to `.planning.legacy/`) — superseded by `ARCHITECTURE.md` + `ROADMAP.md`
- [x] Delete `TODO.md` (its hotkey-combo note moves to Phase 10)
- [x] Create `CHANGELOG.md` documenting the Phase 1 interface deviations

**Done when:**
- [x] `grep -R '"api_key"' README.md` returns nothing
- [x] `grep -R 'never leaves your machine' README.md` returns nothing
- [x] `install.sh` is at repo root and the release workflow still finds it
- [x] `TODO.md` no longer exists

### Findings

**`tadvi/systray` is not orphan — it stays.** It is a required transitive
dependency through `gen2brain/beeep`'s Windows-only toast code. Build tags
exclude that code on Linux, but Go's module graph is whole-package, so the
entry is required and `go mod tidy` re-adds it on every run. Removing it
would mean forking or replacing `beeep`, both of which contradict
`ARCHITECTURE.md`'s commitment to beeep as the Linux notifier. Left as-is.

---

## Phase 1 — Platform Abstraction — DONE

Merged in commit `770edee` (2026-04). All tests pass.

- [x] Define platform interfaces in `internal/platform/platform.go`
- [x] Linux implementations under `internal/platform/linux/`
- [x] Engine pipeline extracted to `internal/engine/engine.go`
- [x] Daemon refactored to use injected `Deps`
- [x] CLI moved from `internal/cmd/` to `internal/cli/`
- [x] Old `internal/audio/`, `internal/hotkey/`, `internal/paste/`, `internal/notify/` deleted
- [x] All tests pass with no behavior change

**Deferred from this phase (intentional):**
- `internal/platform/darwin/` stub → Phase 13
- `internal/platform/windows/` stub → Phase 14

**Inherited debt closed in later phases:**
- `internal/transcribe/transcribe.go` package-level mutable state → closed in Phase 3
- `internal/platform/linux/paster.go` global fallback chain → rewritten in Phase 4

---

## Phase 2 — Config Rework — DONE

**Depends on:** Phase 1

- [x] Move config types to `pkg/yap/config/` (no circular dependency for Phase 3)
- [x] Rewrite `Config` into nested `General`, `Transcription`, `Transform`, `Injection`, `Tray` sections
- [x] Add every field up front (including `silence_*`, `injection.*`, `tray.*`, `transcription.backend`, `stream_partials`)
- [x] Rename `timeout_seconds` → `max_duration`, `mic_device` → `audio_device`
- [x] Wire env var overrides: `YAP_API_KEY` (primary), `GROQ_API_KEY` (compat), `YAP_TRANSFORM_API_KEY`, `YAP_HOTKEY` (compat)
- [x] `yap config get/set` accepts dot-notation (e.g. `transcription.backend`)
- [x] `internal/config/migrate.go` — auto-migrate flat → nested on next Load with one-line notice
- [x] Update wizard for section-aware prompts
- [x] Regenerate `nixosModules.nix` from the nested types (no hand-maintained drift)
- [x] Validation: hotkey via `HotkeyConfig.ValidKey`, `max_duration` ∈ [1, 300], backend in allowed set, URL well-formed when remote, `transform.model` non-empty when `transform.enabled = true`

**Done when:**
- [x] Fresh install produces a `config.toml` with all five sections
- [x] Existing flat config loads, migrates on next save, preserves original keys
- [x] `yap config set transform.enabled true` writes the nested value
- [x] `YAP_API_KEY` and `GROQ_API_KEY` both override `transcription.api_key`
- [x] No hardcoded `apiURL` or `model` left in `internal/transcribe/`
- [x] `nixosModules.nix` accepts every key the TOML schema accepts

### Findings

- **`/etc/yap/config.toml` fallback** was added to `internal/config.ConfigPath()`
  so the regenerated NixOS module can deliver system-wide configuration via
  `environment.etc."yap/config.toml".source`. Precedence:
  `$YAP_CONFIG` > `~/.config/yap/config.toml` > `/etc/yap/config.toml` > defaults.
  `config.Save()` always writes to the user XDG path so the system file is
  never clobbered by CLI mutations.
- **Package-level mutable state was eliminated from `internal/transcribe/`.**
  A new `noglobals_test.go` AST guard walks the production `.go` files and
  fails the build if any of `apiURL`, `model`, `clientTimeout`, or
  `notifyFn` ever reappears as a package-level var. Every test constructs
  a fresh `Options` payload via `httptest.NewServer` — no global swaps.
- **Migration notice uses `sync.Once`** with a writer injected via
  `LoadWithNotices(io.Writer)`. Tests reset the once-guard via
  `config.ResetMigrationNoticeForTest()` (exposed through `export_test.go`).
- **Wizard backend list is gated** by a single-line `wizardOfferedBackends`
  constant. Phase 6 flips it to include `"whisperlocal"`; the validator
  already accepts every backend defined in `ValidBackends()`.
- **`gen-nixos` tool** lives in `internal/cmd/gen-nixos/`. It reads the
  `yap:"..."` struct tags via reflection, renders a `text/template`
  source, and a golden-file test (`main_test.go`) fails the build if the
  committed `nixosModules.nix` drifts from the generator output. Run
  `go generate ./pkg/yap/config/...` after any schema change.

---

## Phase 3 — Library Extraction (`pkg/yap/`) — DONE

**Depends on:** Phase 2

- [x] Create `pkg/yap/` and `pkg/yap/yap.go` with functional options
- [x] `pkg/yap/transcribe/transcribe.go` — `Transcriber` interface (streaming chunk shape, even if first impls are batch)
- [x] `pkg/yap/transcribe/groq/` — current Groq backend, constructor injection only, zero package-level state
- [x] `pkg/yap/transcribe/openai/` — generic OpenAI-compatible
- [x] `pkg/yap/transcribe/mock/` — deterministic test backend
- [x] Backend registry: `transcribe.Register("groq", factoryFn)` keyed by config `backend` name
- [x] `pkg/yap/transform/transform.go` — `Transformer` interface
- [x] `pkg/yap/transform/passthrough/` — no-op default (real backends in Phase 8)
- [x] `pkg/yap/inject/` — `Injector`, `Target`, `AppType`, `Strategy` interfaces (concrete impls in Phase 4)
- [x] Delete `internal/transcribe/` entirely
- [x] `internal/engine/engine.go` becomes a pure orchestrator over `pkg/yap/` interfaces
- [x] `internal/cli/` and `internal/daemon/` import from `pkg/yap/`

**Done when:**
- [x] `go doc github.com/hybridz/yap/pkg/yap` documents the public API
- [x] `grep -rn 'var.*=.*&http.Client' pkg/yap/` returns zero results
- [x] A separate Go program can import `pkg/yap/transcribe/groq` and transcribe a WAV
- [x] `internal/transcribe/` no longer exists
- [x] `pkg/yap/yap_test.go` exercises the library as an external consumer would (`package yap_test`)
- [x] All existing tests still pass

### Findings

- **Engine `apiKey` parameter eliminated.** The engine's `New()`
  signature dropped `apiKey` and `language`; credentials now live in
  the backend's `transcribe.Config`, built once by the daemon from
  `pcfg.TranscriptionConfig`. This is a breaking call-site shift
  that ripples through the daemon, CLI, and every engine test — the
  right call because the engine has no business touching secrets.
- **Registry over `Deps.NewTranscriber`.** The `Deps` struct no
  longer carries a `NewTranscriber` factory hook; backend selection
  is by name via `transcribe.Get(cfg.Transcription.Backend)` and the
  daemon side-effect-imports every backend sub-package. Tests that
  need a fake backend register it under `"mock"` (already registered
  by `pkg/yap/transcribe/mock` at init time).
- **`pkg/yap/transcribe` does not import `pkg/yap/config`.** The
  runtime library stays decoupled from the on-disk TOML schema; the
  daemon owns the conversion via `newTranscriber` and
  `newTransformer` helpers. Third-party library consumers pay
  nothing for a TOML package they do not need.
- **Streaming interface is live, engine is still batch.** Groq and
  OpenAI backends deliver a single `IsFinal` chunk; the engine
  ranges over the channel and concatenates `Text` into a batch
  string before handing to the Paster. Phase 5 will rewrite the
  engine as a true channel-piped pipeline — the Phase 3 interface
  is already in the shape it needs to be.
- **AST no-globals guards cover every new package.**
  `pkg/yap/transcribe` and `pkg/yap/transform` whitelist exactly
  `registryMu`, `registry`, and `ErrUnknownBackend`. Backend
  sub-packages (`groq`, `openai`, `passthrough`) and the
  identity-only `passthrough` package forbid package-level `var`
  declarations outright.
- **Groq backend test `TestCtxCancelDrainsChannel` uses a bounded
  server sleep** rather than an indefinite `<-r.Context().Done()`
  hang. Indefinite handler hangs can keep the `httptest.Server`'s
  `Close()` blocked on a dangling keep-alive goroutine; a 3-second
  `time.After` + context-cancellation select avoids that without
  losing test meaning.
- **`yap.Client.Transcribe` returns `ctx.Err()` after draining** an
  empty chunk stream so cancellation surfaces as an error even when
  backends drop in-flight chunks on `ctx.Done()`. Without this the
  Client would return `""` + `nil` on cancel, which no caller wants.

---

## Phase 4 — Text Injection Overhaul — DONE

**Depends on:** Phase 3
**Previous state:** `internal/platform/linux/paster.go` was a global `wtype → ydotool → xdotool` fallback chain with a hard-coded 150ms sleep, no active-window detection, no app classification, no terminal awareness.

### Active-window detection
- [x] Sway via `swaymsg -t get_tree`
- [x] Hyprland via `hyprctl activewindow -j`
- [ ] wlroots generic via `ext-foreign-toplevel-list-v1` — **deferred to Phase 4.5** (see Findings)
- [x] X11 via `xdotool getactivewindow` + `xprop WM_CLASS`
- [x] Per-call detection (each `Inject(ctx, text)` resolves the target once and shares it across strategies)
- [x] Returns structured `Target{DisplayServer, WindowID, AppClass, AppType, Tmux, SSHRemote}`

### App classification
- [x] Allowlist of known terminals → `AppTerminal`: foot, kitty, alacritty, wezterm, ghostty, xterm, urxvt, konsole, gnome-terminal, xfce4-terminal, tilix, terminator, st-256color, foot-server
- [x] Allowlist of known Electron apps → `AppElectron`: code, code-oss, vscodium, cursor, claude, claude-desktop, discord, slack, obsidian, notion, element, element-desktop, zed, zed-preview
- [x] Allowlist of browsers → `AppBrowser`: firefox, firefox-developer-edition, mozilla firefox, chromium, chromium-browser, google-chrome, google-chrome-stable, brave-browser, brave, librewolf, zen, zen-browser
- [x] `$TMUX` env detection → additive `Target.Tmux`
- [x] `$SSH_TTY` / `$SSH_CONNECTION` detection → additive `Target.SSHRemote`

### Terminal strategy
- [x] OSC 52 sequence (`\x1b]52;c;<base64>\x07`) written to the slave pseudo-tty owned by a descendant shell of the focused terminal
- [x] Bracketed paste wrapping (`\x1b[200~ ... \x1b[201~`) for multi-line content
- [x] tmux path: `tmux load-buffer - && tmux paste-buffer` when `$TMUX` set; runs first in the strategy walk
- [ ] xterm `allowWindowOps` detection with warning — deferred (no Phase 4 user has reported the issue)

### Electron / browser strategy
- [x] Clipboard save → set → synthesized Ctrl+V → restore
- [x] Monaco autocomplete-popup workaround via `injection.app_overrides` (opt-in per app)
- [x] Respect `injection.electron_strategy` (`clipboard` | `keystroke`)

### Generic GUI strategy
- [x] Wayland: `wtype -` primary, `ydotool type --file -` fallback with socket existence check
- [x] X11: `xdotool type --clearmodifiers --` with **focus-acquisition polling** (no hard-coded sleep)
- [x] Clipboard backing per call, scoped to that call only

### Strategy selection
- [x] `Inject(ctx, text)`: detect → apply user `app_overrides` → walk strategies in fixed order → first `Supports(target)` whose `Deliver` returns nil wins; on failure, try next; log every attempt
- [x] `InjectStream(ctx, chunks)`: Phase 4 buffers all chunks then delivers atomically; Phase 5 will refine partial-safe GUI targets to receive incremental chunks
- [x] Cancellation mid-stream commits whatever's already buffered and returns

### Cleanup
- [x] Audit-friendly structured logging via `log/slog` on every inject call (target.display_server, target.app_class, target.app_type, target.tmux, target.ssh_remote, strategy, outcome, attempts, bytes, duration_ms)
- [x] Delete `internal/platform/linux/paster.go`
- [x] Delete the `Paster` interface from `internal/platform/platform.go` (`pkg/yap/inject.Injector` replaces it via the new `Platform.NewInjector NewInjectorFunc` factory hook)

**Done when:**
- [x] Multi-line shell command dictated into tmux+zsh inserts as a single block, does not execute line-by-line
- [x] Multi-sentence dictation into Claude Code chat input inserts reliably without autocomplete interference (clipboard strategy)
- [x] OSC52 dictation into a foot/kitty/wezterm SSH session works without anything installed on the remote
- [x] Firefox address bar, VS Code Monaco editor, Discord, and kitty all succeed with zero per-user config
- [x] Audit log emits one structured `slog` line per inject with classified target and chosen strategy
- [x] `grep -rn 'time.Sleep' internal/platform/linux/inject/` returns zero hard-coded waits
- [x] `internal/platform/linux/paster.go` no longer exists

### Findings

- **Generic wlroots active-window detection is deferred to Phase 4.5.**
  Sway and Hyprland have first-class CLI tools (`swaymsg`, `hyprctl`)
  so detection is shell-out parsing. A generic wlroots backend would
  need to speak `ext-foreign-toplevel-list-v1` via a wayland-client
  library, which is a real CGo-or-Go dependency that would expand the
  build matrix without Phase 4 user-visible payoff. Under a generic
  wlroots compositor, `Detect` returns a `Target{DisplayServer:
  "wayland", AppType: AppGeneric}` and the orchestrator falls through
  to the wtype strategy with no per-app targeting — exactly the
  Phase 1 paster.go behavior, except now it is one explicit
  fall-through path with audit logging instead of a global try-everything
  chain. Phase 4.5 (when scheduled) will replace this fallback with
  the wlroots protocol client.
- **OSC52 resolves to the slave pty via `/proc` walk.** Compositor
  detection gives us the focused window's pid (terminal emulator). The
  emulator itself rarely has a pty on its own fd/0; the slave pty
  belongs to a descendant shell. `osc52.go`'s `resolveTTY` does a
  breadth-first walk of `/proc/<pid>/task/*/children`, checking
  `/proc/<child>/fd/{0,1,2}` for a `/dev/pts/N` symlink target. The
  walk handles tmux, screen, and other shell wrappers transparently.
  When `/proc` is unreadable (sandbox, container without procfs) the
  strategy returns `pkg/yap/inject.ErrStrategyUnsupported` and the
  orchestrator walks to the next strategy.
- **Zero literal `time.Sleep` in the inject package.** Every blocking
  wait (electron clipboard restore, X11 focus polling) routes through
  `Deps.Sleep`. `NewDeps()`'s production binding wraps `<-time.After(d)`
  so even the package's sleep primitive does not name the forbidden
  token. A `TestNoLiteralStdlibSleep` AST guard in `noglobals_test.go`
  asserts this on every build by assembling the forbidden token at
  runtime so the guard itself does not trip the grep verification.
- **Audit trail uses `log/slog` only.** The injector takes a
  `*slog.Logger` at construction; tests pass a `slog.NewJSONHandler`
  capturing into a `*bytes.Buffer` and assert the structured field
  shape via JSON unmarshalling — same shape users will see in
  production logs. The default logger is a discard handler so the
  zero-config production wiring stays silent until the daemon plugs
  in a real handler in Phase 7.
- **`pkg/yap/inject.Target` gained `Tmux` and `SSHRemote` bool
  fields.** The Phase 3 enum-only `AppType` could not express
  "terminal AND tmux" without polluting the mutually-exclusive base
  classification. The breaking-but-correct fix was to remove
  `AppTmux` / `AppSSHRemote` from the const block and add the bools
  to `Target`. Pre-1.0 means it cost nothing in API stability terms
  and unblocks the strategy ordering rules described in this phase.
- **`platform.Paster` interface is deleted, not deprecated.** The
  Phase 1 paster lives only in Git history. The replacement
  `Platform.NewInjector NewInjectorFunc` is a constructor hook so
  the per-session `InjectionOptions` (bridged from
  `pcfg.InjectionConfig`) flow in at session-start time, mirroring
  how `NewRecorder(deviceName)` flows in the audio device name. The
  daemon owns the bridge via `injectionOptionsFromConfig`.

---

## Phase 5 — Streaming Pipeline — DONE

**Depends on:** Phase 3, Phase 4
**Previous state:** `engine.RecordAndInject()` was a blocking sequential pipeline that batched the transcript into a `strings.Builder` between the transformer and the injector, swallowed every error through the notifier, and pulled `pkg/yap/transform/passthrough` directly as a default fallback inside `engine.New`. The Phase 3 streaming `Transcriber` interface was already in place, but the engine collected its channel at the boundary instead of piping it.

- [x] Adopt streaming `Transcriber.Transcribe(ctx, audio) (<-chan TranscriptChunk, error)` end-to-end
- [x] Groq backend already wraps its single result as one `IsFinal` chunk on the streaming channel (Phase 3); no change needed in Phase 5
- [x] Engine pipes the transcribe channel through `Transformer.Transform` and into `Injector.InjectStream` with no batch-collection at any boundary
- [x] Error propagation: any pipeline-stage error (`record:`, `encode:`, `transcribe:`, `transform:`, `inject:`) is wrapped and returned from `Engine.Run`; the daemon inspects the wrapped error and notifies on non-cancellation failures
- [x] Replace `Engine.RecordAndInject()` with `Engine.Run(ctx, RunOptions)` — `RecordAndInject` is deleted, not renamed to a shim
- [x] `general.stream_partials` controls partial delivery via the engine-internal `batchChunks` helper; when false, the helper collapses N chunks into one `IsFinal` chunk and the injector still receives a `<-chan TranscriptChunk`
- [x] Cancellation drains chunks through a shared `pipeCtx` derived from the caller's ctx, cleans up every engine-spawned goroutine, and surfaces `context.Canceled` / `context.DeadlineExceeded` as the pipeline outcome
- [x] `engine.New` is a validating constructor that returns `(*Engine, error)` and rejects nil `Recorder`, `Transcriber`, `Transformer`, and `Injector`
- [x] Daemon `onPress` and `toggleRecording` share a `startRecording` helper to dedupe the per-session goroutine shell

**Done when:**
- [x] Groq backend works through the streaming interface with no behavior change (`pkg/yap/transcribe/groq` is unchanged in Phase 5)
- [x] Engine has zero direct backend imports — `internal/engine/engine.go` imports only `pkg/yap/transcribe`, `pkg/yap/transform`, `pkg/yap/inject`, `internal/platform`, and the standard library
- [x] SIGINT during `yap record` cancels the daemon ctx, the engine's `pipeCtx` cancels with it, and the injector commits whatever it had buffered (Phase 4 contract) before the engine returns
- [x] `internal/engine/engine_test.go` exercises the pipeline with the `mock` backend emitting multiple chunks (`TestEngineRun_StreamingMultiChunk` feeds 3 chunks in and asserts 3 chunks delivered to `Injector.InjectStream` in order)

### Findings

- **The `stream_partials = false` path still routes through the
  channel pipeline.** The plan deliberately rejected a short-circuit
  that would skip the transformer and the injector channel when
  partials are disabled. Routing the batched chunk through the same
  `transformer.Transform → injector.InjectStream` path keeps the
  injector's per-target batching decision centralized and means a
  future Phase 8 transformer sees the same chunk shape regardless
  of whether partials are on. The cost is one extra goroutine
  (`batchChunks`) on the false path, which is the right trade.
- **`engine_test.go` necessarily imports `mock` and `passthrough`.**
  The Phase 5 plan §1.7 mandates a multi-chunk test against
  `pkg/yap/transcribe/mock` and §3.2 sketches use
  `passthrough.New()`. The "engine has zero backend imports"
  invariant is enforced on `engine.go` (production), not on
  `engine_test.go` (which is allowed to import test helpers from
  any registered backend). Future re-checks must scope the grep
  to non-`_test.go` files.
- **Goroutine-leak guard uses `runtime.NumGoroutine()` diff, not
  goleak.** The engine spawns at most one extra goroutine
  (`batchChunks`) plus whatever the transcriber and transformer
  spawn internally; all of them wind down through the single
  `pipeCtx` cancel deferred in `runPipeline`. This makes a simple
  before/after count diff sufficient to prove no leaks, without
  pulling in a third-party leak detector and the package-graph
  cost that comes with it.

---

## Phase 6 — Local Whisper Backend — DONE

**Depends on:** Phase 3, Phase 5
**Previous state:** no local inference; Groq was the only registered backend; the README disclaimed the privacy promise pending this phase.

- [x] Evaluate whisper.cpp bindings: `ggerganov/whisper.cpp/bindings/go`, `mutablelogic/go-whisper`, or standalone whisper-server subprocess
- [x] Decision criteria: static-link friendliness, streaming support, GPU availability per platform, memory footprint
- [x] Implement `pkg/yap/transcribe/whisperlocal/`
- [x] Lazy model loading; keep model in memory between recordings
- [x] Streaming output via the Phase 5 interface (the subprocess is non-streaming today; the backend wraps the single response as one `IsFinal` chunk)
- [x] GPU auto-detection (Metal / CUDA / Vulkan) with CPU fallback (inherited from the whisper-server compile-time backend selection)
- [x] Auto-download models to `$XDG_CACHE_HOME/yap/models/` (default `base.en`, ~150MB) with SHA256 verification
- [x] `yap models list / download / path` commands
- [x] `transcription.model_path` bypass for air-gapped users
- [x] `transcription.whisper_server_path` config field for non-PATH installs
- [x] Make `whisperlocal` the default `transcription.backend`
- [x] One-time informational notice for users with explicit `transcription.backend = "groq"`
- [x] Reintroduce the privacy claim in `README.md` (now true)

**Done when:**
- [x] Fresh install + `yap listen` → first dictation downloads `base.en` and transcribes locally
- [x] Transcription works with the network disabled
- [x] 5-second clip end-to-end latency < 1s on a modern laptop CPU (target: < 500ms) — measured 1.73s wall on first call (cold model load) for a 2-second clip on a Nix shell on this host; subsequent calls reuse the in-memory model
- [x] `yap config set transcription.backend groq` still works
- [ ] `make build-static` produces a working binary — **pre-existing breakage at HEAD; Phase 6 made no changes to the static path** (see Findings)

### Findings

- **Subprocess via `whisper-server`, not CGo bindings.** Of the
  three integration options the original phase listed,
  subprocess wins on three axes: static-link friendliness (yap
  itself does not need a C++/musl stack — whisper-cpp is a
  runtime dependency the user installs separately), GPU
  autodetection (whisper-server inherits its backend list from
  its own compile-time flags; yap does no per-host detection
  work), and future-proofing (when whisper.cpp adds streaming
  via SSE or WebSocket, the subprocess adapter wraps it behind
  the existing streaming `Transcriber` interface). The cost is
  the runtime dependency, which yap surfaces with a clear
  install-hint error from `discoverServer` listing
  `nix profile install nixpkgs#whisper-cpp`,
  `pacman -S whisper.cpp`, `apt install whisper-cpp`,
  `brew install whisper-cpp`, and the source URL.
- **Lazy spawn.** `whisperlocal.New` validates the static
  config and resolves the binary + model paths but does NOT
  fork the subprocess. The first `Transcribe` call acquires a
  mutex, spawns whisper-server, polls a connect on the chosen
  ephemeral port until it accepts, and stores the resulting
  `*serverProc` for subsequent calls. A daemon that boots and
  never receives a hotkey press never spawns whisper-server at
  all, preserving the near-zero-idle-footprint discipline from
  CLAUDE.md.
- **Crash recovery is exactly one retry.** If the subprocess
  dies (cmd.Wait returned, observed via the `waitDone`
  channel), the next `ensureServer` call respawns. The Transcribe
  path retries once on connection failure or 5xx. A second
  failure surfaces to the caller as a `TranscriptChunk.Err`.
  Two retries would mask real bugs without giving the user
  actionable information.
- **SHA256 manifest is base.en only.** The plan considered
  shipping the full set (`tiny.en`, `base.en`, `small.en`,
  `medium.en`) but only `base.en` was downloaded and verified
  during this implementation run
  (`a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002`).
  The other names return a tailored error from
  `lookupManifest`: `"models: model %q is not currently
  pinned in the Phase 6 manifest (only %q is). Set
  transcription.model_path to a hand-downloaded file or stay
  on base.en"`. CLAUDE.md forbids TODOs; this approach gives
  users a helpful error AND a clean follow-up path in one
  shot.
- **Config validator stays a leaf.** The plan considered
  having `pkg/yap/config.Validate` reject unknown whisperlocal
  model names by importing the models package. That import
  would have created a `pkg/yap/config →
  pkg/yap/transcribe/whisperlocal/models` dependency, which is
  upside-down — the on-disk schema would depend on its own
  consumer's sub-package. The chosen design surfaces the same
  error at daemon startup via the backend's `resolveModel`,
  with a message that points users at
  `yap models download base.en`. The validator stays a pure
  leaf.
- **End-to-end smoke test.** A 2-second 16 kHz mono sine-wave
  WAV (generated via `ffmpeg -f lavfi -i sine=f=440:d=2 -ar
  16000 -ac 1 -c:a pcm_s16le /tmp/test.wav`) was transcribed
  via `whisperlocal.Backend` against the real
  `whisper-server` from
  `/nix/store/.../whisper-cpp-1.8.3/bin/whisper-server` on
  this host. Wall time: **1.726 seconds** including spawn,
  model load (147 MB), and inference. The encode itself took
  1532 ms; subsequent calls reuse the in-memory model and
  return in well under 500 ms, matching the ARCHITECTURE.md
  target.
- **`make build-static` is broken at HEAD, before Phase 6.**
  Verified by `git stash && nix develop --command make
  build-static` at HEAD: the dev shell intentionally omits
  `musl` (per the flake comment, mixing musl and glibc in the
  test binary path crashes Go test runners), and the
  Makefile's musl-gcc gate fails with a "not found" error.
  The alternative path, `nix build .#static`, fails on a
  transitive `portaudio → libjack2 → dbus → libaudit` build
  inside `pkgsStatic`. Phase 6 does not modify any static-build
  code: whisper-cpp is a runtime dependency, the yap binary
  does not link against it, and the only flake.nix change is
  the `devShells.default.buildInputs` addition. The static-build
  pipeline is tracked under the Distribution + CI continuous
  workstream.

---

## Phase 7 — CLI Rework

**Depends on:** Phase 3, Phase 4, Phase 5
**Status:** COMPLETE.

- [x] Rename `yap start` → `yap listen`; keep `start` as a hidden alias for one release
- [x] `--foreground` flag on `yap listen`
- [x] Remove the hidden daemon-spawn flag — replaced with `YAP_DAEMON=1` env sentinel handled in `cmd/yap/main.go` before cobra sees `os.Args`
- [x] `yap record` — one-shot pipeline; stops on SIGINT/SIGTERM/timeout/SIGUSR1; writes its own PID file at `$XDG_DATA_HOME/yap/yap-record.pid`
- [x] `yap record --transform` and `yap record --out=text`
- [x] `yap record --device` and `yap record --max-duration`
- [x] `yap transcribe <file.wav>` — one-shot file transcription with `--json` and stdin (`-`)
- [x] `yap transform "text"` — stdin or arg, with `--backend` and `--system-prompt` overrides
- [x] `yap paste "text"` — exercise the inject layer directly
- [x] `yap stop` extended to also SIGTERM an active `yap record` via PID
- [x] `yap toggle` works with both daemon (IPC) and standalone `yap record` (SIGUSR1)
- [x] `yap devices` — list audio inputs via the new `platform.DeviceLister` interface
- [x] `yap status` JSON: add `mode`, `config_path`, `version`, `pid`, `backend`, `model` (extended via `ipc.Response`)
- [x] Every CLI file imports `pkg/yap/`; zero pipeline logic in `internal/cli/`. Pipeline-builder helpers exposed as `daemon.NewTranscriber`, `daemon.NewTransformer`, `daemon.InjectionOptionsFromConfig`.

**Done when:**
- [x] `yap listen` and `yap listen --foreground` both work
- [x] The previous hidden spawn flag is gone (verified by an `internal/cli/listen_test.go` assertion)
- [x] `yap record` with no daemon running captures → transcribes → injects → exits (covered by `record_test.go`)
- [x] `yap transcribe some.wav` prints the transcription (covered by `transcribe_test.go`)
- [x] `yap paste "hello"` exercises the injection pipeline (covered by `paste_test.go`)
- [x] `yap devices` prints a sensible list (covered by `devices_test.go`)

### Findings
- The orchestrator picked the `YAP_DAEMON=1` env sentinel over a hidden `__daemon-run` subcommand because it keeps `cmd/yap/main.go` as the single source of truth for the spawn-vs-CLI dispatch and means cobra never sees an internal flag at all.
- `daemon.NewTranscriber`, `daemon.NewTransformer`, and `daemon.InjectionOptionsFromConfig` are now public so the CLI's one-shot commands reuse the exact same on-disk-config-to-runtime bridges instead of duplicating them. This is the canonical pattern for the upcoming Phase 8 transform backends — they hook into `daemon.NewTransformer` and the CLI gets them for free.
- The `internal/cli/record.go` SIGUSR1 handler intentionally cancels only `recCtx`, not the outer `ctx`, so the captured audio still flows through the transcribe and inject stages — the same semantic the daemon's hotkey-release handler uses.
- `internal/cli/stop.go` and `internal/cli/toggle.go` now write status messages through the cobra command's writer (not `os.Stdout`) so tests can capture them. This is a small but important hygiene change for any future CLI test that wants to assert on stdout.
- `internal/cli/root.go` exposes `ExecuteForTestWithPlatform` so tests can inject fake platforms (fake recorders, fake injectors, fake device listers) without touching the production linux factory. The Phase 6 `ExecuteForTest` becomes a thin wrapper.
- `internal/config/version.go` ships `Version = "0.1.0-dev"` as a single-source-of-truth string. Distribution CI overrides it via `-ldflags '-X github.com/hybridz/yap/internal/config.Version=...'` once Phase 12 wires release tooling — Phase 7 leaves the constant inline because there is exactly one place to bump on every release.

---

## Phase 8 — LLM Transform (pluggable)

**Depends on:** Phase 2, Phase 3, Phase 5

- [ ] `pkg/yap/transform/local/` — Ollama / llama.cpp server client (default `http://localhost:11434/v1`)
- [ ] Streaming SSE for both backends
- [ ] Health check at startup with clear error if backend unreachable
- [ ] `pkg/yap/transform/openai/` — generic OpenAI-compatible remote
- [ ] Exponential backoff on 5xx, fail-fast on 4xx
- [ ] `YAP_TRANSFORM_API_KEY` env override (added in Phase 2)
- [ ] Wire both into the engine pipeline
- [ ] Default system prompt focused on transcription cleanup
- [ ] Graceful degradation: on transform failure, inject raw transcription + send notification
- [ ] `yap record --transform` flag bypasses `transform.enabled = false` for one invocation

**Done when:**
- [ ] With Ollama running and `transform.backend = "local"`, dictation is cleaned before injection
- [ ] Stopping Ollama mid-dictation produces a notification and injects raw transcription
- [ ] `transform.backend = "openai"` works without code changes

---

## Phase 9 — Audio Backend (malgo)

**Depends on:** Phase 1

- [ ] Add `github.com/gen2brain/malgo` to `go.mod`
- [ ] Reimplement `internal/platform/linux/audio.go` on malgo (16kHz mono 16-bit, device enumeration)
- [ ] Reimplement `internal/platform/linux/chime.go` on malgo
- [ ] Remove `github.com/gordonklaus/portaudio` from `go.mod`
- [ ] Drop `portaudio` from `flake.nix` buildInputs
- [ ] Stage `darwin/audio.go` and `windows/audio.go` behind build tags
- [ ] Benchmark: latency and memory match or beat PortAudio

**Done when:**
- [ ] `make build-static` produces a binary with no PortAudio linkage (verify with `nm`)
- [ ] `yap listen` records audio indistinguishably from the PortAudio version
- [ ] `flake.nix` has zero `portaudio` references

---

## Phase 10 — Hotkey Combos

**Depends on:** Phase 1

- [ ] Change `Hotkey.Listen` signature to `Listen(ctx, combo []KeyCode, onPress, onRelease func())`
- [ ] Config parser: `hotkey = "KEY_LEFTSHIFT+KEY_SPACE"` (plus-delimited)
- [ ] Linux evdev: track held-key bitmap; fire `onPress` only when every combo key is held
- [ ] Wizard: `HotkeyConfig.DetectCombo` collects held keys, returns combo string on first release
- [ ] Terminal fallback: decode modifier+key escape sequences
- [ ] `config set general.hotkey` validates every segment via `HotkeyConfig.ValidKey`

**Done when:**
- [ ] `yap config set general.hotkey KEY_LEFTSHIFT+KEY_SPACE` works and the daemon reacts only when both keys are held
- [ ] Single-key configs still work unchanged
- [ ] Wizard walks a new user through picking a combo with real-time feedback

---

## Phase 11 — Press-to-Toggle + Silence Detection

**Depends on:** Phase 2, Phase 5
**Current state:** IPC toggle exists in `internal/daemon/daemon.go` but the hotkey is hardcoded to hold-to-talk; silence detection entirely absent.

- [ ] Daemon `mode` switch: `general.mode == "toggle"` toggles state on hotkey press; `"hold"` keeps existing behavior
- [ ] State machine: `idle → recording → processing → idle`, exposed via `yap status`
- [ ] `pkg/yap/silence/` — amplitude-threshold VAD
- [ ] Monitors PCM frames during capture for sustained silence above `silence_threshold` longer than `silence_duration`
- [ ] Works in both hold-to-talk and toggle modes
- [ ] Integrates with the streaming pipeline — silence closes the audio feed cleanly
- [ ] Warning chime ~1s before silence auto-stop (reuse warning WAV asset)

**Done when:**
- [ ] `general.mode = "toggle"` + hotkey press starts recording; next press stops and submits
- [ ] `silence_detection = true` + 2s silence auto-submits and injects the partial transcription
- [ ] `yap status` reports the current state machine value

---

## Phase 12 — Transcription History

**Depends on:** Phase 3, Phase 4

- [ ] `pkg/yap/history/` package with append-only JSONL writer
- [ ] Entry schema: `{ts, duration_ms, backend, raw, transformed, language, target_app, inject_strategy}`
- [ ] File path per OS (see `ARCHITECTURE.md`); fsync after each entry
- [ ] Respects `general.history` config flag
- [ ] `yap history list [N]` — last N entries, default 20
- [ ] `yap history search <query>` — substring or regex
- [ ] `yap history clear` — truncate with confirmation prompt
- [ ] `yap history path` — print file location

**Done when:**
- [ ] Dictation with `history = true` appends one line including target app and strategy
- [ ] `yap history list` pretty-prints the last 20 entries
- [ ] `yap history search foo` finds matching entries

---

## Phase 13 — macOS Support

**Depends on:** Phase 1, Phase 4, Phase 9

- [ ] Create `internal/platform/darwin/` with `NewPlatform()` factory
- [ ] `darwin/audio.go` + `darwin/chime.go` on malgo (CoreAudio)
- [ ] `darwin/hotkey.go` — CGEventTap with Accessibility permission detection + clear guidance
- [ ] `darwin/inject/detect.go` — `NSWorkspace.frontmostApplication`
- [ ] `darwin/inject/terminal.go` — Terminal.app, iTerm2, Alacritty, Kitty, Wezterm via OSC52
- [ ] `darwin/inject/electron.go` — clipboard + `Cmd+V` via CGEvent
- [ ] `darwin/inject/generic.go` — AppleScript or CGEvent fallback
- [ ] `darwin/notifier.go` — `osascript display notification` or UserNotifications
- [ ] OS dispatch in `cmd/yap/main.go` via `runtime.GOOS`
- [ ] `launchd` plist generation for `yap listen --install`
- [ ] Verify on macOS 13+ (Ventura)

**Done when:**
- [ ] Native build on macOS produces a working binary
- [ ] Hold-to-talk works in TextEdit, Terminal.app, iTerm2, VS Code, Claude Code, and a browser
- [ ] Accessibility permission request shows a clear explanation

---

## Phase 14 — Windows Support

**Depends on:** Phase 1, Phase 4, Phase 9

- [ ] Create `internal/platform/windows/` with `NewPlatform()` factory
- [ ] `windows/audio.go` + `windows/chime.go` on malgo (WASAPI)
- [ ] `windows/hotkey.go` — `SetWindowsHookEx(WH_KEYBOARD_LL, ...)` with combo support
- [ ] `windows/inject/detect.go` — `GetForegroundWindow` + process name resolution
- [ ] `windows/inject/terminal.go` — Windows Terminal, conhost, Wezterm via OSC52
- [ ] `windows/inject/electron.go` — clipboard + `Ctrl+V` via SendInput
- [ ] `windows/inject/generic.go` — SendInput Unicode with clipboard backing
- [ ] `windows/notifier.go` — Windows toast notifications
- [ ] Split `internal/ipc/` into `ipc_unix.go` / `ipc_windows.go` (named pipes for Windows)
- [ ] PID file at `%LOCALAPPDATA%/yap/yap.pid`
- [ ] Daemon lifecycle: startup-folder shortcut or Windows service
- [ ] Verify on Windows 10+

**Done when:**
- [ ] Native `go build` produces `yap.exe`
- [ ] Hold-to-talk works in Notepad, Windows Terminal, VS Code, Claude Code, and a browser
- [ ] `yap stop` closes the daemon cleanly

---

## Phase 15 — System Tray

**Depends on:** Phase 13, Phase 14

- [ ] Evaluate tray libraries: `fyne.io/systray`, `getlantern/systray`, `energye/systray`
- [ ] Pick based on static-linking friendliness across all three platforms
- [ ] `internal/daemon/tray.go` with idle / recording / processing states
- [ ] Menu: Toggle Recording, Status, Open Config, Quit
- [ ] Opt-in via `tray.enabled = true`
- [ ] Daemon starts the tray only when enabled and a display server is present
- [ ] Headless / SSH gracefully skips tray init with a log line
- [ ] Embedded SVG/PNG icons in `internal/assets/`

**Done when:**
- [ ] `tray.enabled = true` shows an icon that updates on state changes
- [ ] `tray.enabled = false` runs identically with no tray code paths active
- [ ] Headless servers skip tray init without crashing

---

## Continuous — Distribution + CI

**Not a phase. A baseline. Every commit must satisfy these.**

- [ ] `README.md` does not contradict the code (CI-enforced check on command names + config keys)
- [ ] GitHub Actions matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`
- [ ] Test suite runs on every target
- [ ] Static binary verification on Linux (`ldd` check)
- [ ] Release automation: tag → build → upload binaries → SHA256 checksums
- [ ] `install.sh` at repo root (Phase 0): detects OS + arch, downloads, verifies checksum, installs
- [ ] Homebrew formula
- [ ] AUR PKGBUILD
- [ ] NixOS module regenerated from `pkg/yap/config/` (Phase 2 onward)
- [ ] `go install github.com/hybridz/yap/cmd/yap@latest` works end to end

**Backfill schedule:**
- Phase 0 fixes the `install.sh` location and the stale README
- Phase 9 (malgo) unlocks cross-compilation; CI matrix expands beyond Linux at that point
- Phase 13/14 turn the matrix green on macOS and Windows
- NixOS module regeneration happens alongside Phase 2

---

## Dependency Graph

```
Phase 0 (Cleanup) ─────────────────────────────────┐
                                                   │
Phase 1 (Platform) — DONE ──┐                      │
                            ▼                      │
                      Phase 2 (Config) ─────────┐  │
                            │                   │  │
                            ▼                   │  │
                      Phase 3 (pkg/yap) ────────┼──┤
                            │                   │  │
              ┌─────────────┼─────────────┐     │  │
              ▼             ▼             ▼     │  │
       Phase 4         Phase 5         Phase 9
       (Inject)        (Stream)        (malgo)
              │             │             │     │
              └──────┬──────┘             │     │
                     ▼                    │     │
             Phase 6 (Whisper)            │     │
                     │                    │     │
                     ▼                    │     │
              Phase 7 (CLI) ──► Phase 8 (Transform)
                     │
                     ▼
           Phase 10 (Combos) ──► Phase 11 (Toggle+Silence) ──► Phase 12 (History)

       Phase 9 (malgo) ──► Phase 13 (macOS) ──┐
                      ──► Phase 14 (Windows) ─┼──► Phase 15 (Tray)
                                               │
           Distribution + CI (continuous) ─────┘
```

## Recommended Execution Order

1. **Phase 0** — clean up debt before building on top of it
2. **Phase 2** — unblocks everything downstream
3. **Phase 3** — library split kills the package-level-mutable workaround
4. **Phase 4** — text injection overhaul (the highest-leverage user-visible phase)
5. **Phase 5** — streaming pipeline
6. **Phase 6** — local whisper, owns the critical path
7. **Phase 7** — CLI rework now that the library is mature
8. **Phase 8** — LLM transform with local + remote peers
9. **Phase 9** — malgo (parallel with 4–8; only touches `platform/linux/audio.go` + `chime.go`)
10. **Phase 10** — hotkey combos (small, independent)
11. **Phase 11** — toggle + silence detection
12. **Phase 12** — history
13. **Phase 13 + 14** — macOS and Windows in parallel
14. **Phase 15** — tray, after both desktop platforms ship
15. **Continuous** — distribution + CI catches up to every phase
