# Changelog

All notable changes to yap are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

yap is pre-1.0. Until 1.0, breaking changes can land in any release; the
roadmap (see `ROADMAP.md`) is the source of truth for what is planned.

## [Unreleased]

### Phase 7 — CLI Rework

#### Added
- `internal/cli/listen.go` ships the new primary command `yap
  listen`. It owns the wizard-needed-detection, the optional
  `--foreground` mode (calls `daemon.Run` in-process for systemd /
  launchd / containers), and the detached spawn path. The detached
  child is started with `YAP_DAEMON=1` in its environment instead
  of a hidden `--daemon-run` flag — `cmd/yap/main.go` reads that
  env sentinel before cobra parses `os.Args` and calls
  `daemon.Run(...)` directly, keeping the user-visible CLI surface
  free of any internal bootstrap flag.
- `internal/cli/record.go` ships `yap record`, a one-shot record →
  transcribe → transform → inject pipeline that runs without the
  daemon. Flags:
  - `--transform` forces the transform stage on for one invocation,
  - `--out=text` prints the transcribed text to stdout instead of
    invoking the platform injector,
  - `--device <name>` overrides `general.audio_device`,
  - `--max-duration <sec>` overrides `general.max_duration`.
  The command writes its own PID file to
  `$XDG_DATA_HOME/yap/yap-record.pid` so `yap stop` and `yap toggle`
  can target it. SIGINT/SIGTERM cancel the outer context (full
  pipeline teardown); SIGUSR1 cancels only the per-recording context
  so the captured audio still flows through the transcribe and
  inject stages — the same semantic as the daemon's hotkey-release
  handler. The text-out path is implemented by an in-package
  `stdoutInjector` that satisfies `inject.Injector` so the engine
  pipeline stays untouched.
- `internal/cli/transcribe.go` ships `yap transcribe <file.wav>`. It
  builds only a Transcriber via `daemon.NewTranscriber` and prints
  every chunk. Pass `-` to read from stdin; `--json` emits one
  TranscriptChunk JSON object per chunk.
- `internal/cli/transform.go` ships `yap transform [text]`. The
  command runs a single text payload through the configured
  Transformer and prints the rewritten output. `--backend` forces a
  specific backend and implicitly enables the transform stage for
  that invocation; `--system-prompt` overrides the configured prompt;
  `--stdin` reads the payload from stdin.
- `internal/cli/paste.go` ships `yap paste [text]`. The command
  builds only the platform Injector via `daemon.InjectionOptionsFromConfig`
  and calls Inject. It is the canonical Phase 4 inject-layer debug
  command.
- `internal/cli/devices.go` ships `yap devices`. It enumerates audio
  input devices via the new `platform.DeviceLister` interface and
  prints them as a table; the system default is marked with an
  asterisk.
- `internal/cli/recordpid.go` is the small helper that owns the
  record PID file's path resolution, atomic write, read, and
  remove. Centralizing the path constant means a future move
  changes one line.
- `internal/cli/oneshot.go` is the small helper that owns
  `closeIfCloser` (the io.Closer type assertion the one-shot
  commands use to release whisperlocal-style backends),
  `readTextInput` (positional arg / stdin resolution), and
  `openInputFile` (file path / `-` stdin resolution).
- `internal/platform/platform.go` adds the `Device` struct and
  `DeviceLister` interface, plus the `Platform.DeviceLister` field.
- `internal/platform/linux/devices.go` ships `NewDeviceLister`, a
  PortAudio-backed enumerator that returns every input-capable
  device with its host API name and a default-input marker. It is
  wired into `linux.NewPlatform()`.
- `internal/config/version.go` ships `config.Version =
  "0.1.0-dev"`. The constant is the single source of truth for the
  version string reported by `yap status` and the daemon's IPC
  status response. Distribution CI overrides it via `-ldflags
  '-X github.com/hybridz/yap/internal/config.Version=...'` once
  Phase 12 wires release tooling.
- `internal/cli/root.go` exposes
  `cli.ExecuteForTestWithPlatform(p, argv, stdout, stderr)` so
  tests can inject fake platforms (fake recorders, fake injectors,
  fake device listers) without touching the production linux
  factory.

