# yap — Architecture

> The single source of truth for **what yap is**.
> Companion to `ROADMAP.md` (the path). This file describes the system as built.

---

## Product Thesis

yap is a **voice-to-text input method** focused on one job: *let the user drive any app — terminals and Claude Code first — with their voice, reliably and fast*.

The user holds a key, speaks, and text appears in whatever is focused. That's the entire interaction. Everything in this architecture exists to make that moment feel instantaneous and correct, especially in the targets where dictation normally breaks: tmux, kitty, foot, alacritty, wezterm, ghostty, Electron editors (VS Code, Claude Code, Discord, Slack), Monaco/contenteditable fields in browsers, and SSH sessions.

**What yap is not:** a transcription service, an agent tool, an LLM frontend, a recording app, an always-listening assistant, or a GUI application.

---

## Design Axioms

1. **Own the critical path.** Transcription and text injection are the product. They are built from scratch and owned end-to-end. Dependencies are reserved for the non-critical path.
2. **Local first.** Local inference is the default. Remote APIs are swappable fallbacks for users who want top-tier quality or run on constrained hardware. Privacy claims are not aspirations — they are enforced by the architecture.
3. **Composable primitives, not fused pipelines.** `Record`, `Transcribe`, `Transform`, and `Inject` are independent operations exposed through `pkg/yap/`. The engine composes them. So can the user, a script, or a different frontend.
4. **Zero workarounds.** If a problem is hard, it gets a deep module. The text injection layer is the canonical example: not a fallback chain, not a global try-everything-and-hope. It detects the active window, classifies the target, and picks a per-app strategy.
5. **Single source of truth.** The TOML schema, NixOS module, Home Manager module, wizard prompts, validation, and CLI completion all generate from the same Go types in `pkg/yap/config/`. No hand-maintained drift.
6. **No global mutable state.** Every dependency is injected. The daemon, engine, and library packages contain zero package-level mutable variables. Tests never reach for monkey-patching. AST-walking `noglobals_test.go` guards enforce this on every build.
7. **The agent is the target, not the caller.** The agent (Claude Code, a shell, a browser) consumes dictated text. yap does not expose RPC operations to agents — it exposes a focused app to the user's voice. Agent-callable interfaces (MCP, gRPC, etc.) are an explicit non-goal.

---

## The Pipeline

The core pipeline runs once per recording session:

```
record → [audioprep] → transcribe → [batch] → transform → inject
```

- **Record:** Platform recorder captures 16kHz mono 16-bit PCM via malgo (miniaudio). Silence detection optionally auto-stops the recording.
- **Audioprep** (optional): High-pass biquad filter (80Hz cutoff) removes sub-speech rumble. Leading/trailing silence trimming prevents Whisper hallucinations on dead air. Both enabled by default; both skip-safe via config.
- **Transcribe:** Whisper prompt is seeded with project vocabulary from the hint system (Phase 12). Backends emit `TranscriptChunk` values on a channel.
- **Batch** (when `stream_partials = false`): Collapses all chunks into a single `IsFinal` chunk. The channel-based invariant is preserved end-to-end.
- **Transform** (optional): LLM cleans transcription errors, grounded by conversation context from the hint system. Graceful fallback to passthrough on backend failure.
- **Inject:** Detects the active window, classifies the app, selects a per-target strategy, and delivers the text.

The pipeline is channel-based throughout. Cancelling `ctx` at any point drains downstream channels, commits whatever's already been injected, and tears down each backend cleanly.

### Hint System (Context-Aware Pipeline)

Before audio capture, the daemon assembles a two-layer hint `Bundle`:

1. **Vocabulary layer** (always-on when `hint.enabled = true`): Reads project doc files (`CLAUDE.md`, `AGENTS.md`, `README.md`) from cwd walking up to git root. Per-project `.yap.toml` can override with explicit `vocabulary_terms`. The text feeds the Whisper `prompt` parameter, biasing token probabilities toward domain vocabulary ("yap", "whisperlocal", "OSC52" instead of "jump", "whisper local", "Oscar 52").
2. **Conversation layer** (first-match provider walk): Resolves the focused window via the injector's `StrategyResolver`, then walks configured hint providers. The `claudecode` provider reads session JSONL; the `termscroll` provider reads terminal scrollback (kitty via remote control API). Conversation context feeds the transform LLM's system prompt as a reference block.

