# yap — Architecture

> The single source of truth for **what yap is**.
> Companion to `ROADMAP.md` (the path). This file describes the destination.

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
5. **Single source of truth.** The TOML schema, NixOS module, wizard prompts, validation, and CLI completion all generate from the same Go types in `pkg/yap/config/`. No hand-maintained drift.
6. **No global mutable state.** Every dependency is injected. The daemon, engine, and library packages contain zero package-level mutable variables. Tests never reach for monkey-patching.
7. **The agent is the target, not the caller.** The agent (Claude Code, a shell, a browser) consumes dictated text. yap does not expose RPC operations to agents — it exposes a focused app to the user's voice. Agent-callable interfaces (MCP, gRPC, etc.) are an explicit non-goal.

---

## The Three Critical-Path Pillars

Every line of code in yap serves one of three pillars. Anything that doesn't is polish.

### Pillar 1 — Local, low-latency transcription

Transcription is 100% of the product's value. It must be local by default and fast enough that the user forgets the machine is there.

- **Default backend:** `whisper.cpp` (local). Streaming output where the binding supports it.
- **Swappable backends:** Groq, OpenAI, any OpenAI-compatible HTTP endpoint, custom backends via the `Transcriber` interface.
- **Latency target:** under 500ms end-to-end for a 5-second clip on a modern laptop CPU, fully local.
- **Streaming first:** the `Transcriber` interface emits chunks on a channel. Non-streaming backends wrap their batch output as a single `IsFinal` chunk; streaming backends deliver progressively.

### Pillar 2 — App-aware text injection

The "text appears where I'm typing" promise is the entire UX. Linux has no unified input API. Solving that is not plumbing — it's the deep module where most of the engineering value lives.

- **Active-window detection** per display server: Sway via `swaymsg`, Hyprland via `hyprctl`, generic wlroots via `ext-foreign-toplevel-list-v1`, X11 via `xdotool` + `xprop`.
- **App classification** via WM_CLASS / process name allowlists: `AppTerminal`, `AppElectron`, `AppBrowser`, `AppGeneric`. Additive flags `Target.Tmux` and `Target.SSHRemote` when `$TMUX` / `$SSH_TTY` are detected.
- **Per-app strategies**:
  - Terminals → OSC52 clipboard sequence, with tmux-aware passthrough via `paste-buffer -p`.
  - Electron / browsers → clipboard save + synthesized Ctrl+V, with Monaco autocomplete-popup workarounds.
  - Generic GUI → wtype on Wayland, `xdotool type --clearmodifiers` on X11, both with focus-acquisition polling (never hard-coded sleeps).
- **Strategy selection** is explicit. The user can override per-app via config. Selection is logged in the audit trail.
- **Streaming injection** for partial-safe targets: GUI textboxes receive partial chunks as they arrive; terminals batch until the final chunk.

### Pillar 3 — Daemon + hotkey, as thin as possible

The daemon exists to amortize expensive resources (audio device, loaded whisper model, hotkey listener). It is not where pipeline logic lives.

- The daemon owns: hotkey listener, audio device handle, loaded model, IPC server.
- The daemon does not own: transcription logic, transform logic, injection logic. All of those are imported from `pkg/yap/`.
- The CLI's `yap record` one-shot uses the same `pkg/yap/` primitives directly, without going through the daemon.

---

## Module Layout

