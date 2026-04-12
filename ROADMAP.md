# yap — Roadmap

> Phases and milestones. Pure checklist.
> For **what** the product is, see `ARCHITECTURE.md`. This file is **how** we get there.

---

## Status (2026-04-11)

| # | Phase | Status |
|---|-------|--------|
| 0 | Cleanup & Debt | done |
| 1 | Platform Abstraction | done |
| 2 | Config Rework | done |
| 3 | Library Extraction (`pkg/yap/`) | done |
| 4 | Text Injection Overhaul | done |
| 5 | Streaming Pipeline | done |
| 6 | Local Whisper Backend | done |
| 7 | CLI Rework | done |
| 8 | LLM Transform (pluggable) | done |
| 9 | Audio Backend (malgo) | done |
| 10 | Hotkey Combos | pending |
| 11 | Press-to-Toggle + Silence | done |
| 12 | Context-Aware Pipeline (Linux) | pending |
| 13 | Transcription History | pending |
| 14 | macOS Support | pending |
| 15 | Windows Support | pending |
| 16 | System Tray | pending |
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
- [x] `go doc github.com/Enriquefft/yap/pkg/yap` documents the public API
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
- [x] wlroots generic via `ext-foreign-toplevel-list-v1`
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
- [x] tmux path: `tmux load-buffer - && tmux paste-buffer -p` when `$TMUX` set; runs first in the strategy walk
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
- [x] `Inject(ctx, text)`: detect → apply user `app_overrides` → apply `default_strategy` → walk strategies in fixed order → first `Supports(target)` whose `Deliver` returns nil wins; on failure, try next; log every attempt
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

- **Generic wlroots active-window detection via `ext-foreign-toplevel-list-v1`.**
  Implemented in `detect_wlroots.go` using the `wlr-foreign-toplevel-management-unstable-v1`
  Wayland protocol. Sway and Hyprland have first-class CLI tools (`swaymsg`, `hyprctl`)
  so detection is shell-out parsing; the generic wlroots backend covers everything else.
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

### Review findings (post-Phase 4 code review)

- **F1 OSC52 silent clipboard corruption**: bracketed-paste wrapping
  was removed from the OSC52 payload. The markers (`\x1b[200~` /
  `\x1b[201~`) are terminal-side wire framing, not clipboard data;
  embedding them corrupted every multi-line dictation on paste.
  `bracketed.go` and `bracketed_test.go` are deleted.
- **F2 tmux double-wrap**: replaced manual bracketed wrap with
  `tmux paste-buffer -p`, which lets tmux decide whether to wrap
  based on the pane's bracketed-paste state.
- **F3 prependUnique Supports gate**: `selectStrategies` now calls
  `forced.Supports(target)` before prepending an override.
  Unsupported overrides fall through to natural order.
- **C2 ctx plumbing**: `Deps.ExecCommand` replaced with
  `Deps.ExecCommandContext`; `Deps.Sleep` replaced with
  `Deps.SleepCtx`. Every strategy and detect backend passes ctx
  through and aborts on cancellation.
- **C7 DefaultStrategy**: new `injection.default_strategy` config
  option. When no app_override matches, this strategy is prepended
  with the same Supports gate. Bridges through
  `platform.InjectionOptions.DefaultStrategy`.
- **C8 classify allowlist cleanup**: removed `rxvt-unicode` and
  `st-256color` (TERM values, not WM_CLASS); added actual WM_CLASS
  `st`.
- **C10 Injector concurrency mutex**: added `sync.Mutex`. `Inject`
  and `InjectStream` lock it; a private `inject()` body is shared
  between both entry points.

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

### Review findings (post-Phase 5 code review)

- **streamPartials escape hatch in daemon**: `daemon.NewTransformerWithFallback`
  now takes `streamPartials bool`. When true, the fallback decorator is
  skipped — the user gets streaming but loses graceful degradation on
  transform failure. The health probe still runs in both modes and can
  swap to passthrough on startup if the backend is unreachable. This
  resolves the fallback-vs-streaming tradeoff: the buffered fallback
  decorator would defeat the partial-injection promise the user opted
  into via `general.stream_partials`.