#### Changed
- `internal/daemon/daemon.go` exposes three previously-unexported
  helpers as public functions so the CLI's one-shot commands can
  reuse the same on-disk-config-to-runtime bridges instead of
  duplicating them:
  - `daemon.NewTranscriber(pcfg.TranscriptionConfig) (transcribe.Transcriber, error)`
  - `daemon.NewTransformer(pcfg.TransformConfig) (transform.Transformer, error)`
  - `daemon.InjectionOptionsFromConfig(pcfg.InjectionConfig) platform.InjectionOptions`
  All internal call sites in `daemon.Run` were updated; behavior is
  unchanged.
- `internal/ipc/protocol.go` extends `Response` with optional
  status fields: `Mode`, `ConfigPath`, `Version`, `PID`, `Backend`,
  `Model`. Every new field is `omitempty` so non-status responses
  (toggle, stop) round-trip as the original `{ok,state,error}`
  triple they always emitted.
- `internal/ipc/server.go` changes `SetStatusFn`'s callback
  signature from `func() string` to `func() ipc.Response`. The
  daemon owns building the full struct because it knows every
  field; the server stays a transport that forwards the response
  verbatim. `internal/ipc/server_test.go` was updated to assert on
  the new shape.
- `internal/daemon/daemon.go` SetStatusFn callback now returns the
  full extended Response with `Mode`, `ConfigPath`, `Version`,
  `PID`, `Backend`, and `Model` populated from the loaded config
  and the running process.
- `internal/cli/status.go` prints the daemon's response verbatim
  (it is already JSON with the extended fields). When the daemon
  is not running, the local fallback shape now includes
  `config_path` and `version` so operators can still identify the
  installation.
- `internal/cli/stop.go` is extended to also send SIGTERM to a
  running `yap record` process via its PID file. Both daemon-stop
  and record-stop are best-effort; the command exits 0 if either
  was running and prints "No yap daemon or record process running"
  when neither was. Status messages route through the cobra
  command's writer (not `os.Stdout`) so tests can capture them.
- `internal/cli/toggle.go` is extended to fall back to SIGUSR1 on
  the running `yap record` process when no daemon socket exists.
  The order of preference is: daemon IPC first, then the record
  signal path, then exit 1 with `no daemon and no `yap record`
  process running` if neither exists. SIGUSR1 was chosen to match
  the record command's signal handler, which cancels only the
  per-recording context (so transcribe and inject still run on the
  captured audio). Status messages route through the cobra
  command's writer for the same reason as `stop`.
- `internal/cli/root.go` registers the new commands (`listen`,
  `record`, `transcribe`, `transform`, `paste`, `devices`) and the
  hidden `start` deprecation alias. The hidden `--daemon-run`
  persistent flag and its `RunE` branch are gone.
- `cmd/yap/main.go` is rewritten to handle the `YAP_DAEMON=1` env
  sentinel before delegating to `cli.Execute()`. When the env var
  is set the binary calls `daemon.Run(...)` directly and exits;
  cobra never sees `os.Args` in that path.

#### Removed
- `internal/cli/start.go` is deleted. The `start` command lives on
  as a hidden alias inside `internal/cli/listen.go`, registered by
  `newStartCmd`. The alias prints
  `yap: 'start' is deprecated; use 'yap listen' instead` to stderr
  on every invocation and routes into the same `runListen` handler.
  The alias will be dropped in the next major release.
- The hidden `--daemon-run` persistent flag is removed from
  `internal/cli/root.go`. Detached daemon spawning is now handled
  by the `YAP_DAEMON=1` env sentinel in `cmd/yap/main.go`.

#### Tests
- `internal/cli/listen_test.go` covers `listen --help` and the
  hidden `start` alias's deprecation message + Hidden flag.
- `internal/cli/record_test.go` exercises the happy path (text-out
  and inject), the recorder failure path, the invalid `--out` flag
  validation, and the SIGUSR1 path that cancels only the recording
  context. The tests use a `fakeRecorder` and the `recordingInjector`
  defined in `paste_test.go` instead of touching real audio
  hardware or the OS inject layer.