Both layers are bounded: vocabulary by `vocabulary_max_chars` (default 250), conversation by `conversation_max_chars` (default 8000). A stuck provider is aborted within `timeout_ms` (default 300ms) and never delays audio capture.

---

## Module Layout

```
pkg/yap/                                # Public library — the product surface
├── yap.go                              # Client type, functional options
├── config/                             # Config types + schema + validation
│   └── config.go                       # Single source of truth: TOML, NixOS, wizard, validation
│
├── transcribe/                         # Transcription backend interface + registry
│   ├── transcribe.go                   # Transcriber interface, Options, TranscriptChunk
│   ├── whisperlocal/                   # whisper.cpp subprocess — DEFAULT
│   │   └── models/                     # Model manifest, download, SHA256 verification
│   ├── groq/                           # Groq remote (OpenAI-compatible /audio/transcriptions)
│   ├── openai/                         # Generic OpenAI-compatible endpoint
│   └── mock/                           # Deterministic test backend
│
├── transform/                          # LLM transform backend interface + registry
│   ├── transform.go                    # Transformer interface, Options, Checker
│   ├── passthrough/                    # No-op default (identity)
│   ├── local/                          # Ollama native API (POST /api/chat, NDJSON streaming)
│   ├── openai/                         # OpenAI-compatible (SSE streaming)
│   ├── fallback/                       # Buffered replay decorator (primary → passthrough on error)
│   └── httpstream/                     # Shared HTTP streaming + retry scaffolding
│
├── inject/                             # App-aware text injection — the deep module
│   └── inject.go                       # Injector, StrategyResolver, Target, AppType, Strategy
│
├── hint/                               # Context-aware hint system
│   ├── hint.go                         # Provider interface, Bundle, registry
│   ├── vocab.go                        # ReadVocabularyFiles (walks cwd to git root)
│   ├── project.go                      # .yap.toml per-project overrides
│   ├── trunc.go                        # HeadBytes / TailBytes budget helpers
│   ├── claudecode/                     # Claude Code session JSONL provider
│   └── termscroll/                     # Terminal scrollback provider (kitty strategy)
│
├── audioprep/                          # Audio preprocessing (high-pass filter + silence trim)
│   ├── audioprep.go                    # Processor (satisfies engine.AudioProcessor)
│   ├── biquad.go                       # 2nd-order Butterworth high-pass filter
│   ├── trim.go                         # Leading/trailing silence trimmer (windowed RMS)
│   └── wav.go                          # Self-contained WAV parser/builder (no go-audio dep)
│
└── silence/                            # Amplitude-threshold VAD
    └── silence.go                      # Detector — fires warning + auto-stop callbacks

internal/
├── platform/                           # OS adapters (not library-consumable)
│   ├── platform.go                     # Recorder, ChimePlayer, Hotkey, HotkeyConfig,
│   │                                   # Notifier, DeviceLister, InjectionOptions, Platform
│   └── linux/
│       ├── platform.go                 # NewPlatform() factory
│       ├── audio.go                    # malgo recorder (16kHz mono 16-bit PCM)
│       ├── chime.go                    # malgo chime playback
│       ├── wav.go                      # WAV encoder
│       ├── hotkey.go                   # evdev listener
│       ├── detect_terminal.go          # Wizard key detection fallback
│       ├── devices.go                  # Device enumeration
│       ├── notifier.go                 # beeep / libnotify wrapper
│       └── inject/                     # Linux-specific injection strategies
│           ├── injector.go             # Orchestrator (implements Injector + StrategyResolver)
│           ├── detect.go               # Active-window detection dispatcher
│           ├── detect_sway.go          # swaymsg -t get_tree
│           ├── detect_hyprland.go      # hyprctl activewindow -j
│           ├── detect_wlroots.go       # ext-foreign-toplevel-list-v1
│           ├── detect_x11.go           # xdotool + xprop WM_CLASS
│           ├── classify.go             # WM_CLASS / process-name → AppType allowlists
│           ├── strategy.go             # Strategy selection + ordering
│           ├── tmux.go                 # tmux load-buffer / paste-buffer -p
│           ├── osc52.go                # OSC 52 clipboard sequence (pty resolution via /proc)
│           ├── electron.go             # Clipboard save → set → Ctrl+V → restore
│           ├── wayland.go              # wtype primary, ydotool fallback
│           └── x11.go                  # xdotool type --clearmodifiers (focus polling)
│
├── engine/                             # Pipeline orchestrator — thin, no backend logic
│   └── engine.go                       # Wires Recorder → AudioProcessor → Transcriber →
│                                       # Transformer → Injector. Zero backend imports.
│
├── daemon/                             # Long-running service
│   └── daemon.go                       # Deps injection, hotkey wiring, lifecycle,
│                                       # hint bundle assembly, silence detection wiring,
│                                       # state machine (idle → recording → processing → idle)
│
├── ipc/                                # Daemon ↔ CLI communication
│   └── ndjson.go                       # NDJSON over Unix domain socket
│
├── config/                             # Internal config helpers
│   └── (ConfigPath, Version, migration)
│
├── pidfile/                            # Atomic PID file with flock
├── assets/                             # Embedded chime WAVs
│
├── cli/                                # Thin CLI layer over pkg/yap/
│   ├── root.go                         # Cobra root, platform injection
│   ├── listen.go                       # yap listen [--foreground]
│   ├── record.go                       # yap record [--transform] [--out=text]
│   ├── transcribe.go                   # yap transcribe <file.wav>
│   ├── transform.go                    # yap transform "text"
│   ├── paste.go                        # yap paste "text" (inject layer debug)
│   ├── hint.go                         # yap hint (hint pipeline debug)
│   ├── resolve.go                      # yap resolve (strategy resolution debug)
│   ├── init.go                         # yap init [--backend claude]
│   ├── stop.go                         # yap stop
│   ├── status.go                       # yap status (JSON)
│   ├── toggle.go                       # yap toggle
│   ├── devices.go                      # yap devices
│   ├── models.go                       # yap models list/download/path
│   ├── config_*.go                     # yap config get/set/path/overrides
│   └── oneshot.go                      # Shared one-shot pipeline builder
│
└── cmd/gen-nixos/                      # NixOS/Home Manager module generator
    └── (reads yap:"..." struct tags, renders nixosModules.nix)

cmd/yap/
└── main.go                             # Single binary entry point
```