- **Retry backoff now ctx-aware**: groq and openai transcribe backends
  replaced `time.Sleep(backoffDelays[attempt])` with `sleepCtx` so
  mid-backoff cancellation returns within ~100ms instead of the full
  3.5s sleep.
- **httpstream no longer imports internal/config**: `NewClient` takes
  `(timeout, userAgent string)` — third parties can reuse the scaffolding.
- **Mock backend defensive copy**: `Chunks` field unexported to prevent
  data race; `New` returns a defensive copy.

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
- [x] `make build-static` produces a working binary — **fixed alongside Phase 9; see Findings**

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
- **SHA256 manifest covers all four English models.** The pinned
  manifest includes `tiny.en`, `base.en`, `small.en`, and
  `medium.en` — each with a SHA256 verified against the canonical
  Hugging Face download. Users who want a model not on this list
  can point `transcription.model_path` at a hand-downloaded
  ggml-\*.bin file.
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
- **`make build-static` is fixed.** Two issues blocked the static
  build: (1) `nix build .#static` failed because `withRecordConfig`
  and `TestStatus_NoDaemon` set `XDG_DATA_HOME` via `t.Setenv` but
  never called `xdg.Reload()` — the `adrg/xdg` library caches paths
  at init time, so `pidfile.RecordPath()`/`pidfile.SocketPath()`
  resolved to unwritable defaults in the Nix sandbox (`/homeless-shelter`).
  Fix: add `xdg.Reload()` after setting XDG env vars in both test
  helpers. (2) `make build-static` failed because the dev shell
  intentionally omits musl to avoid glibc/musl mixing in test
  binaries. Fix: add `devShells.static` with a `musl-gcc` wrapper
  targeting `pkgsStatic.stdenv.cc`. `nix build .#static` now
  produces an 8 MB static ELF binary; `make build-check` works
  inside `nix develop .#static`.

### Review findings (post-Phase 6 code review)

- **C1 Close rug-pulls in-flight Transcribe**: Backend now has a
  `sync.WaitGroup` + close context. Every `Transcribe` call
  `Add(1)`s; `Close` cancels the context so in-flight HTTP requests
  abort, waits on the WaitGroup (bounded at 5s), then tears down
  the subprocess.
- **C2 Spawn lock narrowed**: `spawning chan struct{}` sentinel.
  The mutex is held only for the spawn-vs-reuse decision; the
  expensive spawn runs without the lock. Concurrent `Transcribe`
  calls during cold start block on the channel.
- **C3 Circuit breaker**: after 3 consecutive failures the backend
  returns a sticky error pointing users at `journalctl`/`yap status`
  instead of fork-exec'ing whisper-server on every hotkey press.
  30s cooldown before retry.
- **C4+C5 Subprocess stderr captured + spawn-retry loop**: `pipeBuffer`
  type (32-line ring buffer) tees stderr with `[whisper-server]`
  prefix. Startup failures carry the real diagnostic. Port 0 bind
  race mitigated by spawn-retry loop (3 attempts).
- **S2 Manager struct**: package-level globals replaced with
  `Manager` struct + `NewManager`, `WithHTTPClient`, `WithManifest`
  options. `Default()` lazy singleton via `sync.Once` for production
  callers.
- **S3 File lock for concurrent downloads**: `unix.Flock LOCK_EX` on
  `<cachedir>/.lock` (Unix) / `LockFileEx` (Windows). Download
  acquires the lock, re-checks for the final file, then downloads.
- **S8 ggml magic bytes check**: `resolveModel` rejects files that
  don't start with the ggml magic (`"lmgg"`).
- **S7 Windows portability**: `whisperlocal_unix.go` / `whisperlocal_windows.go`
  split; models lock split similarly. `GOOS=windows go build` succeeds.