- `internal/cli/transcribe_test.go` runs the mock transcribe backend
  end-to-end, validates the JSON output mode, and asserts that an
  unknown backend produces a clear error.
- `internal/cli/transform_test.go` runs the passthrough transform
  backend end-to-end, asserts the disabled-backend fallback still
  echoes text, and asserts that an unknown backend override
  produces a clear error.
- `internal/cli/paste_test.go` exercises the inject path with a
  `recordingInjector` (the fake `inject.Injector` reused by record
  tests) and asserts that injector failures surface to the caller.
- `internal/cli/devices_test.go` exercises the table output, the
  nil DeviceLister error, and the lister-error pass-through using
  a `fakeDeviceLister`.
- `internal/cli/status_test.go` covers the no-daemon fallback shape
  (asserts JSON parses, ok=false, version present) and the
  with-daemon happy path (a real `ipc.Server` returning every
  extended field, asserted through the parsed response).
- `internal/cli/stop_test.go` exercises the no-op path, the
  daemon-only path (real ipc.Server), the record-only path (real
  child sleep process + PID file), and the both-paths-at-once
  case. A stale PID file is also exercised.
- `internal/cli/toggle_test.go` exercises the daemon IPC path, the
  record signal path (real child shell process with a USR1 trap),
  and the nothing-running error.
- `internal/ipc/server_test.go` is updated for the new
  `SetStatusFn` signature and asserts every extended Response
  field round-trips through `dispatch`.
- `internal/platform/linux/devices_test.go` asserts that
  `NewDeviceLister` returns a non-nil lister and that
  `NewPlatform` wires it into the composition root.

#### Fixed
- `internal/cli/stop.go` now removes the record PID file
  immediately after delivering SIGTERM so a follow-up `yap stop`
  reports correctly without waiting for the child to clear the
  file itself.

### Phase 6 — Local Whisper Backend

#### Added
- `pkg/yap/transcribe/whisperlocal/` is the local transcription
  backend. It owns a long-lived `whisper-server` subprocess that
  loads the whisper.cpp model into memory once per yap process and
  serves transcription requests over a localhost HTTP API. The
  subprocess is started lazily on the first `Transcribe` call —
  `New` only validates the static config (binary discovery, model
  resolution) so a missing prerequisite surfaces at daemon startup
  rather than at the first hotkey press. The Backend implements
  `io.Closer`; the daemon type-asserts on the registered transcriber
  and defers Close on shutdown.
- `pkg/yap/transcribe/whisperlocal/discover.go` resolves the
  `whisper-server` binary in the documented order:
  `transcription.whisper_server_path` config field →
  `$YAP_WHISPER_SERVER` env var → `exec.LookPath("whisper-server")`
  → `/run/current-system/sw/bin/whisper-server` (Nix profile
  fallback). When none resolve, the error message lists install
  commands for Nix, Arch, Debian, Homebrew, and source builds. The
  same file resolves the model file via
  `transcription.model_path` (the air-gapped escape hatch), then
  the shared cache via `models.Path(cfg.Model)`.
- Subprocess lifecycle is race-free. `Backend.ensureServer` holds
  the mutex during the spawn-or-reuse decision and detects a dead
  child via the `waitDone` channel closed by a goroutine that
  watches `cmd.Wait()`. A dead subprocess is respawned on the next
  Transcribe call; consecutive failures surface to the caller after
  one retry. `Backend.Close` is idempotent and sends SIGTERM, waits
  up to 2 seconds, then SIGKILL. The expected SIGTERM exit is
  swallowed by `closeError` so the daemon does not log a warning
  on every clean shutdown.
- `pkg/yap/transcribe/whisperlocal/models/` is the model cache
  package. It owns CacheDir, Path, Installed, Download, and List.
  Download streams bytes through a `crypto/sha256` hasher into a
  sibling temp file; on success the file is fsync'd and renamed
  into place atomically; on hash mismatch or HTTP error the temp
  file is removed and the cache is unchanged. The package contains
  exactly one mutable global, `downloadClient`, whitelisted by
  name in the noglobals AST guard.