### Why this layout

- **`pkg/yap/` is public** so the primitives are consumable by other Go programs (alternative frontends, integration tests, scripted pipelines). It is also where the no-global-mutable-state invariant is AST-enforced.
- **`internal/platform/` stays private** because OS adapters are implementation details. The interfaces they implement live in `pkg/yap/inject/` (for injection) and `internal/platform/platform.go` (for OS resources like audio capture and hotkeys).
- **`internal/engine/` is a thin orchestrator.** It composes `pkg/yap/` primitives via channels. It contains zero backend-specific code.
- **`internal/daemon/` is a long-running shell** around the engine. It owns hotkey wiring, lifecycle, hint bundle assembly, and silence detection wiring. The daemon does not transcribe, transform, or inject — it asks the library to.
- **`internal/cli/` is a thin command surface.** Each subcommand is a Cobra wrapper that constructs the right options and calls into `pkg/yap/`. No pipeline logic in the CLI.

---

## Interface Contracts

```go
// pkg/yap/transcribe/transcribe.go
type Transcriber interface {
    Transcribe(ctx context.Context, audio io.Reader, opts Options) (<-chan TranscriptChunk, error)
}

type Options struct {
    Prompt string  // Whisper prompt / initial_prompt — biases token probabilities
}

type TranscriptChunk struct {
    Text     string
    IsFinal  bool          // true for the last chunk in a stream
    Offset   time.Duration // relative to start of audio
    Language string        // detected or configured
    Err      error         // non-nil marks a failed chunk; the stream closes after
}

// pkg/yap/transform/transform.go
type Transformer interface {
    Transform(ctx context.Context, in <-chan TranscriptChunk, opts Options) (<-chan TranscriptChunk, error)
}

type Options struct {
    Context string  // recent text from focused app, prepended to system prompt
}

type Checker interface {
    HealthCheck(ctx context.Context) error  // opt-in startup probe
}

// pkg/yap/inject/inject.go
type Injector interface {
    Inject(ctx context.Context, text string) error
    InjectStream(ctx context.Context, in <-chan TranscriptChunk) error
}

type StrategyResolver interface {
    Resolve(ctx context.Context) (StrategyDecision, error)  // pure query, no side effects
}

type Target struct {
    DisplayServer string   // "wayland" | "x11"
    WindowID      string   // compositor-specific (typically PID as decimal)
    AppClass      string   // WM_CLASS / process name
    AppType       AppType  // terminal | electron | browser | generic
    Tmux          bool     // additive — $TMUX detected
    SSHRemote     bool     // additive — $SSH_TTY detected
}

type Strategy interface {
    Name() string
    Supports(Target) bool
    Deliver(ctx context.Context, text string) error
}

// pkg/yap/hint/hint.go
type Provider interface {
    Name() string
    Supports(target inject.Target) bool
    Fetch(ctx context.Context, target inject.Target) (Bundle, error)
}

type Bundle struct {
    Vocabulary   string  // project docs (daemon base layer)
    Conversation string  // app-specific state (provider)
    Source       string  // provider Name() that produced Conversation
}

// internal/platform/platform.go — OS resource interfaces (not library-consumable)
type Recorder interface {
    Start(ctx context.Context) error  // blocks until ctx cancelled
    Encode() ([]byte, error)          // WAV 16kHz mono 16-bit PCM
    Close()
}

type ChimePlayer interface {
    Play(r io.Reader)  // async, non-blocking
}

type Hotkey interface {
    Listen(ctx context.Context, key KeyCode, onPress, onRelease func())
    Close()
}

type HotkeyConfig interface {
    ValidKey(name string) bool
    ParseKey(name string) (KeyCode, error)
    DetectKey(ctx context.Context) (string, error)  // wizard
}

type Notifier interface {
    Notify(title, message string)  // best-effort, never panics
}

// internal/engine/engine.go — optional preprocessor
type AudioProcessor interface {
    ProcessWAV(wav []byte) ([]byte, error)
}
```