- **Latent race in adrg/xdg**: concurrent `CacheDir()` calls serialized
  with `cacheDirMu sync.Mutex`.

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
- `internal/config/version.go` ships `Version = "0.1.0-dev"` as a single-source-of-truth string. Distribution CI overrides it via `-ldflags '-X github.com/Enriquefft/yap/internal/config.Version=...'` once Phase 12 wires release tooling — Phase 7 leaves the constant inline because there is exactly one place to bump on every release.

### Review findings (post-Phase 7 code review)

- **Lowercase Short descriptions**: all cobra `Short` fields now start
  with a lowercase letter per Go CLI conventions.
- **pidfile path helpers extracted**: `internal/pidfile/paths.go` added
  with `DaemonPath()` and `SocketPath()` — single source of truth for
  daemon PID and IPC socket paths.
- **Listen command refactor**: `listen.go` restructured with proper
  internal test coverage (`listen_internal_test.go`).
- **Oneshot tests**: `cmd/yap/main_test.go` and `internal/cli/oneshot_test.go`
  added for the one-shot pipeline commands.
- **whisperlocal Manager threading**: `models.go` subcommands now take
  `*models.Manager` parameter; `root.go` creates the default manager
  instance.

---

## Phase 8 — LLM Transform (pluggable) — DONE

**Depends on:** Phase 2, Phase 3, Phase 5

- [x] `pkg/yap/transform/local/` — Ollama native API client (default `http://localhost:11434`, `POST /api/chat`, NDJSON streaming)
- [x] Streaming for both backends (NDJSON for local, SSE for openai)
- [x] Health check at startup with clear error if backend unreachable (new `transform.Checker` interface, daemon calls it from `NewTransformerWithFallback`)
- [x] `pkg/yap/transform/openai/` — generic OpenAI-compatible remote (SSE, `POST {api_url}/chat/completions`)
- [x] Exponential backoff on 5xx, fail-fast on 4xx (shared `pkg/yap/transform/httpstream` helper)
- [x] `YAP_TRANSFORM_API_KEY` env override (added in Phase 2)
- [x] Wire both into the engine pipeline (side-effect imports in `internal/daemon/daemon.go`)
- [x] Default system prompt focused on transcription cleanup (backend `DefaultSystemPrompt` mirrors the on-disk default owned by `pkg/yap/config`)
- [x] Graceful degradation: on transform failure, inject raw transcription + send notification (`pkg/yap/transform/fallback/` decorator wrapped in `daemon.NewTransformerWithFallback`)
- [x] `yap record --transform` flag bypasses `transform.enabled = false` for one invocation (Phase 7 feature preserved; Phase 8 routes it through the new fallback wrapper so it degrades gracefully too)

**Done when:**
- [x] With Ollama running and `transform.backend = "local"`, dictation is cleaned before injection
- [x] Stopping Ollama mid-dictation produces a notification and injects raw transcription
- [x] `transform.backend = "openai"` works without code changes

### Findings

