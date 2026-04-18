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
| 12 | Context-Aware Pipeline (Linux) | done |
| 13 | Audio Preprocessing | done |
| 14 | Transcription History | pending |
| 15 | macOS Support | pending |
| 16 | Windows Support | pending |
| 17 | System Tray | pending |
| 18 | Exec Output Mode | done |
| 19 | Evaluation Framework | pending |
| 19.5 | Privacy & Redaction | pending |
| 20 | Accuracy — Objective Wins (20a/20b/20c) | pending |
| 21 | Accuracy — Testable Levers | pending |
| 22 | Locale + Domain Packs | pending |
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
- `internal/platform/darwin/` stub → Phase 15
- `internal/platform/windows/` stub → Phase 16

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
- [ ] Stage `darwin/audio.go` and `windows/audio.go` behind build tags — **deferred to Phase 15/16**
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
  Linux-only for now; Phase 15 and 16 will add `darwin/audio.go`
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

## Phase 12 — Context-Aware Pipeline (Linux) — DONE

**Depends on:** Phase 3, Phase 4, Phase 5, Phase 6, Phase 8
**Goal:** Whisper and the LLM transform both know *what the user is dictating into* so domain vocabulary ("yap", "whisperlocal", "OSC52") is recognized correctly at the source instead of being fixed after the fact. The motivating failure: saying "what is yap?" inside a Claude Code session on this repo and getting "what is jump?" because Whisper has no vocabulary anchor.

**Key insight:** this is a lexical-bias problem, not an LLM-cleanup problem. Whisper's `prompt` parameter (OpenAI, Groq, whisper.cpp all support it) nudges token probabilities toward supplied vocabulary. The fix has two orthogonal layers: (1) **project-level vocabulary** from docs files (CLAUDE.md, AGENTS.md, README.md — project instruction files that vary by LLM client but serve the same purpose) provides stable domain terms; (2) **app-specific conversation context** from the canonical application state (Claude Code session jsonl, tmux scrollback) provides recent conversational grounding. Layer 1 is always-on and provider-independent. Layer 2 fires when a hint provider matches the focused app. Both layers are configurable per project.

### Interface changes (breaking, pre-1.0)

- [x] `pkg/yap/transcribe`: add `Options struct { Prompt string }`
- [x] `Transcriber.Transcribe(ctx, audio, opts Options)` — per-call prompt, not per-backend
- [x] Remove `transcribe.Config.Prompt` — it was at the wrong granularity (construction-time, but the prompt must differ per recording because the focused window differs per recording)
- [x] Thread `Options.Prompt` through every backend: groq, openai, whisperlocal, mock
- [x] `pkg/yap/transform`: add `Options struct { Context string }`
- [x] `Transformer.Transform(ctx, in, opts Options)` — per-call context string
- [x] Thread `Options.Context` through every backend: local, openai, passthrough, fallback. Non-empty context is prepended to the configured `SystemPrompt` as a reference block (`"Recent context (reference only, do not repeat):\n{context}\n---\n"`) at call time
- [x] Update `internal/engine/engine.go`: `RunOptions` gains `TranscribeOpts` and `TransformOpts`; daemon builds these from the hint bundle and budget fields

### New package: `pkg/yap/hint/`

- [x] `pkg/yap/hint/hint.go` — `Provider` interface (`Name`, `Supports(target)`, `Fetch(ctx, target) (Bundle, error)`), `Bundle{Vocabulary, Conversation, Source}`, `Factory`, `Config{RootPath}`, `Register/Get/Providers` registry (identical shape to `pkg/yap/transcribe`)
- [x] `pkg/yap/hint/vocab.go` — `ReadVocabularyFiles(root string, filenames []string) string` utility: walks from cwd up to git root, reads each file if found, concatenates with `\n---\n` separators, returns natural text. Used by the daemon's base-vocabulary layer, not by individual providers.
- [x] `pkg/yap/hint/project.go` — `.yap.toml` per-project overrides with `vocabulary_terms` support
- [x] `pkg/yap/hint/trunc.go` — `HeadBytes` / `TailBytes` budget helpers for vocabulary and conversation truncation
- [x] `pkg/yap/hint/hint_test.go` — registry tests
- [x] `pkg/yap/hint/vocab_test.go` — tests the file-reading utility against t.TempDir fixtures
- [x] `pkg/yap/hint/noglobals_test.go` — AST guard matching existing packages
- [x] Bundle semantics: `Vocabulary` and `Conversation` are **two orthogonal fields** with different sources. Vocabulary = project docs (CLAUDE.md, AGENTS.md, README.md) read by the daemon's base layer, optionally overridden by `.yap.toml` `vocabulary_terms`. Conversation = app-specific state returned by the matched provider. Vocabulary feeds the Whisper prompt (lexical bias). Conversation feeds the transform context (intent grounding).

### Linux providers (the two shipped in Phase 12)

- [x] `pkg/yap/hint/claudecode/` — reads `~/.claude/projects/<cwd-slug>/<latest-session>.jsonl`
  - `cwd-slug`: absolute cwd with `/` replaced by `-` (e.g. `/home/hybridz/Projects/yap` → `-home-hybridz-Projects-yap`)
  - Latest session = the jsonl in that directory with the most recent mtime
  - Parses user + assistant entries, extracts `message.content` strings (and text blocks from assistant arrays), skips meta / command-caveat / tool-use entries
  - Formats as natural `user: … / assistant: …` prose
  - Returns **only `Bundle.Conversation`** — vocabulary is handled by the daemon's base layer
  - `Supports` matches on `target.AppType == AppTerminal` or `target.Tmux`
  - Graceful empty bundle when no session file exists (not an error)
- [x] `pkg/yap/hint/termscroll/` — single provider with an internal **strategy pattern** (mirrors Phase 4 inject architecture). Each terminal backend is a Strategy; the provider walks them in priority order.
  - `termscroll.go` — `Provider` impl: `Name()="termscroll"`, `Supports()` matches `AppTerminal || Tmux`, `Fetch()` walks strategy list
  - `strategy.go` — `Strategy` interface: `Name() string`, `Supports(inject.Target) bool`, `Read(ctx) (string, error)`
  - `kitty.go` — strategy: `kitty @ get-text --extent=screen`. Detects socket via `$KITTY_LISTEN_ON` or `/tmp/kitty-{uid}-*` probing. Graceful skip when `allow_remote_control` is not enabled.
  - Strips ANSI sequences from all strategy output
  - First-match-wins: claudecode ranks above termscroll in default list

### Active-window detection reuse

- [x] Daemon calls `d.injector.(inject.StrategyResolver).Resolve(ctx)` at press time to get the focused `inject.Target`. Same `Target` drives both the hint provider walk AND the eventual `InjectStream` delivery. Single-source-of-truth for target classification.
- [x] Vocabulary files resolved from the focused window's cwd (via `/proc` walk to descendant shell), not the daemon's cwd.

### Config schema