```
pkg/yap/                                # Public library — the product surface
├── yap.go                              # Client type, functional options
├── config/                             # Config types + schema + validation
│   └── config.go                       # Single source of truth: TOML, NixOS, wizard, validation all generate from here
│
├── transcribe/                         # Transcription backend interface + implementations
│   ├── transcribe.go                   # Transcriber interface (streaming chunks)
│   ├── whisperlocal/                   # whisper.cpp — DEFAULT
│   ├── groq/                           # Groq remote (OpenAI-compatible /audio/transcriptions)
│   ├── openai/                         # Generic OpenAI-compatible (vLLM, llama.cpp server, custom)
│   └── mock/                           # Deterministic test backend
│
├── transform/                          # LLM transform backend interface + implementations
│   ├── transform.go                    # Transformer interface (streaming chunks in, chunks out)
│   ├── local/                          # Ollama / llama.cpp server
│   ├── openai/                         # OpenAI-compatible remote
│   └── passthrough/                    # No-op default
│
├── inject/                             # App-aware text injection — the deep module
│   ├── inject.go                       # Injector interface, Target type, AppType, Strategy interface
│   ├── target.go                       # Active-window detection orchestration
│   ├── classify.go                     # WM_CLASS / process-name → AppType allowlists
│   └── registry.go                     # Strategy registration + selection
│
├── silence/                            # Amplitude-threshold VAD
│   └── silence.go                      # Detector interface + implementation
│
└── history/                            # Append-only JSONL transcription log
    └── history.go                      # Writer + Query API

internal/
├── platform/                           # OS adapters (not library-consumable)
│   ├── platform.go                     # Recorder, ChimePlayer, Hotkey, HotkeyConfig, Notifier interfaces
│   ├── linux/
│   │   ├── platform.go                 # NewPlatform() factory
│   │   ├── audio.go                    # malgo recorder
│   │   ├── chime.go                    # malgo chime playback
│   │   ├── wav.go                      # 16kHz mono 16-bit WAV encoder
│   │   ├── hotkey.go                   # evdev listener
│   │   ├── detect_terminal.go          # wizard key detection fallback
│   │   ├── notifier.go                 # beeep / libnotify wrapper
│   │   └── inject/                     # Linux-specific injection strategies
│   │       ├── detect.go               # Sway/Hyprland/wlroots/X11 active-window detection
│   │       ├── osc52.go                # Terminal OSC52 clipboard
│   │       ├── tmux.go                 # tmux load-buffer / paste-buffer -p
│   │       ├── electron.go             # Clipboard + synthesized paste
│   │       ├── wayland.go              # wtype primary, ydotool fallback
│   │       └── x11.go                  # xdotool with focus polling
│   ├── darwin/
│   │   ├── platform.go
│   │   ├── audio.go                    # malgo CoreAudio
│   │   ├── chime.go
│   │   ├── hotkey.go                   # CGEventTap + Accessibility permission
│   │   ├── notifier.go                 # osascript / UserNotifications
│   │   └── inject/
│   │       ├── detect.go               # NSWorkspace.frontmostApplication
│   │       ├── terminal.go             # Terminal.app, iTerm2, Alacritty, Kitty, Wezterm
│   │       ├── electron.go             # Cmd+V via CGEvent or AppleScript
│   │       └── generic.go              # AppleScript keystroke / CGEvent
│   └── windows/
│       ├── platform.go
│       ├── audio.go                    # malgo WASAPI
│       ├── chime.go
│       ├── hotkey.go                   # SetWindowsHookEx WH_KEYBOARD_LL
│       ├── notifier.go                 # Windows toast notifications
│       └── inject/
│           ├── detect.go               # GetForegroundWindow + GetModuleFileNameEx
│           ├── terminal.go             # Windows Terminal, conhost, wezterm
│           ├── electron.go             # SendInput Ctrl+V
│           └── generic.go              # SendInput unicode + clipboard backing
│
├── engine/                             # Pipeline orchestrator — thin, no backend logic
│   └── engine.go                       # Wires Recorder + Transcriber + Transformer + Injector
│
├── daemon/                             # Long-running service
│   ├── daemon.go                       # Deps injection, hotkey wiring, lifecycle
│   └── tray.go                         # Optional, opt-in
│
├── ipc/                                # Daemon ↔ CLI communication
│   ├── ndjson.go                       # NDJSON protocol
│   ├── ipc_unix.go                     # Unix domain socket
│   └── ipc_windows.go                  # Named pipes
│
├── pidfile/                            # Atomic PID file lifecycle
├── assets/                             # Embedded chime WAVs + small whisper model
└── cli/                                # Thin CLI layer over pkg/yap/
    ├── root.go
    ├── listen.go                       # yap listen [--foreground]
    ├── record.go                       # yap record [--transform] [--out=text]
    ├── transcribe.go                   # yap transcribe <file.wav>
    ├── transform.go                    # yap transform "text"
    ├── paste.go                        # yap paste "text" (debug the inject layer directly)
    ├── stop.go
    ├── status.go
    ├── toggle.go
    ├── devices.go                      # yap devices
    ├── history_*.go                    # yap history list/search/clear/path
    └── config_*.go                     # yap config get/set/path (dot-notation)

cmd/yap/
└── main.go                             # Single binary entry point, dispatches by GOOS
```

### Why this layout