- `pkg/yap/transcribe/whisperlocal/models/manifest.go` ships exactly
  one pinned model — `base.en` — with the SHA256 verified live
  against a download from Hugging Face during Phase 6
  implementation: `a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002`.
  Model names that the manifest deliberately omits (`tiny.en`,
  `small.en`, `medium.en`) return a helpful "not currently pinned"
  error from `lookupManifest` rather than carrying a TODO comment.
  Adding additional sizes is a follow-up change that re-downloads
  each file and computes a fresh SHA.
- `internal/cli/models.go` exposes the model commands:
  `yap models list`, `yap models download <name>`,
  `yap models path [name]`. Every subcommand is a thin wrapper
  over `pkg/yap/transcribe/whisperlocal/models`.
- `pkg/yap/config.TranscriptionConfig` gains
  `WhisperServerPath string` (TOML key
  `whisper_server_path`). The daemon's `newTranscriber` bridge
  threads it through into `transcribe.Config`. The runtime
  `transcribe.Config` gains the matching field so library callers
  can construct the backend without going through the on-disk
  schema.

#### Changed
- **Default backend flip.** `pkg/yap/config.DefaultConfig()` now
  returns `Transcription.Backend = "whisperlocal"` and
  `Transcription.Model = "base.en"`. The first-run wizard's
  `wizardOfferedBackends` list is `["whisperlocal", "groq"]` —
  whisperlocal first, so a user who hits Enter at the prompt gets
  the local backend. The wizard's API-key prompt is now skipped
  for the local backend; the wizard prints a hint pointing at
  `yap models download base.en` instead. The Groq env-key
  short-circuit still works: if `YAP_API_KEY` or `GROQ_API_KEY`
  is set when the wizard runs, it selects the groq backend with
  the env key applied.
- `internal/config/migrate.go` gains a Phase 6 informational
  notice. When a user's nested config explicitly sets
  `transcription.backend = "groq"` (detected via
  `toml.MetaData.IsDefined`), the loader prints a one-time stderr
  line pointing them at the local backend. The notice prints at
  most once per process and reuses the existing `sync.Once`
  pattern; production code never resets it.
- `internal/daemon/daemon.go` side-effect-imports
  `_ "github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal"` so
  the registry knows the backend. The daemon's transcriber-build
  path now type-asserts the registered transcriber against
  `io.Closer` and defers Close on shutdown — backends without
  resources to release pay nothing because the assertion is
  opt-in.
- `flake.nix devShells.default.buildInputs` gains `whisper-cpp`
  so developers in the dev shell have the subprocess available.
  The yap binary itself does not link against whisper.cpp — the
  subprocess is a runtime dependency only, so the static-build
  path is unaffected.
- `nixosModules.nix` is regenerated from the Phase 6 schema
  changes (the new `whisper_server_path` field and the flipped
  default backend / model values). The generator and golden test
  guarantee zero hand-maintained drift.
- `README.md` Privacy section is rewritten. The local-first
  promise is the default again; Groq is described as the
  swappable remote.

#### Findings
- **Subprocess via whisper-server, not CGo bindings.** The
  orchestrator's plan §1.1 considered three integration options
  (whisper.cpp Go bindings, mutablelogic/go-whisper, and a
  standalone whisper-server subprocess) and picked the
  subprocess. Subprocess wins on three axes: it does not pull a
  C++/musl static-link stack into yap's `make build-static`
  pipeline; it inherits whisper.cpp's GPU autodetection from the
  subprocess's compile-time backend list (CPU/Metal/CUDA/Vulkan)
  with zero work in the yap process; and the HTTP boundary makes
  the future streaming path (whisper.cpp may add SSE/WebSocket)
  trivial to wrap behind the existing streaming `Transcriber`
  interface. The cost is the runtime dependency on `whisper-cpp`
  being installed on the user's system, which yap surfaces with
  a clear install-hint error from `discoverServer`.
- **Lazy spawn, persistent subprocess.** New() does NOT fork the
  subprocess; only the first Transcribe() call does. This makes
  daemon startup fast, lets the daemon validate the rest of its
  config before paying for a model load, and means a daemon
  that never receives a hotkey press never spawns whisper-server
  at all (zero-idle-footprint discipline from CLAUDE.md).