---

## Registry Pattern

Transcription, transform, and hint backends all use the same registration pattern:

```go
// In each backend's init.go:
func init() {
    transcribe.Register("whisperlocal", newBackend)
}

// At runtime:
factory, err := transcribe.Get(cfg.Transcription.Backend)
transcriber, err := factory(transcribe.Config{...})
```

The daemon side-effect-imports every backend sub-package. The registries are append-only `map[string]Factory` guarded by `sync.RWMutex`. The only package-level mutable state in the entire codebase is the three registry maps (`registryMu`, `registry`) in `transcribe`, `transform`, and `hint` — each explicitly whitelisted in the AST guards.

---

## Data Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                          TRIGGER LAYER                               │
│                                                                      │
│   Daemon (hotkey)                CLI (yap record, signals)           │
│         │                              │                             │
│         └──────────────┬───────────────┘                             │
│                        ▼                                             │
│         ┌─── Hint Bundle Assembly ───┐                               │
│         │  vocab: CLAUDE.md/README   │                               │
│         │  conversation: claudecode  │                               │
│         │           or termscroll    │                               │
│         └────────────┬───────────────┘                               │
│                      ▼                                               │
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │                  ENGINE  (internal/engine)                       │ │
│ │                                                                  │ │
│ │  Recorder ──► AudioProc ──► Transcriber ──► Transformer ──► Injector │
│ │  (WAV)      (HPF+trim)    (chunks chan)   (chunks chan)   (per-target)│
│ │      │                         ▲                              │  │ │
│ │      ▼                         │                              ▼  │ │
│ │  Silence ──────────────────────┘                                 │ │
│ │  Detector  (closes audio feed                                    │ │
│ │            on sustained silence)                                 │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│                          pkg/yap (library)                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  transcribe.whisperlocal   transform.passthrough  inject.*     │  │
│  │  transcribe.groq           transform.local        silence.*    │  │
│  │  transcribe.openai         transform.openai       audioprep.*  │  │
│  │  transcribe.mock           transform.fallback     hint.*       │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│                     PLATFORM LAYER  (internal/platform)              │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  Linux: evdev, malgo, Sway/Hyprland/wlroots/X11 detect,       │  │
│  │         OSC52, tmux, wtype, xdotool, beeep                    │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