- [x] `pkg/yap/config.HintConfig` under `Config.Hint`: `enabled`, `vocabulary_files`, `providers`, `vocabulary_max_chars`, `conversation_max_chars`, `timeout_ms`
- [x] `enabled = true` by default
- [x] Validator in `pkg/yap/config/validate.go`: clamp ranges, reject unknown provider names against `hint.Providers()`
- [x] `nixosModules.nix` regenerated from the updated schema

### Daemon wiring

- [x] `Daemon` struct gains hint providers built at startup from `cfg.Hint.Providers` via the registry
- [x] `startRecording` calls `fetchHintBundle(ctx)` BEFORE audio capture — two-layer assembly with bounded timeout
- [x] Bundle threaded into `engine.RunOptions` as `TranscribeOpts` and `TransformOpts`

### CLI

- [x] `yap hint` debug command — prints resolved Target + winning provider + bundle summary
- [x] `yap init` — generates `.yap.toml` with extracted vocabulary terms from project docs
- [x] `yap init --ai` — LLM-based term extraction via configured transform backend
- [x] `yap init --ai --backend claude` — zero-config term extraction via Claude Code CLI

### Tests

- [x] Unit tests for the two new Options structs in `pkg/yap/transcribe` and `pkg/yap/transform`
- [x] Updated backend tests for every transcribe + transform implementation to thread Options through
- [x] `pkg/yap/hint/claudecode/claudecode_test.go` — parses fixture session.jsonl
- [x] `pkg/yap/hint/termscroll/termscroll_test.go` — tests provider walk with fake strategies; ANSI stripping
- [x] `internal/engine/engine_test.go` — exercises Options threading with capturing mock transformer
- [x] `internal/cli/hint_test.go` — exercises the debug command against a fake platform
- [x] `internal/cli/init_test.go` — exercises `yap init` with fake vocabulary input
- [x] AST no-globals guards on every new package

**Done when:**
- [x] Dictating "what is yap?" into a Claude Code session inside the yap repo produces a transcription containing the word "yap", not "jump" / "chap" / "jap" (verified manually on a real host with `whisperlocal` + `base.en` + the claudecode provider enabled)
- [x] `yap config set hint.enabled false` restores pre-Phase-12 behavior byte-for-byte
- [x] `nix develop --command go build ./...` and `nix develop --command go test ./...` both pass
- [x] Every new package has a `noglobals_test.go` AST guard

### Findings