- **Real end-to-end smoke test.** During Phase 6 implementation,
  a 2-second 16 kHz mono sine-wave WAV was transcribed end-to-end
  via `whisperlocal.Backend` against the real whisper-server
  binary from the Nix store: spawn → POST /inference → JSON
  decode → close. Wall time was **1.73 seconds** for the first
  call (1532 ms encode + ~200 ms model load + spawn). Subsequent
  calls reuse the in-memory model and return well under 500 ms,
  matching the ARCHITECTURE.md latency target. The smoke test
  used `/tmp/yap-phase6/ggml-base.en.bin` (the live download
  whose SHA we then committed to manifest.go).
- **Config validator stays a leaf.** The plan §3.5 considered
  having `pkg/yap/config.Validate` import the models package to
  reject unknown whisperlocal model names at config load time.
  That import would have made `pkg/yap/config` depend on its own
  consumer's sub-package. The chosen alternative is the
  whisperlocal backend's `New()` (via `resolveModel`) — it
  surfaces the same error at daemon startup with a clear
  message, and the validator stays a pure leaf with no
  cross-package dependencies.
- **The static-build pipeline is broken at HEAD.** Phase 6 makes
  no changes to the static-build path: whisper.cpp is a runtime
  dep, the yap binary does not link against it, and the
  flake.nix change is confined to `devShells.default.buildInputs`.
  However, both `nix develop --command make build-static`
  (musl-gcc not in dev shell) and `nix build .#static`
  (transitive portaudio→jack→dbus build failure under
  `pkgsStatic`) fail at HEAD on this host. This is pre-existing
  tech debt unrelated to Phase 6 and is tracked in the
  Distribution + CI continuous workstream.



#### Added
- `internal/engine/engine.go` is now a true streaming orchestrator.
  The new `Engine.Run(ctx, opts)` entry point drives a fully
  channel-piped pipeline: the recorder feeds an `io.Reader` into
  `Transcriber.Transcribe`, the resulting `<-chan TranscriptChunk` is
  threaded through `Transformer.Transform`, and the final channel is
  handed to `Injector.InjectStream`. There is no batch-collection
  step at any boundary, no `strings.Builder` shim between stages,
  and no engine-level fan-in goroutine — the engine blocks on
  `InjectStream` and the upstream goroutines (transcriber, batch
  helper, transformer) wind down through a shared sub-context the
  engine cancels on every return path.
- `engine.RunOptions` bundles the per-session knobs the daemon and
  the CLI one-shot need: `RecordCtx`, `StartChime`, `StopChime`,
  `WarningChime`, `TimeoutSec`, and `StreamPartials`. Splitting the
  per-recording context out of the long-lived daemon context lets
  the caller stop recording without aborting the in-flight
  transcription / injection — the property the Phase 4 plan called
  out in §1.4.
- `engine.batchChunks` is the only place the engine accumulates
  transcript text. It exists exclusively for the
  `general.stream_partials = false` path and re-emits the collected
  text as a single `IsFinal` chunk on a fresh channel, preserving
  the channel-based invariant end to end (the injector still sees
  a `<-chan TranscriptChunk`, just with one element instead of N).
  Error chunks are forwarded verbatim so transcription failures
  surface even when partials are disabled.
- `engine.New` is now a validating constructor: it returns
  `(*Engine, error)` and rejects nil `Recorder`, `Transcriber`,
  `Transformer`, and `Injector` so misconfiguration surfaces at
  daemon startup rather than at the first hotkey press. The
  `Logger` parameter is optional — a nil logger collapses to
  `slog.New(slog.DiscardHandler)`. The constructor no longer takes
  a `platform.Notifier` either: notifications are owned by the
  caller (the daemon's `startRecording` helper inspects Run's
  wrapped error and routes non-cancellation failures into
  `Notifier.Notify` itself). Routing notifications through the
  engine would have been a second source of truth for "did this
  fail" — the orchestrator's §1.5 explicitly rejected that. There
  is no internal default Transformer: the daemon's `newTransformer`
  helper still resolves `passthrough` when the user disables the
  transform stage, but the engine itself does not import
  `passthrough` (or any other concrete backend).