- **`pkg/yap/` is public** so the primitives are consumable by other Go programs (alternative frontends, integration tests, scripted pipelines). It is also where the package-level-mutable-state antipattern is forbidden.
- **`internal/platform/` stays private** because OS adapters are implementation details. The interfaces they implement live in `pkg/yap/inject/` (for injection) and `internal/platform/platform.go` (for OS resources like audio capture and hotkeys).
- **`internal/engine/` is a thin orchestrator.** It composes `pkg/yap/` primitives via channels. It contains zero backend-specific code.
- **`internal/daemon/` is a long-running shell** around the engine. It owns hotkey wiring and lifecycle. The daemon does not transcribe, transform, or inject — it asks the library to.
- **`internal/cli/` is a thin command surface.** Each subcommand is a Cobra-style wrapper that constructs the right options and calls into `pkg/yap/`. No pipeline logic in the CLI either.

---

## Public Library Surface

The `pkg/yap/` packages are the contract. A third-party Go program can use yap as a library in a few lines:

```go
import (
    "github.com/Enriquefft/yap/pkg/yap"
    "github.com/Enriquefft/yap/pkg/yap/transcribe/whisperlocal"
)

client, err := yap.New(
    yap.WithTranscriber(whisperlocal.New(whisperlocal.Options{Model: "base.en"})),
)
text, err := client.TranscribeFile(ctx, "recording.wav")
```

Or compose primitives directly:

```go
import "github.com/Enriquefft/yap/pkg/yap/transcribe/groq"

t := groq.New(groq.Options{APIKey: os.Getenv("GROQ_API_KEY")})
chunks, err := t.Transcribe(ctx, audioReader)
for chunk := range chunks {
    fmt.Println(chunk.Text)
}
```

---

## Interface Contracts

```go
// pkg/yap/transcribe/transcribe.go
type Transcriber interface {
    // Transcribe consumes audio and emits transcript chunks.
    // Non-streaming backends deliver a single IsFinal chunk.
    // The channel is closed when the backend is done or ctx is cancelled.
    Transcribe(ctx context.Context, audio io.Reader) (<-chan TranscriptChunk, error)
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
    // Transform consumes transcript chunks and emits transformed chunks.
    // Streaming backends emit per-chunk; non-streaming accumulate and emit once on IsFinal.
    Transform(ctx context.Context, in <-chan TranscriptChunk) (<-chan TranscriptChunk, error)
}

// pkg/yap/inject/inject.go
type Injector interface {
    // Inject delivers text to the currently focused application.
    // Detects the active target, classifies the app, picks the strategy.
    // Returns an error only if every applicable strategy failed.
    Inject(ctx context.Context, text string) error

    // InjectStream delivers text as it arrives.
    // Partial-safe targets (GUI textboxes, clipboard-backed strategies) receive partials.
    // Unsafe targets (terminals, shells) batch until the stream ends.
    InjectStream(ctx context.Context, in <-chan TranscriptChunk) error
}

type Target struct {
    DisplayServer string  // "wayland" | "x11" | "macos" | "windows"
    WindowID      string  // compositor / OS-specific identifier
    AppClass      string  // WM_CLASS / bundle ID / process name
    AppType       AppType // classified type
    Tmux          bool    // additive — set when $TMUX is detected
    SSHRemote     bool    // additive — set when $SSH_TTY is detected
}

type AppType int
const (
    AppGeneric AppType = iota
    AppTerminal
    AppElectron
    AppBrowser
)

type Strategy interface {
    Name() string
    Supports(Target) bool
    Deliver(ctx context.Context, text string) error
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
    Listen(ctx context.Context, combo []KeyCode, onPress, onRelease func())
    Close()
}

type HotkeyConfig interface {
    ValidKey(name string) bool
    ParseKey(name string) (KeyCode, error)
    DetectCombo(ctx context.Context) ([]string, error)  // wizard
}

type Notifier interface {
    Notify(title, message string)  // best-effort, never panics
}
```

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
│ ┌──────────────────────────────────────────────────────────────────┐ │
│ │                  ENGINE  (internal/engine)                       │ │
│ │                                                                  │ │
│ │  Recorder ──► Transcriber ──► Transformer ──► Injector           │ │
│ │  (audio)     (chunks chan)   (chunks chan)   (per-target)        │ │
│ │      │              ▲                              │             │ │
│ │      ▼              │                              ▼             │ │
│ │  Silence ───────────┘                          History           │ │
│ │  Detector  (closes audio feed                  (optional log)    │ │
│ │            on sustained silence)                                 │ │
│ └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│                          pkg/yap (library)                           │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  transcribe.whisperlocal   transform.local     inject.*        │  │
│  │  transcribe.groq           transform.openai    silence.*       │  │
│  │  transcribe.openai         transform.passthrough  history.*    │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│                     PLATFORM LAYER  (internal/platform)              │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  Linux: evdev, malgo, Sway/Hyprland/X11 detect, OSC52, wtype   │  │
│  │  macOS: CGEventTap, CoreAudio, NSWorkspace, AppleScript, CG    │  │
│  │  Windows: WinHook, WASAPI, GetForegroundWindow, SendInput      │  │
│  └────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────┘
```

The pipeline is channel-based throughout. Cancelling `ctx` at any point drains downstream channels, commits whatever's already been injected, and tears down each backend cleanly.

---

## Configuration

### Schema

The config types live in `pkg/yap/config/`. The TOML format below, the NixOS module, the first-run wizard, and the validation logic are all generated from those types — no hand-maintained drift.

```toml
[general]
hotkey = "KEY_RIGHTCTRL"        # single key or combo: "KEY_LEFTSHIFT+KEY_SPACE"
mode = "hold"                   # "hold" | "toggle"
max_duration = 60               # max recording seconds
audio_feedback = true           # chime on start/stop
audio_device = ""               # empty = system default
silence_detection = false
silence_threshold = 0.02        # amplitude threshold (0.0–1.0)
silence_duration = 2.0          # seconds of silence before auto-stop
history = false                 # append every transcription to history.jsonl
stream_partials = true          # paste partials to safe targets while speaking