---

## Transcription Backends

| Backend | Implementation | Notes |
|---|---|---|
| `whisperlocal` (default) | Subprocess via `whisper-server` | Lazy spawn on first transcribe. Crash recovery (one retry). Circuit breaker after 3 consecutive failures. SHA256-verified model auto-download. GPU auto-detection inherited from whisper-server compile flags. |
| `groq` | Remote, OpenAI-compatible `/audio/transcriptions` | Single `IsFinal` chunk. Exponential backoff on 5xx, fail-fast on 4xx. |
| `openai` | Remote, generic OpenAI-compatible endpoint | Same chunk/retry semantics as groq. Works with vLLM, llama.cpp server, etc. |
| `custom` | Same as `openai` | Config-level alias for user-supplied endpoints. |
| `mock` | Deterministic test backend | Emits pre-configured chunks. |

All backends deliver `TranscriptChunk` values on a channel. Non-streaming backends (all current ones) wrap their single result as one `IsFinal` chunk. The `Options.Prompt` parameter feeds Whisper's `prompt` / `initial_prompt` for vocabulary bias.

## Transform Backends

| Backend | Implementation | Notes |
|---|---|---|
| `passthrough` (default) | Identity | Forwards chunks unchanged. Zero allocation. |
| `local` | Ollama native API (`POST /api/chat`) | NDJSON streaming. Default URL `http://localhost:11434`. |
| `openai` | OpenAI-compatible (`POST {api_url}/chat/completions`) | SSE streaming. Rejects empty `api_url` at construction. |

The `fallback` decorator wraps a primary transformer: on failure, it replays buffered input through `passthrough` and notifies the user. Startup health probes run via the optional `Checker` interface.

## Injection Strategies (Linux)

| Strategy | Target | Mechanism |
|---|---|---|
| `tmux` | Terminal + `$TMUX` | `tmux load-buffer - && tmux paste-buffer -p` |
| `osc52` | Terminal | OSC 52 sequence to slave pty (resolved via `/proc` walk) |
| `electron` | Electron / Browser | Clipboard save + set + synthesized Ctrl+V + restore |
| `wayland` | Generic Wayland | `wtype` primary, `ydotool` fallback |
| `x11` | Generic X11 | `xdotool type --clearmodifiers` with focus polling |

Selection: detect target -> apply user `app_overrides` -> apply `default_strategy` -> walk strategies in fixed order -> first `Supports(target)` whose `Deliver` returns nil wins.

### Active-Window Detection

| Compositor | Method |
|---|---|
| Sway | `swaymsg -t get_tree` |
| Hyprland | `hyprctl activewindow -j` |
| wlroots generic | `ext-foreign-toplevel-list-v1` Wayland protocol |
| X11 | `xdotool getactivewindow` + `xprop WM_CLASS` |

### App Classification

- **Terminal:** foot, kitty, alacritty, wezterm, ghostty, xterm, urxvt, konsole, gnome-terminal, xfce4-terminal, tilix, terminator, st
- **Electron:** code, code-oss, vscodium, cursor, claude, claude-desktop, discord, slack, obsidian, notion, element, zed, zed-preview
- **Browser:** firefox, chromium, google-chrome, brave-browser, librewolf, zen, zen-browser

---

## Configuration

### Schema

Config types live in `pkg/yap/config/`. The TOML format, NixOS module, Home Manager module, wizard, and validation are all generated from those types.