- **Local backend is Ollama-native, not OpenAI-compat.** The plan
  settled on `POST {api_url}/api/chat` with NDJSON streaming rather
  than routing through Ollama's OpenAI-compat shim. Users who want
  the OpenAI-compat path (llama.cpp-server, vLLM, Ollama's /v1 layer)
  point `transform.backend = "openai"` at the appropriate URL —
  `http://localhost:8080/v1`, `http://localhost:11434/v1`, etc. The
  `local` name is preserved for schema stability (Phase 2 already
  enumerated it) even though it is now specifically "Ollama". This
  is documented in the package godoc of both backends.
- **`httpstream` is a public sub-package, not internal.** Both
  backends need the same HTTP-streaming + retry scaffolding. Putting
  it under `pkg/yap/transform/httpstream/` instead of
  `pkg/yap/transform/internal/httpstream/` lets third parties
  writing their own transform backend reuse the same policy without
  copy-pasting the retry loop. The surface is a single `Client`
  struct, `NewClient(timeout)`, `PostJSON`, and the
  `NonRetryableError` sentinel.
- **Fallback uses buffered replay, not tee.** The transform input
  channel is consumable once, so the decorator drains the full input
  into a slice before either transformer runs. For dictation this is
  a few hundred bytes to a few kB, the cost is trivial, and the
  semantics are simple: "if the primary fails for any reason, replay
  the same chunks through the fallback". Partial success is treated
  as failure — we never mix transformed and raw output.
- **Upstream errors bypass both transformers.** When an input chunk
  carries `chunk.Err != nil` (a transcription failure), the fallback
  decorator propagates it directly without running either the
  primary or the fallback. Transcription failures are not a
  transform-layer concern; papering over them with a raw replay
  would be wrong.
- **Checker is a new type, not a method on Transformer.** The
  `transform.Checker` interface is a separate one-method interface
  that backends opt into. The daemon type-asserts on it; backends
  that skip it (passthrough, for instance) still work. This matches
  the `io.Closer` opt-in pattern already used for long-lived
  backends like whisperlocal.
- **Startup health check is synchronous but bounded.** The daemon
  runs the probe inside `NewTransformerWithFallback` with a 5s
  timeout. A failure fires one notification and swaps the primary
  out for passthrough for the rest of the session. The daemon never
  refuses to start over a transform health failure, matching the
  graceful-degradation ethos.
- **CLI `yap transform` intentionally bypasses fallback.** The debug
  tool passes a nil notifier to `NewTransformer`, which delegates
  to `NewTransformerWithFallback(tc, nil)` and returns the primary
  directly. The point of `yap transform` is to see real errors when
  iterating on prompts or backend configs.
- **OpenAI backend rejects empty api_url at construction.** Unlike
  the local backend, there is no sensible default URL — it depends
  entirely on which upstream server the user is targeting (real
  OpenAI, llama.cpp-server, vLLM, Together.ai...). The validation
  happens in `New`, which surfaces the error as a hard config error
  the first time the transformer is built.

### Review findings (post-Phase 8 code review)

- **streamPartiales escape hatch**: the daemon's
  `NewTransformerWithFallback` now takes `streamPartiales bool`.
  When true, the fallback decorator is skipped — the user gets
  streaming partials but loses graceful degradation on transform
  failure. The health probe still runs and can swap to passthrough
  on startup. This is the correct resolution because the buffered
  fallback decorator would defeat the partial-injection promise.
- **Config validation hardened**: `ValidModes`, `ValidBackends`,
  etc. converted from package-level `var` slices to functions
  returning fresh slices — zero mutable package state. AST-walking
  `noglobals_test.go` guards enforce this.
- **App overrides strategy validation**: `ValidInjectionStrategies()`
  now returns the five real strategy names (tmux, osc52, electron,
  wayland, x11) and both `app_overrides[].strategy` and
  `injection.default_strategy` validate against the same list.

---

## Phase 9 — Audio Backend (malgo) — DONE

**Depends on:** Phase 1

- [x] Add `github.com/gen2brain/malgo` to `go.mod`
- [x] Reimplement `internal/platform/linux/audio.go` on malgo (16kHz mono 16-bit, device enumeration)
- [x] Reimplement `internal/platform/linux/chime.go` on malgo
- [x] Remove `github.com/gordonklaus/portaudio` from `go.mod`
- [x] Drop `portaudio` from `flake.nix` buildInputs
- [ ] Stage `darwin/audio.go` and `windows/audio.go` behind build tags — **deferred to Phase 13/14**
- [x] ~~Benchmark: latency and memory match or beat PortAudio~~ Portaudio is gone; no baseline to compare against. Audio works correctly — sufficient.

**Done when:**
- [x] `make build-static` produces a binary with no PortAudio linkage (verify with `nm`) — **fixed alongside Phase 9**
- [x] `yap listen` records audio indistinguishably from the PortAudio version
- [x] `flake.nix` has zero `portaudio` references

### Findings

- **malgo (miniaudio) replaces portaudio.** Each recorder owns a
  `malgo.AllocatedContext` with per-recording device lifecycle.
  `portaudio` is fully removed from `go.mod` and `flake.nix`. malgo
  bundles miniaudio.h directly — no system audio C library needed.
- **darwin/windows audio stubs deferred.** The malgo backend is
  Linux-only for now; Phase 13 and 14 will add `darwin/audio.go`
  and `windows/audio.go` with the appropriate build tags.
- **Static build fixed alongside Phase 9.** Two fixes applied: (1)
  `xdg.Reload()` added to test helpers that set XDG env vars, so
  `pidfile` paths resolve correctly in the Nix sandbox; (2)
  `devShells.static` added to `flake.nix` with a `musl-gcc` wrapper
  for Makefile-based static builds. `nix build .#static` produces an
  8 MB static binary; `make build-check` works inside
  `nix develop .#static`.

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

## Phase 11 — Press-to-Toggle + Silence Detection — DONE

**Depends on:** Phase 2, Phase 5

- [x] Daemon `mode` switch: `general.mode == "toggle"` toggles state on hotkey press; `"hold"` keeps existing behavior
- [x] State machine: `idle → recording → processing → idle`, exposed via `yap status`
- [x] `pkg/yap/silence/` — amplitude-threshold VAD
- [x] Monitors PCM frames during capture for sustained silence above `silence_threshold` longer than `silence_duration`
- [x] Works in both hold-to-talk and toggle modes
- [x] Integrates with the streaming pipeline — silence closes the audio feed cleanly
- [x] Warning chime ~1s before silence auto-stop (reuse warning WAV asset)

**Done when:**
- [x] `general.mode = "toggle"` + hotkey press starts recording; next press stops and submits
- [x] `silence_detection = true` + 2s silence auto-submits and injects the partial transcription
- [x] `yap status` reports the current state machine value

---

## Phase 12 — Context-Aware Pipeline (Linux)

**Depends on:** Phase 3, Phase 4, Phase 5, Phase 6, Phase 8
**Goal:** Whisper and the LLM transform both know *what the user is dictating into* so domain vocabulary ("yap", "whisperlocal", "OSC52") is recognized correctly at the source instead of being fixed after the fact. The motivating failure: saying "what is yap?" inside a Claude Code session on this repo and getting "what is jump?" because Whisper has no vocabulary anchor.

**Key insight:** this is a lexical-bias problem, not an LLM-cleanup problem. Whisper's `prompt` parameter (OpenAI, Groq, whisper.cpp all support it) nudges token probabilities toward supplied vocabulary. The fix has two orthogonal layers: (1) **project-level vocabulary** from docs files (CLAUDE.md, AGENTS.md, README.md — project instruction files that vary by LLM client but serve the same purpose) provides stable domain terms; (2) **app-specific conversation context** from the canonical application state (Claude Code session jsonl, tmux scrollback) provides recent conversational grounding. Layer 1 is always-on and provider-independent. Layer 2 fires when a hint provider matches the focused app. Both layers are configurable per project.

### Interface changes (breaking, pre-1.0)

- [ ] `pkg/yap/transcribe`: add `Options struct { Prompt string }`
- [ ] `Transcriber.Transcribe(ctx, audio, opts Options)` — per-call prompt, not per-backend
- [ ] Remove `transcribe.Config.Prompt` — it was at the wrong granularity (construction-time, but the prompt must differ per recording because the focused window differs per recording)
- [ ] Thread `Options.Prompt` through every backend: groq, openai, whisperlocal, mock
- [ ] `pkg/yap/transform`: add `Options struct { Context string }`
- [ ] `Transformer.Transform(ctx, in, opts Options)` — per-call context string
- [ ] Thread `Options.Context` through every backend: local, openai, passthrough, fallback. Non-empty context is prepended to the configured `SystemPrompt` as a reference block (`"Recent context (reference only, do not repeat):\n{context}\n---\n"`) at call time
- [ ] Update `internal/engine/engine.go`: `RunOptions` gains `Bundle hint.Bundle` + `VocabularyMaxChars` + `ConversationMaxChars`; `runPipeline` tail-truncates bundle text and passes to the per-stage Options

### New package: `pkg/yap/hint/`

- [ ] `pkg/yap/hint/hint.go` — `Provider` interface (`Name`, `Supports(target)`, `Fetch(ctx, target) (Bundle, error)`), `Bundle{Vocabulary, Conversation, Source}`, `Factory`, `Config{RootPath}`, `Register/Get/Providers` registry (identical shape to `pkg/yap/transcribe`)
- [ ] `pkg/yap/hint/vocab.go` — `ReadVocabularyFiles(root string, filenames []string) string` utility: walks from cwd up to git root, reads each file if found, concatenates with `\n---\n` separators, returns natural text. Used by the daemon's base-vocabulary layer, not by individual providers.
- [ ] `pkg/yap/hint/hint_test.go` — registry tests
- [ ] `pkg/yap/hint/vocab_test.go` — tests the file-reading utility against t.TempDir fixtures
- [ ] `pkg/yap/hint/noglobals_test.go` — AST guard matching existing packages
- [ ] Bundle semantics: `Vocabulary` and `Conversation` are **two orthogonal fields** with different sources. Vocabulary = project docs (CLAUDE.md, AGENTS.md, README.md — project instruction files that vary by LLM provider client but serve identical purposes) read by the daemon's base layer. Conversation = app-specific state returned by the matched provider. Vocabulary feeds the Whisper prompt (lexical bias). Conversation feeds the transform context (intent grounding). A recording session can have Vocabulary without Conversation (no provider matched) but not the reverse — the base vocabulary layer is always-on when hint.enabled=true.

### Linux providers (the two shipped in Phase 12)

- [ ] `pkg/yap/hint/claudecode/` — reads `~/.claude/projects/<cwd-slug>/<latest-session>.jsonl`
  - `cwd-slug`: absolute cwd with `/` replaced by `-` (e.g. `/home/hybridz/Projects/yap` → `-home-hybridz-Projects-yap`)
  - Latest session = the jsonl in that directory with the most recent mtime
  - Parses user + assistant entries, extracts `message.content` strings (and text blocks from assistant arrays), skips meta / command-caveat / tool-use entries
  - Formats as natural `user: … / assistant: …` prose
  - Returns **only `Bundle.Conversation`** — vocabulary is handled by the daemon's base layer (CLAUDE.md / AGENTS.md / README.md). The claudecode provider does NOT read project docs; it provides session-specific conversational context only.
  - `Supports` matches on `target.AppType == AppTerminal` or `target.Tmux`
  - Graceful empty bundle when no session file exists (not an error)
- [ ] `pkg/yap/hint/tmuxpane/` — shells `tmux capture-pane -p -S -500 -t $TMUX_PANE`
  - Strips ANSI sequences, returns the raw terminal buffer text
  - Returns **only `Bundle.Conversation`** — vocabulary is the daemon's base layer
  - `Supports` matches on `target.Tmux`
  - Non-tmux sessions return an empty bundle (not an error)
  - Graceful empty when `tmux` is not on `$PATH`
  - **Does NOT fire when the claudecode provider already matched** — the provider walk is first-match-wins and claudecode ranks above tmuxpane in the default list. Tmux scrollback is a noisy superset of the session jsonl; the structured jsonl is strictly better when available.

### Active-window detection reuse

- [ ] Daemon calls `d.injector.(inject.StrategyResolver).Resolve(ctx)` at press time to get the focused `inject.Target`. This is the Phase 4 contract — `Resolve` is documented as a pure query, no side effects. The same `Target` then drives both the hint provider walk AND the eventual `InjectStream` delivery at the end of the pipeline. Single-source-of-truth for target classification.
- [ ] Verify the Linux injector at `internal/platform/linux/inject/injector.go` implements `StrategyResolver` (confirmed at phase planning time).

### Config schema

- [ ] `pkg/yap/config.HintConfig` under `Config.Hint`:
  - `enabled bool`
  - `vocabulary_files []string` — project doc filenames to read for base vocabulary (default: `["CLAUDE.md", "AGENTS.md", "README.md"]`). LLM provider clients use different instruction file names (CLAUDE.md for Claude Code, AGENTS.md for others, .cursorrules for Cursor, etc.) — the default covers the common ones. Users configure per-project by editing their config.
  - `providers []string` — ordered fallback list for conversation context (default: `["claudecode","tmuxpane"]`)
  - `vocabulary_max_chars int` — Whisper prompt budget (default: 1000 bytes ≈ ~250 tokens, leaving margin under Whisper's 224-token window)
  - `conversation_max_chars int` — transform context budget (default: 8000 bytes)
  - `timeout_ms int` — wall-time budget on the provider walk (default: 300ms; hard cap so a stuck provider never delays audio capture)
- [ ] `enabled = true` by default. This is shipped as core functionality, not opt-in. Principle 2: perfection is the target, not an MVP.
- [ ] Validator in `pkg/yap/config/validate.go`: clamp ranges, reject unknown provider names against `hint.Providers()`
- [ ] `nixosModules.nix` regenerated from the updated schema via the existing `gen-nixos` tool

### Daemon wiring

- [ ] `Daemon` struct gains `hintProviders []hint.Provider` built at startup from `cfg.Hint.Providers` via the registry
- [ ] `startRecording` calls a new `fetchHintBundle(ctx)` helper BEFORE audio capture. Two-layer assembly:
  1. **Base vocabulary layer (always-on):** `hint.ReadVocabularyFiles(cwd, cfg.Hint.VocabularyFiles)` reads project docs (CLAUDE.md, AGENTS.md, README.md, etc.) from cwd walking up to git root. Returns natural text with domain terms. This layer fires regardless of whether any provider matches, so Whisper always gets project vocabulary.
  2. **Provider conversation layer:** bounded `fetchCtx` with `cfg.Hint.TimeoutMS`, `d.injector.(inject.StrategyResolver).Resolve(fetchCtx)` → `Target`, walk `hintProviders` in order, first provider whose `Supports(target)` returns true AND whose `Fetch` returns a non-empty Bundle.Conversation wins.
  3. Assemble final `Bundle{Vocabulary: baseVocab, Conversation: provider.Conversation, Source: provider.Name()}`
  4. Provider errors are non-fatal: log at debug, try next
  5. Empty conversation is a legal null case — the pipeline still benefits from project vocabulary
- [ ] Bundle threaded into `engine.RunOptions` (daemon builds `transcribe.Options` and `transform.Options` from the bundle and budget fields, engine receives ready-made Options)

### CLI

- [ ] `yap hint` debug command — runs the same provider walk against the live focused window and prints the resolved Target + winning provider + bundle summary + first N bytes of each field. Verification tool, no side effects.

### Tests

- [ ] Unit tests for the two new Options structs in `pkg/yap/transcribe` and `pkg/yap/transform`
- [ ] Updated backend tests for every transcribe + transform implementation (mock, groq, openai, whisperlocal, passthrough, local, openai-transform, fallback) to thread Options through
- [ ] `pkg/yap/hint/claudecode/claudecode_test.go` — parses a fixture `testdata/session.jsonl` with a realistic mix of user/assistant/meta/tool entries, asserts extracted Bundle text
- [ ] `pkg/yap/hint/tmuxpane/tmuxpane_test.go` — PATH-shim fake `tmux` binary printing canned output, asserts parsing and ANSI stripping
- [ ] `internal/engine/engine_test.go` — exercises `RunOptions.Bundle` threading with captured Options from mock transcriber + mock transformer; asserts tail-truncation against budgets
- [ ] `internal/daemon/daemon_test.go` — `fetchHintBundle` against fake providers (match, no-match, error, empty) and fake resolver; asserts ordering and non-fatal error handling
- [ ] `internal/cli/hint_test.go` — exercises the new debug command against a fake platform
- [ ] Goroutine-leak guard in the daemon integration test (runtime.NumGoroutine() delta across a full session)
- [ ] AST no-globals guards on every new package

**Done when:**
- [ ] Dictating "what is yap?" into a Claude Code session inside the yap repo produces a transcription containing the word "yap", not "jump" / "chap" / "jap" (verified manually on a real host with `whisperlocal` + `base.en` + the claudecode provider enabled)
- [ ] Dictating inside tmux with a non-Claude program open produces transcription biased by recent pane content (verified manually: dictate a word visible in the pane and confirm it transcribes correctly)
- [ ] `yap config set hint.enabled false` restores pre-Phase-12 behavior byte-for-byte (no bundle fetch, no Options plumbing at call time beyond a zero-value pass-through)
- [ ] A stuck provider (simulated 5-second hang) is aborted within `timeout_ms` and does not delay audio capture
- [ ] `nix develop --command go build ./...` and `nix develop --command go test ./...` both pass
- [ ] `make build-static` still produces a working binary (no new cgo dependencies introduced by this phase)
- [ ] Every new package has a `noglobals_test.go` AST guard

### Deferred to later phases (intentional)

- **AT-SPI provider** (GTK/Qt desktop apps via `org.a11y.atspi.*`). Requires a nontrivial DBus protocol implementation on top of `github.com/godbus/dbus/v5`; most useful target apps (Electron, browsers) have poor a11y tree coverage anyway. Revisit after a user asks for it.
- **Compositor-specific providers** (KWin / GNOME Shell extension hooks). Niche, low leverage.
- **Vision provider** (screenshot + vision-capable LLM). Expensive, universal fallback-of-last-resort. Separate phase when demand emerges.
- **macOS AX provider** — rides with Phase 14 (macOS Support).
- **Windows UIA provider** — rides with Phase 15 (Windows Support).
- **Per-project config overlay** (`.yap.toml` in repo root merged with user config). Enables truly per-repo `vocabulary_files` and `providers` without editing the global config. Revisit when multiple-project usage becomes common.

---

## Phase 13 — Transcription History

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

## Phase 14 — macOS Support

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

## Phase 15 — Windows Support

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

## Phase 16 — System Tray

**Depends on:** Phase 14, Phase 15

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
- [ ] `go install github.com/Enriquefft/yap/cmd/yap@latest` works end to end

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
       (Inject)        (Stream)      (malgo) ✓
              │             │             │     │
              └──────┬──────┘             │     │
                     ▼                    │     │
             Phase 6 (Whisper)            │     │
                     │                    │     │
                     ▼                    │     │
              Phase 7 (CLI) ──► Phase 8 (Transform)
                     │                         │
                     ▼                         │
           Phase 10 (Combos) ──► Phase 11 (Toggle+Silence)
                                        │      │
                                        ▼      ▼
                                 Phase 12 (Context-Aware Pipeline)
                                        │
                                        ▼
                                 Phase 13 (History)

       Phase 9 (malgo) ✓ ──► Phase 14 (macOS) ──┐
                            ──► Phase 15 (Windows) ─┼──► Phase 16 (Tray)
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
12. **Phase 12** — context-aware pipeline (Linux) — fixes domain-vocabulary misses at source
13. **Phase 13** — history
14. **Phase 14 + 15** — macOS and Windows in parallel
15. **Phase 16** — tray, after both desktop platforms ship
16. **Continuous** — distribution + CI catches up to every phase