[transcription]
backend = "whisperlocal"        # "whisperlocal" | "groq" | "openai" | "custom"
model = "base.en"               # for whisperlocal: tiny.en | base.en | small.en | medium.en
model_path = ""                 # explicit path; empty = auto-download to cache
language = "en"                 # empty = auto-detect
prompt = ""                     # context hint

# Used only when backend is a remote API:
api_url = ""                    # required for groq/openai/custom
api_key = ""                    # or env: YAP_API_KEY / GROQ_API_KEY

[transform]
enabled = false
backend = "passthrough"         # "passthrough" | "local" | "openai"
model = ""                      # e.g. "llama3.2:3b" for local, "gpt-4o-mini" for openai
system_prompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."
api_url = ""                    # for openai/local (Ollama = http://localhost:11434/v1)
api_key = ""                    # or env: YAP_TRANSFORM_API_KEY

[injection]
prefer_osc52 = true             # use OSC52 for terminals when supported
bracketed_paste = true          # wrap multi-line text for shells
electron_strategy = "clipboard" # "clipboard" | "keystroke"
default_strategy = ""           # fallback strategy when no app_override matches; empty = natural order
# Per-app overrides, evaluated in order, first match wins:
# app_overrides = [
#   { match = "firefox", strategy = "clipboard" },
#   { match = "kitty",   strategy = "osc52" },
#   { match = "code",    strategy = "clipboard" },
# ]