- **Per-call Options, not per-backend Config.** The Whisper prompt must differ per recording because the focused window differs per recording. `transcribe.Options{Prompt}` and `transform.Options{Context}` are per-call value types threaded through `engine.RunOptions`. The engine passes them through unchanged — the daemon owns the assembly.
- **Vocabulary resolves from focused window's cwd, not daemon's cwd.** The daemon walks `/proc` to find the descendant shell's cwd of the focused terminal. This means dictating in a terminal cd'd to `/home/user/project-a` reads `project-a/CLAUDE.md`, even if the daemon started from `/home/user`.
- **`.yap.toml` per-project overrides shipped in Phase 12** (was deferred in original plan). Supports `vocabulary_terms`, `vocabulary_files`, `providers`, and all hint config fields. `yap init` generates this file from project docs.
- **`yap init --ai --backend claude`** uses Claude Code CLI (`claude -p`) for zero-config LLM term extraction — no API key, no transform backend config needed. Falls back to heuristic extraction without `--ai`.
- **`HeadBytes`/`TailBytes` budget helpers** truncate vocabulary (head, for Whisper's 224-token window) and conversation (tail, for most recent context) independently.
- **Provider walk skips when transform is disabled** — if `transform.enabled = false`, conversation context is useless (no LLM to ground), so the provider walk is skipped entirely. Vocabulary still flows to Whisper.
- **Stopword filtering** in heuristic term extraction removes common English words to keep vocabulary focused on domain terms.

### Deferred to Phase 12.5 and later

- **Phase 12.5 — additional termscroll strategies:** tmux (`tmux capture-pane`), wezterm (`wezterm cli get-text`), ghostty (API TBD — investigate IPC), warp (proprietary API TBD). Each is one `.go` file in `pkg/yap/hint/termscroll/` — zero interface changes, zero config changes.
- **AT-SPI provider** (GTK/Qt desktop apps via `org.a11y.atspi.*`). Revisit after a user asks for it.
- **Vision provider** (screenshot + vision-capable LLM). Separate phase when demand emerges.
- **macOS AX provider** — rides with Phase 15 (macOS Support).
- **Windows UIA provider** — rides with Phase 16 (Windows Support).
- **Terminals with no scrollback API** (foot, alacritty, st): vocabulary-only coverage permanently.

---

## Phase 13 — Audio Preprocessing — DONE

**Depends on:** Phase 5, Phase 9
**Goal:** Apply safe, empirically validated audio preprocessing between recording and transcription. Research (arXiv:2512.17562) shows noise reduction preprocessing degrades Whisper accuracy by up to 46.6%. Only two operations are safe: high-pass filtering (removes sub-speech rumble) and silence trimming (prevents hallucinations on dead air).

- [x] `pkg/yap/audioprep/` — pure Go, zero CGo, cross-platform
- [x] 2nd-order Butterworth high-pass biquad filter at configurable cutoff (default 80Hz)
- [x] Leading/trailing silence trimmer via windowed RMS amplitude detection
- [x] Self-contained WAV parser/builder (no go-audio dependency)
- [x] `engine.AudioProcessor` interface (defined in engine, satisfied by audioprep)
- [x] Engine pipeline: record → encode → **audioprep** → transcribe → transform → inject
- [x] `[audio]` config section: `high_pass_filter`, `high_pass_cutoff`, `trim_silence`, `trim_threshold`, `trim_margin_ms`
- [x] Both features enabled by default (`DefaultConfig`)
- [x] Daemon and CLI wiring via `NewAudioPreprocessor` bridge function
- [x] NixOS/Home Manager module regeneration
- [x] Full test suite: WAV round-trip, biquad sine attenuation, trim edge cases, engine integration, noglobals AST guard

**Done when:**
- [x] 40Hz sine wave attenuated >70% by HPF; 440Hz preserved >95%
- [x] Leading/trailing silence trimmed with configurable margin
- [x] `audio.high_pass_filter = false` + `audio.trim_silence = false` restores pre-Phase-13 behavior
- [x] `go test ./...` passes with zero new failures
- [x] No CGo dependencies introduced

### Findings

- **Noise reduction hurts Whisper.** Research (arXiv:2512.17562) shows denoising degrades ASR accuracy by 1.1-46.6% across all noise types and all models tested. Whisper was trained on 680k hours of noisy web audio — spectral distortion from denoising is worse than the original noise. Only sub-speech rumble removal (high-pass) and silence trimming are safe.
- **Biquad filter uses Audio EQ Cookbook coefficients.** 2nd-order Butterworth (Q = 1/√2) with Direct Form II Transposed. Nyquist guard rejects degenerate cutoffs. `math.Round` quantization instead of truncation for correct DSP behavior.
- **WAV parser validates 16-bit mono.** Rejects non-PCM, stereo, or non-16-bit input at parse time. Walks chunks to handle LIST/JUNK between fmt and data. Self-contained — no go-audio dependency.
- **Trimmer handles non-aligned sample counts.** Forward and backward scans clamp the final window to available samples so no trailing fragment is silently dropped. All-silence returns margin's worth of audio (never empty WAV).
- **Typed-nil interface trap handled.** `NewAudioPreprocessor` returns explicit untyped `nil` when both features disabled, not a typed `*Processor(nil)` that would pass the engine's `!= nil` check.
- **Review-driven fixes.** Three independent review agents caught 7 issues: non-aligned trim scan bug, missing format validation, double error prefix, Nyquist guard, odd-chunk padding test gap, int16 truncation→rounding, buildWAV abstraction inconsistency. All fixed before commit.

---

## Phase 14 — Transcription History

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

## Phase 15 — macOS Support

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

## Phase 16 — Windows Support

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

## Phase 18 — Exec Output Mode — DONE

**Depends on:** Phase 5 (Streaming Pipeline)

Adds a second output path: `--exec <command>` pipes the transcript to
an external command via stdin instead of injecting into the focused
application. The engine's output handler is selected per-recording via
`RunOptions.OutputOverride`, threading from CLI → IPC → daemon → engine
with zero changes to the injection pipeline.

### Checklist

- [x] `internal/output/exec/handler.go` — implements `inject.Injector`
- [x] `internal/output/exec/handler_test.go` — happy path, command-not-found, context cancellation
- [x] `internal/ipc/protocol.go` — `Request.Exec` field (omitempty, backwards-compatible)
- [x] `internal/ipc/client.go` — `SendRequest()` for arbitrary Request payloads
- [x] `internal/ipc/server.go` — `toggleFn` signature `func(execCmd string) string`
- [x] `internal/cli/toggle.go` — `--exec` flag, threaded through IPC
- [x] `internal/daemon/daemon.go` — constructs exec handler per-session, passes as `OutputOverride`
- [x] `internal/engine/engine.go` — `RunOptions.OutputOverride` (3-line diff)

### Done when

- [x] `yap toggle --exec cat` records → transcribes → prints transcript to stdout
- [x] `yap toggle` (no --exec) injects into focused app unchanged
- [x] All existing tests pass (`go test ./...`)
- [x] `go vet ./...` clean

### Findings

- **No new interfaces.** The exec handler implements the existing `inject.Injector` contract.
  `InjectStream` collects all chunks, then pipes the concatenated text to the command's stdin
  via direct `os/exec` (no shell). This keeps the engine completely unchanged apart from the
  3-line `OutputOverride` check in `runPipeline`.

- **IPC is backwards-compatible.** The `Exec` field is `omitempty` — old clients sending
  `{"cmd":"toggle"}` get normal injection. The `Send()` convenience function still works
  unchanged; `SendRequest()` is additive.

- **Server toggleFn signature changed** from `func() string` to `func(execCmd string) string`.
  This is internal (not in `pkg/`), so the break is contained to daemon + tests.

---

## Phase 17 — System Tray

**Depends on:** Phase 15, Phase 16

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

## Phase 19 — Evaluation Framework

**Depends on:** Phase 3, Phase 5, Phase 6, Phase 8, Phase 12

**Goal:** Make accuracy measurable. Every downstream accuracy claim ("model X beats Y", "vocab bias reduces WER by Z%", "ensemble improves domain terms") must be defensible with numbers from a reproducible harness. Without this phase, Phases 20-22 are educated guesses; with it, they are engineered decisions.

**Scoping rule (identity boundary):** yap is software + UX, never a model. The eval framework measures how well yap *uses* models, not the models themselves. Fine-tuning / training is explicitly out of scope.

### Corpus

- [ ] `testdata/corpus/` layout: `<locale>/<domain>/<utterance_id>/{audio.wav, reference.txt, meta.toml}`
- [ ] `meta.toml` per utterance: speaker, accent, domain, recording_device, ambient_noise_level, expected_vocabulary_terms
- [ ] Seed corpus (minimum viable):
  - `en/general/` — 20 utterances (baseline regression)
  - `en/programming/` — 20 utterances with yap repo jargon
  - `es/general/` — 20 utterances (neutral Spanish)
  - `es-MX/general/`, `es-AR/general/`, `es-ES/general/` — 10 utterances each
  - `es/programming/` — 10 utterances (Spanglish + technical terms)
- [ ] `yap eval record <locale> <domain>` — guided recording tool: prompts user with reference text, captures audio, writes corpus entry
- [ ] `yap eval import <dir>` — import external datasets (Common Voice subsets, LibriSpeech-ES, etc.) with reference transcripts
- [ ] Corpus stored in git LFS (audio is binary, large); reference text + meta in regular git
- [ ] `testdata/corpus/README.md` documents licensing per source

### Metrics

- [ ] `pkg/yap/eval/` — pure Go package, no CGo
- [ ] `eval.WER(reference, hypothesis string) float64` — Levenshtein over words, normalized
- [ ] `eval.CER(reference, hypothesis string) float64` — Levenshtein over characters
- [ ] `eval.VocabRecall(reference, hypothesis string, expected []string) float64` — fraction of `expected` terms that appear in hypothesis (case-insensitive, substring match with word boundaries). Directly measures the Phase 12 motivating case ("yap"→"jump").
- [ ] `eval.NormalizeText(locale string, s string) string` — locale-aware normalization (lowercase, strip punctuation, Unicode NFD→NFC, ES-specific: handle ¿¡, ñ, accents). Applied before WER/CER so punctuation differences don't inflate the score.
- [ ] Latency metrics: `eval.Latency{Total, Record, Audioprep, Transcribe, Transform, Inject time.Duration}` per utterance
- [ ] Confidence correlation: if backend returns logprobs, compute Pearson correlation between mean logprob and WER — validates whether confidence is calibrated enough to trigger re-transcription (Phase 21)

### Statistical rigor

- [ ] **Backend nondeterminism handling.** Remote backends (Groq, OpenAI) return different outputs across identical requests. Harness runs each utterance N times (default N=3, configurable) and reports `{mean, stddev, min, max}` per metric, not just the point estimate.
- [ ] **Bootstrap confidence intervals.** `eval.BootstrapCI(samples []float64, confidence float64) (low, high float64)` — 1000 resamples for 95% CI on corpus-level WER/CER. A/B comparisons report CI overlap, not just mean delta. Prevents noise-driven conclusions on small corpora.
- [ ] **Significance threshold for regression alerts.** CI gate fires only when CI lower bound on delta exceeds the threshold (default 0.5% absolute WER). No more "mean regressed 0.3%, fail the build" false positives.
- [ ] **Variance budget per utterance.** If a single utterance's N-run stddev exceeds backend-expected variance, flag the utterance as unstable and exclude from summary (noisy input).

### Streaming harness mode

- [ ] **Rationale:** production P95 is dominated by streaming behavior (first-chunk latency, time-to-first-token in transform), not full-WAV batch. Full-WAV eval measures accuracy well but misreports latency.
- [ ] `yap eval run --mode=streaming` — drives backends in streaming mode, measures `time_to_first_char` in addition to `total_time`
- [ ] Latency metrics gain: `eval.Latency{TimeToFirstChar, TimeToFirstWord, TotalTime}`
- [ ] P95 budget applies to `TimeToFirstChar` in streaming mode (the value users feel), to `TotalTime` in batch mode
- [ ] Default mode for CI = streaming; batch mode available for accuracy-only sweeps

### Shadow mode (opt-in real-world eval)

- [ ] **Rationale:** curated corpus ≠ real usage. A pack audit that says "no regression on corpus" may still regress on a user's actual recordings.
- [ ] `general.eval_shadow_mode = true` (opt-in, default off) — daemon writes every recording + final transcription to `~/.local/share/yap/shadow/` (respecting history redaction; see Phase 19.5)
- [ ] `yap eval run --shadow` — runs the harness against the user's shadow corpus with per-user baseline
- [ ] Shadow corpus never leaves the machine; not uploaded to CI, not shared
- [ ] `yap eval shadow clear` — delete shadow corpus
- [ ] Shadow eval signals pack-audit absorption decisions on real data, not just curated

### Latency budget tracker (cross-cutting, consumed by harness + daemon)

- [ ] `pkg/yap/budget/` — pure budget/deadline tracker shared between daemon runtime and eval harness
- [ ] `budget.Tracker` tracks per-stage deadlines and actual durations; emits structured events
- [ ] Pipeline stages receive `context.Context` with deadline = `RunStart + HardCeiling`
- [ ] Daemon: `yap status --latency` prints rolling P50/P95/P99 per stage across last 100 recordings
- [ ] Harness: `eval.LatencyBudget{P95 time.Duration; HardCeiling time.Duration}` defaults P95=2000ms, HardCeiling=3000ms
- [ ] Per-stage SLAs: record=50ms, audioprep=50ms, transcribe=1100ms, transform_first_token=400ms, inject=50ms, margin=350ms (sums to 2000ms P95)
- [ ] `yap eval run` fails if P95 > budget.P95 regardless of WER improvement

### Backend capability manifest

- [ ] **Rationale:** Phases 20/21/22 reference "backends that support keyterms / decoding params / streaming." Today this is implicit. Make it explicit.
- [ ] `transcribe.Capabilities` struct returned by each backend: `{Streaming, KeyTerms, LanguageLock, BeamSize, Temperature, ConfidenceScore, ...}`
- [ ] Pack loader and options threading consult capabilities and degrade silently for unsupported features (no runtime panics, no silent drops — logged at debug level)
- [ ] `yap eval backends` prints capability matrix for all registered backends

### Harness

- [ ] `yap eval run [--locale=es-MX] [--domain=programming] [--backend=groq] [--config=alt.toml] [--mode=streaming|batch] [--runs=3] [--shadow]` — runs full corpus or subset through a real yap pipeline. Each utterance: load WAV → audioprep → transcribe → transform → collect output + latencies.
- [ ] Output: JSONL per utterance + summary table (mean/stddev/p50/p95 WER, CER, vocab recall, latency per stage)
- [ ] `yap eval diff <run_a.jsonl> <run_b.jsonl>` — per-utterance WER delta with bootstrap CI, sorted by regression magnitude
- [ ] `yap eval sweep <param=vals>` — grid sweep, e.g. `yap eval sweep transcription.model=base.en,small.en,medium.en,large-v3` runs each value, outputs comparison table with CIs
- [ ] `yap eval pack-audit <pack_name> [entry_id]` — for decay protocol: runs eval with a specific pack entry disabled, compares vs baseline with CI. Per-entry audit, not per-file.
- [ ] Deterministic mode: hint providers return fixtures, not live system state. `eval.FakeHintProvider` for reproducibility.
- [ ] Isolation: eval harness never touches user's config or daemon state (`YAP_CONFIG_PATH=testdata/eval.toml`)

### CI integration

- [ ] `.github/workflows/eval.yml` — runs on every PR against `en/general/` baseline only (small corpus to keep CI under 3 min). Fails when bootstrap CI lower bound on WER delta exceeds 0.5% absolute vs main branch baseline (significance-gated, not mean-gated).
- [ ] Full corpus eval runs nightly on main; results posted to a GitHub Issue ("nightly eval report") with trend chart and per-metric CIs
- [ ] Baseline numbers committed to `testdata/baselines.json` (versioned per corpus version); updated intentionally via `yap eval update-baseline`
- [ ] Versioned defaults file: `config/defaults/<version>.toml` — when Phase 21 eval updates a default (model, decoding param, etc.), the old defaults remain addressable so users on old corpora don't silently regress

### CLI

- [ ] `yap eval record` — corpus capture
- [ ] `yap eval import` — external dataset import
- [ ] `yap eval run` — run harness (supports `--mode`, `--runs`, `--shadow`)
- [ ] `yap eval diff` — compare two runs with CIs
- [ ] `yap eval sweep` — grid search
- [ ] `yap eval pack-audit` — decay protocol entry-point (Phase 22)
- [ ] `yap eval shadow clear` — wipe shadow corpus
- [ ] `yap eval backends` — capability matrix
- [ ] `yap eval update-baseline` — commit current numbers as new baseline (requires `--confirm`)

### Tests

- [ ] `pkg/yap/eval/wer_test.go` — table tests covering insertion/deletion/substitution combinations, Unicode edge cases, empty strings
- [ ] `pkg/yap/eval/normalize_test.go` — locale-specific normalization fixtures
- [ ] `pkg/yap/eval/harness_test.go` — end-to-end with mock backends, asserts WER/latency shape
- [ ] `pkg/yap/eval/noglobals_test.go` — AST guard

### Non-goals (explicit scope closure)

- **No multi-speaker / diarization.** Dictation is single-speaker by definition. If the use case emerges, it is a separate phase, not a metric addition.
- **No model fine-tuning / training eval.** yap measures model *use*, not model *quality*. Benchmarking whisper-vs-whisper belongs upstream.
- **No cloud-hosted eval.** All corpus and eval runs stay local. Shadow mode is explicitly machine-local.

### Done when

- [ ] `yap eval run --locale=en` produces WER/CER/latency numbers with bootstrap CIs for the seed `en/general/` corpus
- [ ] `yap eval run --mode=streaming` reports `time_to_first_char` P95 (the latency number users feel)
- [ ] `yap eval diff` shows per-utterance regressions with CIs between two runs
- [ ] PR CI fails only when bootstrap CI lower bound on WER delta exceeds 0.5% (no noise-driven failures)
- [ ] Nightly full-corpus eval runs unattended and posts a report
- [ ] Latency P95 > 2000ms fails the harness even if WER improved
- [ ] `yap eval pack-audit` disables one pack entry at a time and reports per-entry WER delta with CI
- [ ] `yap eval backends` prints the capability matrix consumed by Phases 20-22

---

## Phase 19.5 — Privacy & Redaction

**Depends on:** Phase 3, Phase 19

**Goal:** Make yap safe to ship the features in Phases 20-22 that touch user content. Corrections history, conversation context, keyterms, shadow corpus, and pack-audit data can all contain PII, secrets, credentials, or confidential content. This phase is a hard gate for 20.6 (learn-from-corrections), 22 (community packs), and 19's shadow mode.

**Scoping rule:** privacy is infrastructure, not a feature. Every data sink in the pipeline declares its retention policy, redaction policy, and egress policy in a type the compiler can check.

### Redaction pipeline

- [ ] `pkg/yap/redact/` — pure Go, rule-based redaction
- [ ] `redact.Redactor` interface: `Redact(text string, policy Policy) string`
- [ ] Built-in patterns: email addresses, phone numbers, credit-card-shaped digits, AWS/GCP/GitHub token prefixes, bearer tokens, URLs containing credentials
- [ ] `redact.Policy` struct with per-sink settings: `{Emails, Phones, Tokens, URLs, CustomRegex []string}`
- [ ] User-extensible via `~/.config/yap/redact.toml` — custom regex rules (e.g. company-internal ticket IDs, proprietary codes)
- [ ] Applied at write boundaries: history (Phase 14), shadow corpus (Phase 19), corrections store (Phase 20.6), remote backend prompts (transcribe + transform)

### Egress classification

- [ ] Every text-carrying channel tagged with an egress class: `EgressLocal` (never leaves machine), `EgressRemoteBackend` (sent to configured transcribe/transform backend), `EgressPackContribution` (shipped if user contributes a pack)
- [ ] Redaction policy per class — `EgressRemoteBackend` applies aggressive defaults; `EgressLocal` applies user-configured only
- [ ] `yap privacy show` prints the classification table: what goes where, what's redacted, retention TTL

### Corrections privacy (gates Phase 20.6)

- [ ] Personal vocabulary never sent to remote backends as raw strings without redaction pass
- [ ] Corrections store is user-readable, user-deletable: `yap corrections export`, `yap corrections forget <term>`, `yap corrections clear`
- [ ] Opt-out: `general.corrections_learning = false` disables entirely

### Shadow corpus privacy (gates Phase 19 shadow mode)

- [ ] Shadow recordings encrypted at rest using a per-user key stored in the OS keyring (libsecret/Keychain/Credential Manager)
- [ ] Retention TTL configurable: `general.shadow_retention_days = 30` default, auto-prune past TTL
- [ ] Shadow corpus is never an input to remote eval or telemetry — strictly local
- [ ] Explicit consent prompt on first `--shadow` invocation; user must type `yes` to enable

### Pack contribution privacy (gates Phase 22 community packs)

- [ ] `yap pack new --from-corrections` scaffolds a pack from user corrections but runs redaction pass first and shows a diff for user review before writing
- [ ] Pack PRs include a declaration: which corpus entries were user-generated vs synthetic — reviewers know what to scan
- [ ] PII scanner runs on pack PRs in CI; blocks merge on detected patterns unless maintainer override

### Tests

- [ ] `pkg/yap/redact/redact_test.go` — each built-in pattern against a fixture
- [ ] `pkg/yap/redact/policy_test.go` — policy merge + custom regex
- [ ] `pkg/yap/redact/egress_test.go` — egress class routing
- [ ] `pkg/yap/redact/noglobals_test.go` — AST guard
- [ ] Integration: history writer, shadow writer, corrections writer all go through redaction (asserted via fixture-based round-trip tests)

### Done when

- [ ] `yap privacy show` prints the full egress + redaction table
- [ ] History, shadow, corrections, and prompt assembly all route writes through `redact.Redactor`
- [ ] Shadow corpus is encrypted on disk; key rotates on user request
- [ ] Pack contribution tool strips PII before showing the diff
- [ ] CI PII scanner blocks accidental leaks in pack PRs
- [ ] No item in Phases 20.6 or 22 can land without its corresponding privacy test passing

---

## Phase 20 — Accuracy: Objective Wins

**Depends on:** Phase 12, Phase 19 (for verification, not for implementation), Phase 14 (for 20c only)

**Goal:** Ship the accuracy improvements that are objectively better with no tunable trade-off. These are wins that research or vendor documentation already validates — we don't need to run A/B experiments to justify them. Phase 19's harness verifies they land cleanly but is not a gate for shipping.

**Scoping rule:** every item here must be (a) known-correct (research-validated or trivially better), (b) latency-neutral or latency-positive, (c) a pure *use-the-model-better* change — never a model replacement or fine-tune.

**Structure:** split into three sub-phases by cost and dependencies. 20a is pure config/API plumbing (can ship in parallel with Phase 19). 20b adds pipeline stages (gated on capability manifest). 20c depends on Phase 14 and Phase 19.5.

---

### Phase 20a — Config & API

Zero new pipeline stages. Pure plumbing, eval-independent.

#### 20a.1 — Language lock + multilingual default

- [ ] `transcription.language` config field (ISO-639-1, e.g. `en`, `es`) — defaults to empty (auto-detect)
- [ ] When set, passed to backend as `language` param (Groq, OpenAI, whisper.cpp all support per capability manifest)
- [ ] Default `transcription.model` auto-selects multilingual variant when `language != "en"` (drops `.en` suffix); `.en` variants remain available for English-only speakers who want the marginal accuracy + speed bump
- [ ] Validator rejects unknown ISO codes; warns on `.en` model + non-`en` language combo
- [ ] `yap config wizard` asks for primary language at setup
- [ ] **Eliminates** the "Spanish utterance transcribed as Italian" failure class

#### 20a.2 — Dedicated vocabulary API (capability-gated)

- [ ] Research basis: Groq exposes `keyterms` (strong lexical bias, distinct from `prompt`). Deepgram exposes `keywords`. These are purpose-built for vocabulary injection and outperform free-form prompts.
- [ ] Extend `transcribe.Options` with `KeyTerms []string` (orthogonal to `Prompt string`)
- [ ] Routing via Phase 19 backend capability manifest: backends advertising `Capabilities.KeyTerms=true` receive the dedicated param; others fall through to prompt bias (no regression, no panic)
- [ ] Groq backend: maps to `keyterms` API param
- [ ] Deepgram backend (when added): maps to `keywords`
- [ ] Daemon builds `KeyTerms` from hint bundle's vocabulary file section, chunked to API limits

#### 20a.3 — Locale puntuación enforcement

- [ ] Post-transform validator: for `language=es`, enforce `¿...?` and `¡...!` pairs (opening marks are required in Spanish; LLMs often skip them)
- [ ] Locale-specific number format: ES uses `1.000,50`; EN uses `1,000.50`. Transform prompt addon per locale.
- [ ] Date format per locale: DD/MM/YYYY for most non-US locales
- [ ] Runs post-transform, pre-inject; costs <5ms

---

### Phase 20b — Pipeline additions

New pipeline stages. Latency cost measured before default-enabled.

#### 20b.1 — VAD pre-segmentation (silero-vad)

- [ ] Research basis: Whisper hallucinates on non-speech audio (dead air, background noise bursts). VAD gating is standard practice in production ASR pipelines.
- [ ] `pkg/yap/audioprep/vad/` — silero-vad via pure-Go ONNX runtime (or CGo-free alternative). Model is ~2MB, embedded.
- [ ] Integrates into Phase 13 pipeline: `record → hpf → trim → vad → transcribe`
- [ ] VAD outputs speech/non-speech spans; concatenates speech-only audio for Whisper
- [ ] **Default OFF** pending Phase 19 latency measurement. Flip to default-on in Phase 21 only if sweep confirms latency cost <100ms P95 and hallucination-rate reduction is measurable.
- [ ] Complements existing `silence.*` config — VAD is framing pre-Whisper; silence detection is recording termination

#### 20b.2 — Phonetic post-replace

- [ ] Research basis: deterministic, cheap, and fixes the exact "yap→jump" failure class. Runs post-transcription, pre-transform.
- [ ] `pkg/yap/postprocess/phonetic/` — Double Metaphone (English) + Metaphone-ES (Spanish) implementations
- [ ] Algorithm: for each word in transcription, compute phonetic key. For each vocabulary term, compute phonetic key. Replace word with vocab term when (a) phonetic keys match, (b) Levenshtein distance on the original words is below a threshold proportional to length, (c) vocab term has higher prior (appears in project docs — this is deterministic rule prior, not a learned model).
- [ ] **Identity boundary:** this is explicitly rule-based, not a statistical LM. No weights, no learned priors. Input: vocabulary list + hypothesis. Output: substituted hypothesis.
- [ ] Implemented as a new pipeline stage (not a transform backend — this is deterministic rule-based, not generative)
- [ ] `postprocess.phonetic.enabled = true` default; per-locale enable
- [ ] Zero-cost when vocabulary is empty

---

### Phase 20c — Durable-value (user-learning loop)

**Hard prerequisites:** Phase 14 (history infrastructure), Phase 19.5 (redaction + corrections-store privacy).

#### 20c.1 — Learn-from-corrections

- [ ] When user undoes-and-retypes within N seconds of injection, capture the diff: `{original: "jump", corrected: "yap", context: "what is ___"}`
- [ ] Store in `~/.local/share/yap/corrections.jsonl` (append-only, user-private, redacted per Phase 19.5 policy before write)
- [ ] After threshold (e.g. 3 occurrences of same correction), auto-add the corrected term to a personal vocabulary file
- [ ] **Identity boundary:** corrections produce a vocabulary hint (text file consumed by `hint.Provider`), never a trained weight or personalized model artifact. Writes are to hint-layer files only; no model state modified.
- [ ] Personal vocab exposed via a new `hint/personal/` provider that composes alongside Phase 12 providers — single source of truth for all vocabulary routing
- [ ] `yap corrections list` / `yap corrections forget <term>` / `yap corrections clear` for management
- [ ] Privacy: personal corrections never leave the machine as raw text. When sent to remote backends, they flow through Phase 19.5 redaction + the Phase 20a.2 `KeyTerms` path.
- [ ] Opt-out: `general.corrections_learning = false` disables entirely (Phase 19.5 requirement)
- [ ] This is yap's durable moat — no model will ever know your coworker's name

---

### Done when

- [ ] **20a:** Language lock eliminates Spanish-as-Italian misclassification; Groq uses `keyterms` when vocabulary non-empty (verified via request inspection); Spanish transcriptions end with proper `¿¡` marks
- [ ] **20b:** Phonetic replace fixes "yap/jump" on the Phase 12 motivating fixture; VAD ships as opt-in with measured latency/hallucination numbers in the eval harness
- [ ] **20c:** After 3 undo-retype cycles, "Enriquefft" appears in personal vocab without manual intervention; corrections store honors redaction policy per Phase 19.5
- [ ] `yap eval run` (from Phase 19) shows WER improvement on the Spanish corpus vs. pre-phase baseline, with zero latency regression at the 2000ms P95 budget
- [ ] No new vocabulary plumbing: all vocab (project docs, personal corrections, pack seeds) flows through the Phase 12 `hint.Bundle` — one canonical path

---

## Phase 21 — Accuracy: Testable Levers

**Depends on:** Phase 19 (hard gate — these items ship only when measurable)

**Goal:** Accuracy levers whose optimal setting depends on user, workload, or backend. They are not objectively better at every setting — they are trade-offs. Phase 19's harness is required to pick defaults and to let users tune per-locale / per-domain.

**Scoping rule:** nothing here ships without an eval number. Each item must produce a WER/latency table across its tunable range before the default is chosen.

### 21.1 — Model selection sweep

- [ ] Run `yap eval sweep transcription.model=tiny,base,small,medium,large-v3,turbo` per locale
- [ ] Publish WER × latency frontier as markdown table in `docs/model-selection.md`
- [ ] Default model per locale chosen from Pareto frontier at the 2000ms P95 constraint
- [ ] Wizard recommends model based on user's hardware (CPU-only → small, GPU → large-v3)
- [ ] Auto-detect GPU presence (NVIDIA: `nvidia-smi`; Apple: M-series check) at first run

### 21.2 — Decoding parameter tuning

- [ ] Whisper exposes `beam_size`, `best_of`, `temperature`, `temperature_increment_on_fallback`, `compression_ratio_threshold`, `logprob_threshold`, `no_speech_threshold`. Most are hidden today.
- [ ] Expose under `transcription.decoding.*` in config (whisper.cpp + faster-whisper support all; Groq/OpenAI API expose a subset)
- [ ] Eval sweep: `beam_size ∈ {1, 3, 5, 10}`, `best_of ∈ {1, 5}`, `temperature_fallback ∈ {true, false}`
- [ ] Find default that maximizes WER reduction at <10% latency cost
- [ ] Document per-backend support matrix

### 21.3 — Multi-backend ensemble (LLM arbiter only)

- [ ] `pkg/yap/transcribe/ensemble/` — runs N backends in parallel, collects all hypotheses
- [ ] **Single arbiter strategy: LLM arbiter.** Reuses the existing transform backend (already wired in Phase 8). Prompt: "given this context `{hint}`, pick the most plausible transcription from these candidates: ...". No runtime strategy switching, no plugin system for arbiters.
- [ ] Other arbiter strategies (edit-distance consensus, confidence-weighted voting) are explicitly deferred to a later phase — shipping three at once is scope creep. If the LLM arbiter underperforms, the phase that adds a second strategy ships with its own eval.
- [ ] Eval sweep: solo vs 2-backend vs 3-backend ensemble; measure WER gain vs latency cost with bootstrap CI
- [ ] Gated by latency budget — ensemble skipped when budget projection exceeds P95 ceiling
- [ ] Expected result: ensemble wins on domain-heavy utterances, loses on simple ones. Default disabled, enabled per-domain via pack.

### 21.4 — Confidence-gated re-transcription

- [ ] Whisper backends return per-segment logprob. When `mean_logprob < threshold`, re-run just that segment with (a) larger model, (b) different temperature, (c) extended context
- [ ] Only viable if Phase 19 confirms logprob-WER correlation is high enough to be a useful signal
- [ ] Latency cost: 1.5-2x transcribe time on gated segments (hit rate is the key metric)
- [ ] Eval sweep: threshold ∈ {-1.0, -0.5, -0.3, -0.1}; measure hit rate, WER on gated segments, total latency
- [ ] If latency violates budget, this phase ships as opt-in only

### 21.5 — Graceful degradation strategy

- [ ] Ordered fallback list when projected latency > hard ceiling:
  1. Skip ensemble → primary only
  2. Skip confidence re-transcription → single pass
  3. Skip LLM transform → raw inject + notification
  4. Downgrade model one tier (large-v3 → medium) for remainder of session
- [ ] Each step logged; `yap status` shows "degraded: reason=..."
- [ ] Eval harness verifies degradation triggers fire when expected (not just on paper)
- [ ] Automatic recovery: when rolling P95 returns below P95 target for 20 recordings, restore full config

### 21.6 — Context budget sweep

- [ ] Phase 12 shipped `vocabulary_max_chars` and `conversation_max_chars`. Defaults were educated guesses.
- [ ] Eval sweep: vocab ∈ {0, 500, 1000, 2000, 4000 bytes}, conversation ∈ {0, 500, 1000, 2000, 4000 bytes}
- [ ] Expect diminishing returns past Whisper's 224-token prompt window for vocabulary; conversation may benefit from more (feeds LLM transform, not Whisper)
- [ ] Update defaults based on eval

### 21.7 — Post-process hook infrastructure (first-class extension surface)

- [ ] **Positioning:** this is infrastructure, not an escape hatch. Agents are users of yap (Philosophy #7). A typed, permissioned post-process hook is the agent-callable command surface that lets external tools shape transcripts without forking yap. It stays in the roadmap regardless of how well 21.1-21.6 perform.
- [ ] `pkg/yap/postprocess/hook/` — reads stdin (transcription + metadata JSON header), writes stdout (replacement), 1s timeout default
- [ ] `postprocess.hooks = ["/path/to/script.sh", ...]` in config; runs in order, pipes transcript through each
- [ ] Hook input envelope: `{transcript, locale, domains, hint_bundle_summary, backend, confidence}` — rich enough that hooks can make informed transformations, not just string substitution
- [ ] Hook output envelope: `{transcript, abort?: "reason"}` — hooks can abort the pipeline with a user-visible reason
- [ ] Security: hooks are user-owned scripts — no sandbox, treated as trusted code (same trust model as Claude Code hooks and shell rc files)
- [ ] Hook discovery: `yap hooks list` enumerates configured hooks; `yap hooks run <script> --dry-run "test transcript"` exercises a hook without recording
- [ ] Eval harness supports `--hook` flag to measure per-hook WER/latency impact
- [ ] Documented as a stable extension point in `docs/hooks.md` — this is infrastructure yap commits to supporting long-term

### 21.8 — Audio preprocessing sweep

- [ ] Extends Phase 13. Eval-gated additions only.
- [ ] Candidates: loudness normalization (EBU R128), dynamic range compression (light ratio), pre-emphasis filter
- [ ] Each ships only if eval shows WER win with no latency regression
- [ ] Research (arXiv:2512.17562) ruled out denoising; that finding stands unless a new paper overturns it

### Done when

- [ ] Every item in this phase has an entry in `docs/accuracy-tuning.md` with its eval table
- [ ] Default config values per-locale are chosen from eval results, not intuition
- [ ] `yap eval run` shows monotonic WER improvement as each lever lands (or the lever is rejected)
- [ ] Graceful degradation demonstrably fires in simulated budget-overrun tests
- [ ] No lever ships if it violates the 2000ms P95 / 3000ms hard ceiling without an opt-in flag

---

## Phase 22 — Locale + Domain Packs

**Depends on:** Phase 12, Phase 19, Phase 19.5, Phase 20a

**Goal:** Ship per-locale and per-domain configuration bundles that compose orthogonally. A user declares `locale=es-MX` and `domain=programming` independently; yap merges both at load. No per-combination pack proliferation.

**Scoping rule (critical):** packs are **thin declarative TOML**, not code. Anything a future model will likely absorb (general slang lists, common domain jargon, mainstream accent handling) ships as decay-expected content. Anything durable (backend preferences, format rules, prompt addons) ships as structural config. Fine-tuning or custom models are out of scope forever — yap uses models, never produces them.

**Single source of truth for vocabulary:** packs **must not** ship a parallel vocabulary path. All vocabulary — project docs (Phase 12), personal corrections (Phase 20c), pack contributions (this phase) — funnels through the Phase 12 `hint.Bundle` via dedicated hint providers. This phase adds one new provider (`pack`); it does not add a second vocabulary pipe.

### Architecture

- [ ] `pkg/yap/pack/` — pack loader + merge logic (schema, not vocabulary pipeline)
- [ ] `pkg/yap/hint/pack/` — a hint provider that reads active pack vocabulary and emits it as `hint.Bundle.Vocabulary` + `hint.Bundle.KeyTerms`. This is the single path pack vocabulary takes into the pipeline.
- [ ] Pack directories shipped in binary: `locales/<code>.toml` and `domains/<name>.toml` (embedded via `//go:embed`)
- [ ] User override: `~/.config/yap/packs/{locales,domains}/*.toml` shadows shipped packs
- [ ] Config: `general.locale = "es-MX"`, `general.domains = ["programming", "devops"]` (multiple domains stack)
- [ ] Merge order: defaults → locale → domain[0] → domain[1] → ... → user config → `.yap.toml` project override
- [ ] Registry-free at the pack level: packs are data, not pluggable code. Adding a new pack is a TOML PR, not a Go change. (The `pack` hint provider is the single Go piece; it does not grow per pack.)

### Pack schema (TOML)

**Locale pack fields** (structural, durable):
- `language` — ISO-639-1 (e.g. `es`)
- `backend_preference` — ordered list of backend@model combos (e.g. `["groq@whisper-large-v3", "deepgram@nova-3-es-mx"]`). Resolution consults Phase 19 capability manifest — first entry a registered backend supports wins; others fall through silently.
- `number_format`, `date_format` — format strings
- `punctuation_rules` — regex-based enforcement pairs (e.g. `¿...?`)
- `transform_prompt_addon` — appended to the base transform system prompt

**Locale pack fields** (decay-expected, per-entry audited):
- `vocabulary_terms` — list of `{term: "...", audit_date: "2026-06-01", audit_corpus: "testdata/corpus/es-MX/voseo/"}` entries. Each entry must reference a corpus subset that exercises the term. Loaded via the `pack` hint provider into `hint.Bundle.Vocabulary`.

**Domain pack fields** (structural):
- `transform_prompt_addon` — e.g. "no traducir términos técnicos del inglés"
- `decoding_override` — optional decoding param tweaks (e.g. programming may want higher `beam_size`)

**Domain pack fields** (decay-expected, per-entry audited):
- `vocabulary_terms` — same schema as locale pack; flows into `hint.Bundle.Vocabulary`
- `phonetic_rules` — explicit corrections for cases current models fail on (`{rule: "...", audit_date: "...", audit_corpus: "..."}`). Consumed by Phase 20b.2 phonetic replacer via the hint bundle.
- `keyterms` — same schema; flows into `hint.Bundle.KeyTerms` (Phase 20a.2)

### Shipped packs — locales (v0)

- [ ] `locales/en.toml` — default, minimal
- [ ] `locales/es.toml` — neutral Spanish (¿¡ enforcement, 1.000,50 format)
- [ ] `locales/es-MX.toml`
- [ ] `locales/es-AR.toml` — voseo preservation prompt addon
- [ ] `locales/es-CL.toml`
- [ ] `locales/es-ES.toml`

### Shipped packs — domains (v0)

- [ ] `domains/programming.toml` — generic: English technical terms preserved, common tool names (git, docker, kubernetes — only the ones empirically still failing per Phase 19 eval)
- [ ] `domains/medicine.toml`
- [ ] `domains/finance.toml`
- [ ] `domains/academic.toml`
- [ ] `domains/legal.toml`

### Decay protocol

**Per-entry, significance-gated, corpus-backed:**

- [ ] Every decay-expected entry (vocabulary term, phonetic rule, keyterm) carries three required fields: `audit_date` (next audit), `audit_corpus` (path to a corpus subset that exercises the entry), and `added_date` (for age tracking)
- [ ] `yap eval pack-audit <pack> [entry_id]` runs Phase 19 eval with exactly that entry disabled. Disabling any entry whose `audit_corpus` has no utterances is a hard error (prevents false "no regression" readings).
- [ ] Absorption verdict requires: (a) bootstrap CI lower bound on WER delta ≥ -0.5% absolute (not just mean ≥ 0), (b) N ≥ 30 utterances in `audit_corpus`, (c) verdict stable across three runs (handles backend nondeterminism). Anything below those gates = inconclusive, entry stays.
- [ ] Per-entry audit, never per-file. A pack with 50 vocab terms produces 50 audit verdicts, not one.
- [ ] Quarterly: `yap eval pack-audit --all` runs in CI; files an issue listing entries with absorption verdict = true. Maintainer confirms before removal (no auto-delete — human review on community-facing content).
- [ ] Shadow mode (Phase 19 shadow) provides a second audit surface: `yap eval pack-audit --shadow` runs the same protocol against the user's real recordings, not curated corpus. Curated + shadow verdicts must agree for removal.
- [ ] This guarantees packs shrink over time as models improve — yap becomes *thinner* as the ecosystem matures, without noise-driven deletions.

### Backend preference swap-ready

- [ ] `backend_preference` is the durable piece of a locale pack
- [ ] Registry backends advertise supported model IDs; pack loader picks the first supported entry
- [ ] When a specialized model drops (e.g. hypothetical `deepgram:nova-3-es-mx`), updating the pack TOML is a 1-line change — zero code changes in yap core
- [ ] Fallback chain: if preferred model unavailable (no API key, not installed), fall through to next entry silently

### Community contribution

- [ ] `docs/PACKS.md` — contribution guide: TOML schema, required fields, eval + audit requirements
- [ ] Pack PRs gate on Phase 19 eval passing against the pack's own corpus AND each decay-expected entry including an `audit_corpus` that exercises it (no more "ship a term with no test")
- [ ] Phase 19.5 PII scanner runs on pack PRs; blocks merge on detected patterns unless maintainer override
- [ ] No pack merges without (a) corpus, (b) baseline numbers with bootstrap CIs, (c) per-entry audit corpus references, (d) decay markers on time-bound entries, (e) PII scan clean

### CLI

- [ ] `yap pack list` — shipped + user packs, active merge chain
- [ ] `yap pack show <name>` — resolved content after merge
- [ ] `yap pack audit [<name>]` — run decay eval
- [ ] `yap pack new <type> <name>` — scaffold a user pack with schema template
- [ ] `yap config set general.locale es-MX` / `yap config set general.domains programming,devops`

### Tests

- [ ] `pkg/yap/pack/merge_test.go` — locale × domain × user merge precedence
- [ ] `pkg/yap/pack/schema_test.go` — TOML validation, required fields, unknown fields
- [ ] `pkg/yap/pack/audit_test.go` — decay protocol with fixture packs and fake eval harness
- [ ] `pkg/yap/pack/noglobals_test.go` — AST guard
- [ ] Golden eval: `es-MX` pack demonstrably reduces WER on `testdata/corpus/es-MX/` vs `es` alone

### Done when

- [ ] `yap config set general.locale es-MX` + `yap config set general.domains programming` applies both packs, verifiable via `yap pack show active`
- [ ] Shipped locale packs cover `en`, `es`, `es-MX`, `es-AR`, `es-CL`, `es-ES`
- [ ] Shipped domain packs cover `programming`, `medicine`, `finance`, `academic`, `legal`
- [ ] Every pack entry with `decay_expected = true` has an `audit_date`
- [ ] Quarterly audit runs unattended in CI and files a removal-candidate issue
- [ ] Phase 19 eval confirms measurable WER improvement for each pack on its target corpus
- [ ] Zero Go code changes required to add a new locale or domain (TOML-only contribution path)

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
- Phase 15/16 turn the matrix green on macOS and Windows
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
              Phase 5 + 9 ──► Phase 13 (Audio Preprocessing)
                                        │
                                        ▼
                                 Phase 14 (History)

       Phase 9 (malgo) ✓ ──► Phase 15 (macOS) ──┐
                            ──► Phase 16 (Windows) ─┼──► Phase 17 (Tray)
                                                   │
           Distribution + CI (continuous) ─────┘

Accuracy track (parallel to platform track):

   Phase 12 + 13 ──► Phase 19 (Eval Framework) ──┬─► Phase 20a (Config/API)
                              │                  │
                              ▼                  ▼
                      Phase 19.5 (Privacy) ──► Phase 20b (Pipeline)
                              │                  │
                              ▼                  ▼
                   Phase 14 ──┴──► Phase 20c (Learn-from-corrections)
                                           │
                                           ▼
                              Phase 21 (Testable Levers)
                                           │
                                           ▼
                              Phase 22 (Locale + Domain Packs)
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
13. **Phase 13** — audio preprocessing (high-pass filter + silence trimming)
14. **Phase 14** — history
15. **Phase 15 + 16** — macOS and Windows in parallel
16. **Phase 17** — tray, after both desktop platforms ship
17. **Phase 19** — evaluation framework (gates all accuracy work; can start immediately after Phase 12)
18. **Phase 19.5** — privacy & redaction (hard gate for 20c and 22)
19. **Phase 20a** — config/API accuracy wins (language lock, keyterms, puntuación) — can ship alongside Phase 19
20. **Phase 20b** — pipeline accuracy wins (VAD opt-in, phonetic replace) — gated on capability manifest
21. **Phase 14** — history (unblocks 20c)
22. **Phase 20c** — learn-from-corrections (strictly requires Phase 14 + Phase 19.5)
23. **Phase 21** — testable accuracy levers (strict: requires Phase 19 eval numbers)
24. **Phase 22** — locale + domain packs (thin, declarative, decay-aware, vocab via hint provider)
25. **Continuous** — distribution + CI catches up to every phase