```toml
[general]
hotkey = "KEY_RIGHTCTRL"
mode = "hold"                   # "hold" | "toggle"
max_duration = 60
audio_feedback = true
audio_device = ""               # empty = system default
silence_detection = false
silence_threshold = 0.02
silence_duration = 2.0
history = false
stream_partials = true

[transcription]
backend = "whisperlocal"        # "whisperlocal" | "groq" | "openai" | "custom"
model = "base.en"
model_path = ""
whisper_server_path = ""
whisper_threads = 0             # 0 = auto (NumCPU/2, min 1)
whisper_use_gpu = true
language = "en"
api_url = ""
api_key = ""

[transform]
enabled = false
backend = "passthrough"         # "passthrough" | "local" | "openai"
model = ""
system_prompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."
api_url = ""
api_key = ""

[injection]
prefer_osc52 = true
bracketed_paste = true          # retained for schema compat; injector delegates to tmux/terminal
electron_strategy = "clipboard" # "clipboard" | "keystroke"
default_strategy = ""
# app_overrides = [
#   { match = "firefox", strategy = "clipboard" },
#   { match = "kitty",   strategy = "osc52" },
# ]

[hint]
enabled = true
vocabulary_files = ["CLAUDE.md", "AGENTS.md", "README.md"]
providers = ["claudecode", "termscroll"]
vocabulary_max_chars = 250
conversation_max_chars = 8000
timeout_ms = 300

[audio]
high_pass_filter = true
high_pass_cutoff = 80           # Hz; speech fundamentals start at ~85Hz
trim_silence = true
trim_threshold = 0.01
trim_margin_ms = 200

[tray]
enabled = false                 # Phase 17 — not yet implemented
```

### Per-Project Overrides

A `.yap.toml` in the project root (or any ancestor up to git root) can override hint settings per project:

```toml
vocabulary_terms = ["yap", "whisperlocal", "OSC52", "malgo"]
vocabulary_files = ["CLAUDE.md", "AGENTS.md"]
providers = ["claudecode", "termscroll"]
```

### Environment Variables

| Variable | Effect |
|---|---|
| `YAP_API_KEY` | Sets `transcription.api_key` |
| `GROQ_API_KEY` | Compat alias for `transcription.api_key` |
| `YAP_TRANSFORM_API_KEY` | Sets `transform.api_key` |
| `YAP_HOTKEY` | Sets `general.hotkey` |
| `YAP_CONFIG` | Override config file path |
| `YAP_DAEMON` | Internal sentinel — daemon mode (set by `yap listen`) |

### File Locations (Linux)

| Resource | Path |
|---|---|
| Config | `$XDG_CONFIG_HOME/yap/config.toml` (fallback: `/etc/yap/config.toml`) |
| State (PID, socket) | `$XDG_STATE_HOME/yap/` |
| Model cache | `$XDG_CACHE_HOME/yap/models/` |

XDG paths resolved via `github.com/adrg/xdg` to avoid the known `os.UserConfigDir` bug.

---

## CLI Surface

```
yap                            # print help
yap listen                     # start daemon (background, hotkey-driven)
yap listen --foreground        # foreground (for systemd)
yap record                     # one-shot: record → transcribe → (transform) → inject → exit
yap record --transform         # enable transform for this invocation
yap record --out=text          # print to stdout instead of injecting
yap transcribe <file.wav>      # transcribe an existing audio file
yap transform "some text"      # one-shot LLM transform (stdin or arg)
yap paste "some text"          # exercise the inject layer directly (debug)
yap hint                       # debug: print resolved hint bundle for focused window
yap resolve                    # debug: print strategy resolution for focused window
yap init                       # first-run wizard
yap init --backend claude      # zero-config LLM term extraction
yap stop                       # stop daemon or active recording
yap status                     # daemon state, mode, config, version, backend, model (JSON)
yap toggle                     # toggle recording (IPC to daemon or SIGUSR1 to yap record)
yap devices                    # list available audio input devices
yap config get <key>           # dot-notation: yap config get transcription.model
yap config set <key> <value>   # dot-notation
yap config path                # print resolved config file path
yap config overrides           # print per-project .yap.toml overrides
yap models list                # list available whisper models
yap models download <name>     # explicit model download
yap models path                # print model cache path
```

Every command is a thin Cobra wrapper over `pkg/yap/`. Commands contain no pipeline logic.

---

## Platform Layer (Linux)

