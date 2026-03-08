# Architecture Research — yap

## Component Boundaries

```
cmd/yap          — CLI entry point; cobra commands; dispatches to daemon or runs one-shot
daemon           — Long-running background process; owns event loop; goroutine supervisor
ipc              — Unix socket server (daemon-side) + client (CLI-side); newline-delimited JSON
hotkey           — evdev device scanner + key event listener; emits hold/release events
audio            — PortAudio stream management; ring buffer; WAV accumulation
transcription    — Groq API client; multipart POST; returns transcript string
output           — Paste-at-cursor driver (xdotool/ydotool); clipboard save/restore
notify           — OS notification via beeep/libnotify
config           — TOML loader; XDG paths; env var overrides; first-run wizard
platform         — Abstraction interfaces (HotkeyListener, OutputDriver, Notifier); Linux impls
assets           — Embedded WAV chimes via //go:embed
```

## Data Flow

**Recording Flow (hotkey held)**
```
evdev key event → hotkey.Hold() → audio.StartRecording()
    → PortAudio stream → ring buffer → WAV accumulation
    → assets.PlayChime(start) [async]
```

**Transcription Flow (hotkey released)**
```
evdev key event → hotkey.Release() → audio.StopRecording()
    → assets.PlayChime(stop) [async]
    → wav.Encode(pcm) → transcription.Submit(wav)
    → Groq API POST → transcript string
    → output.SaveClipboard()
    → output.PasteAtCursor(transcript)
    → output.RestoreClipboard()
```

**IPC Flow (CLI command)**
```
yap stop/toggle → ipc.Client.Send(cmd) → Unix socket
    → ipc.Server.Recv() → daemon.Dispatch(cmd) → response
    → ipc.Client.Recv() → CLI exits with status
```

## IPC Mechanism

**Choice: Unix Domain Socket (`SOCK_STREAM`) at `$XDG_DATA_HOME/yap/yap.sock`**

Protocol: newline-delimited JSON — each message is a single JSON object terminated with `\n`.

```json
{"cmd": "stop"}
{"cmd": "toggle"}
{"cmd": "status"}
```

Response:
```json
{"ok": true, "state": "idle"}
{"ok": false, "error": "not running"}
```

**Why Unix socket (not signals, D-Bus, gRPC, TCP):**
- Standard `net` package — zero extra dependencies
- Filesystem permission security (socket mode 0600)
- Structured data (signals can't carry payloads)
- Docker precedent (proven pattern for single-machine IPC)
- D-Bus adds `godbus/dbus` complexity and session bus assumptions
- gRPC adds significant binary size
- TCP requires port allocation and firewall considerations

## Platform Abstraction

Three interfaces in `internal/platform/`:

```go
// HotkeyListener — platform-specific key event source
type HotkeyListener interface {
    Listen(ctx context.Context) (<-chan KeyEvent, error)
    Close() error
}

// OutputDriver — paste at cursor + clipboard
type OutputDriver interface {
    PasteText(text string) error
    SaveClipboard() (string, error)
    RestoreClipboard(content string) error
}

// Notifier — OS notifications
type Notifier interface {
    Notify(title, body string) error
    NotifyError(title, body string) error
}
```

Linux implementations selected via `//go:build linux` build tags. X11/Wayland runtime detection via `WAYLAND_DISPLAY` env var. Darwin/Windows stubs return `ErrNotImplemented` (v0.3 scope).

## Suggested Build Order

```
Phase 1 — Foundation
    config + XDG paths + TOML + env vars
    first-run wizard
    project scaffold (go.mod, cobra CLI structure)
    embedded assets (//go:embed WAV files)
    Nix flake (portaudio buildInputs, CGo setup)

Phase 2 — Audio Pipeline
    PortAudio stream init + device enumeration
    ring buffer + WAV accumulation
    WAV encoding (go-audio/wav)
    chime playback (async goroutine)

Phase 3 — IPC + Daemon
    Unix socket server + client
    daemon start/stop/status lifecycle
    signal handling (SIGTERM → graceful shutdown)
    PID file management

Phase 4 — Input + Output
    evdev device scanner + hotkey listener
    hold-to-talk event loop
    Groq API transcription client
    paste-at-cursor fallback chain (xdotool → ydotool)
    clipboard save/restore
    libnotify error notifications

Phase 5 — Polish + Distribution
    first-run wizard UX
    recording timeout enforcement
    CLI config management (yap config set/get/path)
    curl install script
    GitHub releases CI
    NixOS module
```

## Static Linking Strategy

```
# Production static build
CGO_ENABLED=1 \
CC=musl-gcc \
go build \
  -tags netgo,osusergo \
  -ldflags="-linkmode external -extldflags '-static'" \
  ./cmd/yap
```

- Zig toolchain alternative: `CC="zig cc -target x86_64-linux-musl"` for cross-compilation
- Verify: `ldd ./yap` must output `not a dynamic executable`
- Nix build must set `CGO_ENABLED=1`, include `pkgs.portaudio` in `buildInputs`, `pkgs.pkg-config` in `nativeBuildInputs`

## Anti-Patterns to Avoid

| Anti-Pattern | Prevention |
|---|---|
| Global mutable config | Pass config struct via dependency injection |
| PortAudio callbacks with Go channels | Use ring buffer in callback, drain in goroutine |
| evdev exclusive grab (`EVIOCGRAB`) | Never use — locks desktop input entirely |
| Fork-based daemonization | Use `yap start &` in install script; no double-fork |
| Temp files for audio | Keep PCM in memory ring buffer; encode to WAV in-memory |
| xdotool on Wayland without fallback | Always detect `$WAYLAND_DISPLAY`, use ydotool path |

---
*Researched: 2026-03-07 | Confidence: High*