[tray]
enabled = false
```

### Environment Variables

| Variable | Effect | Notes |
|---|---|---|
| `YAP_API_KEY` | Sets `transcription.api_key` | Primary. Used by remote backends. |
| `GROQ_API_KEY` | Sets `transcription.api_key` | Compat alias. |
| `YAP_TRANSFORM_API_KEY` | Sets `transform.api_key` | For the LLM transform backend. |
| `YAP_HOTKEY` | Sets `general.hotkey` | Compat alias. |
| `YAP_CONFIG` | Override config file path | For testing or alternate profiles. |
| `YAP_DAEMON` | Internal sentinel — daemon mode | Set by `yap listen` when forking the child process. Not for users. |

### File Locations

| Resource | Linux | macOS | Windows |
|---|---|---|---|
| Config | `$XDG_CONFIG_HOME/yap/config.toml` | `~/Library/Application Support/yap/config.toml` | `%APPDATA%/yap/config.toml` |
| State (PID, socket) | `$XDG_STATE_HOME/yap/` | `~/Library/Application Support/yap/` | `%LOCALAPPDATA%/yap/` |
| History | `$XDG_DATA_HOME/yap/history.jsonl` | `~/Library/Application Support/yap/history.jsonl` | `%LOCALAPPDATA%/yap/history.jsonl` |
| Model cache | `$XDG_CACHE_HOME/yap/models/` | `~/Library/Caches/yap/models/` | `%LOCALAPPDATA%/yap/Cache/models/` |

XDG paths resolved via `github.com/adrg/xdg` to avoid the known `os.UserConfigDir` bug.

---

## CLI Surface

```
yap                            # print help
yap listen                     # start daemon (background, hotkey-driven)
yap listen --foreground        # foreground (for systemd / launchd / containers)
yap record                     # one-shot: record → transcribe → (transform) → inject → exit
yap record --transform         # enable transform for this invocation
yap record --out=text          # print to stdout instead of injecting
yap transcribe <file.wav>      # transcribe an existing audio file
yap transform "some text"      # one-shot LLM transform (stdin or arg)
yap paste "some text"          # exercise the inject layer directly (debug)
yap stop                       # stop daemon or active recording
yap status                     # daemon state, mode, config path, version, backend, model (JSON)
yap toggle                     # toggle recording (IPC to daemon, or signal to standalone)
yap devices                    # list available audio input devices
yap config get <key>           # dot-notation: yap config get transcription.model
yap config set <key> <value>   # dot-notation
yap config path                # print resolved config file path
yap models list                # list available whisper models
yap models download <name>     # explicit model download
yap models path                # print model cache path
yap history list               # last N transcriptions (default 20)
yap history search <query>     # substring or regex
yap history clear              # truncate (with confirmation)
yap history path               # print history file path
```

Every command is a thin Cobra wrapper over `pkg/yap/`. Commands contain no pipeline logic.

---

## Platform Layer

### Linux

| Concern | Implementation |
|---|---|
| Audio capture | `gen2brain/malgo` (miniaudio) — PulseAudio/ALSA/PipeWire backends |
| Hotkey | `holoplot/go-evdev` — pure Go, reads `/dev/input/event*` |
| Active-window detection | Sway: `swaymsg -t get_tree`. Hyprland: `hyprctl activewindow -j`. wlroots: `ext-foreign-toplevel-list-v1`. X11: `xdotool getactivewindow` + `xprop WM_CLASS`. |
| Terminal injection | OSC 52 (`\x1b]52;c;<base64>\x07`). tmux: `tmux load-buffer - && tmux paste-buffer -p`. |
| Electron / browser injection | Clipboard save → set → synthesized Ctrl+V → restore. Monaco anti-autocomplete shim opt-in per-app. |
| Generic GUI injection | Wayland: `wtype` (primary), `ydotool` (fallback with socket check). X11: `xdotool type --clearmodifiers` with focus polling. |
| Clipboard | `atotto/clipboard` — pure Go |
| Notifications | `gen2brain/beeep` — pure Go via dbus |
| IPC | Unix domain socket at `$XDG_STATE_HOME/yap/yap.sock`, NDJSON protocol |
| Daemon lifecycle | Systemd user service or `yap listen` background fork |

### macOS

| Concern | Implementation |
|---|---|
| Audio capture | `gen2brain/malgo` — CoreAudio backend |
| Hotkey | `CGEventTap` global key monitor; requires Accessibility permission (prompted on first run) |
| Active-window detection | `NSWorkspace.frontmostApplication` + `CGWindowListCopyWindowInfo` |
| Terminal injection | OSC 52 for Terminal.app, iTerm2, Alacritty, Kitty, Wezterm, Ghostty |
| Electron / browser injection | Clipboard + synthesized `Cmd+V` via `CGEventCreateKeyboardEvent` |
| Generic GUI injection | AppleScript `tell application "System Events" to keystroke` or CGEvent fallback |
| Notifications | `osascript display notification` or UserNotifications framework |
| IPC | Unix domain socket |
| Daemon lifecycle | `launchd` plist generated by `yap listen --install` |

### Windows

| Concern | Implementation |
|---|---|
| Audio capture | `gen2brain/malgo` — WASAPI backend |
| Hotkey | `SetWindowsHookEx(WH_KEYBOARD_LL, ...)` low-level keyboard hook |
| Active-window detection | `GetForegroundWindow` + `GetWindowThreadProcessId` + `GetModuleFileNameEx` |
| Terminal injection | OSC 52 for Windows Terminal, conhost, Wezterm |
| Electron / browser injection | Clipboard + `SendInput` `Ctrl+V` |
| Generic GUI injection | `SendInput` Unicode characters with clipboard backing |
| Notifications | Windows toast notifications |
| IPC | Named pipe at `\\.\pipe\yap` |
| Daemon lifecycle | Windows service or startup-folder shortcut |

---

## Build, Distribution, and Static Binary

- **Module path:** `github.com/Enriquefft/yap`
- **Go version:** 1.25+
- **Build:** `nix develop --command go build ./cmd/yap`
- **Static build:** `make build-static` produces a single static binary verified by `ldd` to have no dynamic dependencies on Linux. **Status: blocked** — musl toolchain not wired into the Nix dev shell; tracked under Distribution + CI.
- **CGo boundary:** `malgo` (audio) is the only CGo dependency. `whisperlocal` shells out to `whisper-server` at runtime — no CGo. Static linking needs only `malgo`/miniaudio headers.
- **Cross-compilation:** GitHub Actions matrix builds `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64`. Each target runs the test suite. **Status: not yet wired** — pending Distribution + CI workstream.
- **Distribution channels:** GitHub Releases (canonical), curl install script, Nix flake, Homebrew formula, AUR PKGBUILD, `go install github.com/Enriquefft/yap/cmd/yap@latest`.

---

## Key Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Transcription default | `whisper.cpp` local | Own the critical path. Latency. Privacy. |
| Transcription bootstrap | Groq remote | Quick to ship; swappable behind the same interface. |
| Transcription interface | Streaming chunks | Future-proof for whisper.cpp server mode and OpenAI streaming. Non-streaming backends wrap as a single-chunk channel. |
| Transform backends | Local (Ollama, llama.cpp) and OpenAI-compatible peers | Local LLMs are first-class, not "another endpoint." |
| Text injection | Deep module with per-target strategies | The fallback chain is the symptom; this is the fix. |
| Terminal delivery | OSC52, tmux-aware (`paste-buffer -p`) | Works over SSH, works in modern terminals, works inside tmux. |
| Electron delivery | Clipboard + synthesized paste | Most reliable path for Monaco, contenteditable fields, Claude Code. |
| Audio library | `gen2brain/malgo` (miniaudio) | Single C header, static-link friendly, native backends per OS. |
| Library surface | Public `pkg/yap/` | Composability, testability, no package-level-mutable workarounds. |
| Engine location | Internal, thin orchestrator | Composes `pkg/yap/` primitives via channels; zero backend-specific logic. |
| Config format | Nested TOML, schema-driven | Single source of truth for TOML + NixOS + wizard + validation. |
| IPC (Unix) | NDJSON over Unix domain socket | Simple, pure Go, sufficient. |
| IPC (Windows) | Named pipes | Native Windows equivalent. |
| Linux hotkey | `holoplot/go-evdev` | Pure Go. Replaces unmaintained CGo `gvalkov/golang-evdev`. |
| Linux clipboard | `atotto/clipboard` | Pure Go. Replaces `golang-design/clipboard` (CGo). |
| Linux notifier | `gen2brain/beeep` | Pure Go via dbus. |
| XDG paths | `adrg/xdg` | `os.UserConfigDir` has a known XDG_CONFIG_HOME bug. |
| Silence detection | Amplitude threshold | Simple, no ML model. Works for the "pause means done" use case. |
| History format | JSONL | Append-only, grep-friendly, no database. |
| Test injection | Constructor injection only | Zero package-level mutable state. Tests pass real or mock backends explicitly. |

---

## Non-Goals

These are deliberately excluded. Each was considered and rejected with reason:

- **MCP server / agent-callable RPC.** The agent (Claude Code, a shell, a browser) is the *target* of dictation, not a caller. An MCP layer would expose operations nothing calls. Excluded on purpose.
- **Always-listening / wake word.** Contradicts the near-zero-idle thesis. Excluded on purpose.
- **Real-time streaming display during recording** beyond partial chunk injection. Partials land in safe targets (textboxes); terminals receive the final block. Anything more would be an animation, not a feature.
- **Built-in GUI application.** The human-facing UI is the hotkey + the focused app. The optional system tray is an indicator, not an app.
- **Cloud sync, accounts, telemetry.** No backend, no accounts, no analytics. Ever.
- **Built-in speech model training or fine-tuning.** Use a pre-trained whisper model. Users who want a custom model bring their own and point `transcription.model_path` at it.
- **GUI editor for transcription history.** History is a JSONL file. Read it with `yap history list`, grep, jq, or your editor of choice.