| Concern | Implementation |
|---|---|
| Audio capture | `gen2brain/malgo` (miniaudio) — PulseAudio/ALSA/PipeWire backends |
| Hotkey | `holoplot/go-evdev` — pure Go, reads `/dev/input/event*` |
| Active-window detection | Sway: `swaymsg`. Hyprland: `hyprctl`. wlroots: `ext-foreign-toplevel-list-v1`. X11: `xdotool` + `xprop`. |
| Terminal injection | OSC 52 via slave pty (/proc walk). tmux: `load-buffer - && paste-buffer -p`. |
| Electron / browser injection | Clipboard save + set + synthesized Ctrl+V + restore. |
| Generic GUI injection | Wayland: `wtype` (primary), `ydotool` (fallback). X11: `xdotool type --clearmodifiers` with focus polling. |
| Clipboard | `atotto/clipboard` — pure Go |
| Notifications | `gen2brain/beeep` — pure Go via dbus |
| IPC | Unix domain socket at `$XDG_STATE_HOME/yap/yap.sock`, NDJSON protocol |
| Daemon lifecycle | Systemd user service or `yap listen` background fork |

---

## Build and Distribution

- **Module path:** `github.com/Enriquefft/yap`
- **Go version:** 1.25+
- **Build:** `nix develop --command go build ./cmd/yap`
- **Static build:** `nix build .#static` produces an 8 MB static ELF binary. `nix develop .#static` provides the musl toolchain for `make build-static`.
- **CGo boundary:** `malgo` (audio) is the only CGo dependency. `whisperlocal` shells out to `whisper-server` at runtime — no CGo. Static linking needs only malgo/miniaudio headers.
- **NixOS/Home Manager modules:** Generated from `pkg/yap/config/` struct tags via `internal/cmd/gen-nixos/`. Golden-file test fails the build if committed modules drift from generator output.

---

## Key Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Transcription default | `whisper.cpp` (subprocess) | Own the critical path. No CGo. GPU auto-detection inherited. Lazy spawn preserves near-zero idle. |
| Transcription interface | Streaming chunks + per-call Options | Future-proof for streaming. Options.Prompt varies per recording (focused window differs). |
| Transform backends | Local (Ollama native) and OpenAI-compatible | Local LLMs are first-class. Graceful fallback to passthrough on failure. |
| Text injection | Deep module with per-target strategies | Detect, classify, select. Not a fallback chain. |
| Terminal delivery | OSC52 (pty-resolved), tmux-aware | Works over SSH, works in modern terminals, works inside tmux. |
| Audio library | `gen2brain/malgo` (miniaudio) | Single C header, static-link friendly, native backends per OS. |
| Audio preprocessing | High-pass biquad + silence trim only | Research shows noise reduction degrades Whisper by up to 46.6%. These two are empirically safe. |
| Context-aware pipeline | Vocabulary (Whisper prompt) + Conversation (LLM context) | Fixes domain-term misrecognition at source. Two orthogonal layers with independent budgets. |
| Library surface | Public `pkg/yap/` | Composability, testability, no package-level-mutable workarounds. |
| Config format | Nested TOML, schema-driven | Single source of truth for TOML + NixOS + Home Manager + wizard + validation. |
| Backend selection | Registry pattern (Register/Get) | Side-effect imports at daemon level. Engine has zero backend imports. |
| State enforcement | AST `noglobals_test.go` guards | Every package forbids package-level `var` except explicit whitelist (registry maps). |
| Test injection | Constructor injection only | Zero package-level mutable state. Tests pass real or mock backends explicitly. |

---

## Non-Goals

These are deliberately excluded:

- **MCP server / agent-callable RPC.** The agent is the *target* of dictation, not a caller.
- **Always-listening / wake word.** Contradicts the near-zero-idle thesis.
- **Cloud sync, accounts, telemetry.** No backend, no accounts, no analytics. Ever.
- **Built-in speech model training.** Users who want a custom model point `transcription.model_path` at it.
- **Noise reduction preprocessing.** Research (arXiv:2512.17562) shows it degrades Whisper accuracy by up to 46.6%. High-pass filter and silence trimming are the only safe operations.