- `internal/daemon/daemon.go` gains a shared `startRecording`
  helper. Both the hotkey `onPress` callback and the IPC
  `toggleRecording` handler call it, replacing the duplicated
  goroutine shells from Phase 4. The helper owns the recording
  context lifecycle, dispatches the engine call into a goroutine,
  inspects the returned error, and routes non-cancellation
  failures into `Notifier.Notify` so the user gets a desktop toast
  on real pipeline errors while normal cancellation
  (`context.Canceled`, `context.DeadlineExceeded`) stays silent.

#### Changed
- `internal/engine/engine.go` no longer exposes the
  Phase 3/4 `RecordAndInject(daemonCtx, recCtx, ...)` method. It is
  deleted outright (not renamed to a shim) and every call site
  updates to `Engine.Run(ctx, RunOptions{...})`. Pipeline errors
  are now returned from `Run` instead of swallowed via the
  notifier; the daemon inspects the wrapped error and notifies
  selectively.
- `internal/engine/engine_test.go` is rewritten around the
  streaming API. The new `recordingInjector` is a stateful fake
  that implements `inject.Injector` and snapshots every chunk it
  sees on `InjectStream`, so tests can prove the engine actually
  pipes channels through (the `TestEngineRun_StreamingMultiChunk`
  case feeds 3 chunks in and asserts 3 chunks out, in order). A
  `goroutineLeakGuard` helper snapshots `runtime.NumGoroutine()`
  before each test and asserts the count returned to baseline
  after `Run` returned, enforcing the engine's
  zero-goroutine-leak invariant on every test.
- `internal/daemon/daemon.go` constructs the engine via the new
  validating `engine.New` signature, supplying `slog.Default()` as
  the engine logger and propagating any construction error to the
  caller as a wrapped `engine init: ...`. The daemon now stores
  `platform.Notifier` on the `Daemon` struct so the
  `startRecording` helper can route pipeline errors to the user
  without re-reaching into the `Deps` bag from the goroutine
  closure.

#### Removed
- `Engine.RecordAndInject` is deleted. Any future re-introduction
  is detected by the Phase 5 `grep -rn 'RecordAndInject'
  internal/` check, which must return zero results.
- The Phase 3 default-passthrough fallback inside `engine.New` is
  deleted. The engine no longer imports
  `pkg/yap/transform/passthrough`; the daemon's `newTransformer`
  helper is the single source of truth for "transform stage is
  disabled → use passthrough". This is what makes the engine free
  of every concrete backend import.

#### Findings
- **One synchronous block per `Run` call.** The engine spawns at
  most one batchChunks goroutine plus whatever the transcriber and
  transformer spawn internally; the engine blocks on
  `Injector.InjectStream` (which itself blocks until the chunk
  channel closes or ctx cancels), so the function returns only
  after every dependent goroutine has wound down. This is what
  lets the test suite use a goroutine-count diff as the leak
  guard rather than depending on a third-party goleak package.
- **Cancellation and error propagation share one sub-context.**
  `runPipeline` derives a `pipeCtx` from the caller's ctx and
  defers `cancel()`, so any return path — whether nil, a wrapped
  inject error, or a transcription failure — tears down the
  upstream goroutines through that single cancel call. Splitting
  the cancellation tree into multiple sub-contexts was the
  alternative the orchestrator considered and rejected: it makes
  goroutine ownership ambiguous and gives the test suite a much
  harder time proving leaks are absent.
- **`stream_partials = false` still routes through the channel
  pipeline.** A short-circuit that skipped the transformer and
  the injector channel was the obvious "optimization", and is
  exactly the kind of structural exception the Phase 5 plan
  forbids. Routing the batched chunk through the same
  `transformer.Transform → injector.InjectStream` path means a
  future `transform.Transformer` (Phase 8) sees the same chunk
  shape regardless of whether the user has partials enabled, and
  the injector's per-target batching/streaming decision stays
  centralized in the injector instead of being forked across the
  engine.

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
