# Requirements — yap v0.1

## Milestone Scope

v0.1 delivers a working Linux hold-to-talk dictation daemon: hold a hotkey, speak, release, text appears at cursor. Single static binary, no runtime dependencies, curl-installable.

---

## Functional Requirements

### DAEMON — Background Process Lifecycle

| ID | Requirement |
|----|-------------|
| DAEMON-01 | Daemon starts in background via `yap start` and writes a PID file to `$XDG_DATA_HOME/yap/yap.pid` |
| DAEMON-02 | Daemon stops gracefully via `yap stop` (sends IPC command, waits for clean shutdown) |
| DAEMON-03 | `yap status` reports whether daemon is running and current state (idle/recording) |
| DAEMON-04 | Daemon handles SIGTERM with graceful shutdown: stops audio stream, closes PortAudio, removes PID file and socket |
| DAEMON-05 | Only one daemon instance runs at a time; second `yap start` detects live PID file and exits with error |
| DAEMON-06 | `yap toggle` sends a toggle command over IPC; starts recording if idle, stops if recording |

### IPC — Inter-Process Communication

| ID | Requirement |
|----|-------------|
| IPC-01 | Unix domain socket at `$XDG_DATA_HOME/yap/yap.sock` (mode 0600) |
| IPC-02 | Protocol is newline-delimited JSON: `{"cmd":"stop"}` → `{"ok":true,"state":"idle"}` |
| IPC-03 | CLI commands (`stop`, `status`, `toggle`) send IPC messages and exit with status code 0 on success, 1 on error |
| IPC-04 | Socket is removed on daemon shutdown; stale socket from crashed daemon is cleaned up at startup |

### AUDIO — Recording Pipeline

| ID | Requirement |
|----|-------------|
| AUDIO-01 | Audio capture via `gordonklaus/portaudio` using system default input device (configurable) |
| AUDIO-02 | After `Pa_Initialize()`, enumerate devices and fail with actionable error if count is 0 (PipeWire compat check) |
| AUDIO-03 | PCM samples held in in-memory ring buffer during capture; no temp files written to disk |
| AUDIO-04 | Ring buffer drained by goroutine; PCM data never passed via Go channel inside PortAudio callback |
| AUDIO-05 | WAV encoding to 16kHz 16-bit mono PCM via `github.com/go-audio/wav`; full RIFF/fmt/data headers |
| AUDIO-06 | WAV encoding performed in-memory (to `bytes.Buffer`); no disk I/O in the recording path |
| AUDIO-07 | PortAudio stream and `Pa_Terminate()` always called via deferred cleanup; `os.Exit()` never called directly |
| AUDIO-08 | Recording timeout enforced at configurable max duration (default: 60 seconds); recording auto-stops at limit |

### TRANSCRIPTION — Groq Whisper API

| ID | Requirement |
|----|-------------|
| TRANS-01 | Transcription via Groq Whisper API (`whisper-large-v3-turbo` model) |
| TRANS-02 | API client uses stdlib `net/http` + `mime/multipart`; no third-party SDK |
| TRANS-03 | HTTP client has explicit 30-second timeout |
| TRANS-04 | `resp.StatusCode` checked explicitly; 4xx/5xx treated as errors, not silently dropped |
| TRANS-05 | API key read from config; falls back to `GROQ_API_KEY` environment variable |
| TRANS-06 | Transcription errors surfaced via OS notification (see NOTIFY-01) |

### INPUT — Hotkey Listener

| ID | Requirement |
|----|-------------|
| INPUT-01 | Hotkey detection via `github.com/holoplot/go-evdev`; pure Go, no CGo |
| INPUT-02 | evdev device scanner filters by keyboard capability bitmask (must support `KEY_A`–`KEY_Z`); ignores non-keyboard devices |
| INPUT-03 | `EVIOCGRAB` (exclusive grab) never used; other applications continue receiving input |
| INPUT-04 | `file.Fd()` never called after `NonBlock()` on evdev file descriptor |
| INPUT-05 | Hold-to-talk loop: key press → start recording + play start chime; key release → stop recording + play stop chime → transcribe → paste |
| INPUT-06 | On `permission denied` opening `/dev/input/event*`, emit actionable error: exact `usermod -aG input $USER` command |

### OUTPUT — Paste at Cursor

| ID | Requirement |
|----|-------------|
| OUTPUT-01 | Paste method selected at runtime: detect `$WAYLAND_DISPLAY` for Wayland, `$DISPLAY` for X11 |
| OUTPUT-02 | Wayland paste fallback chain in order: `ydotool` → `wtype` → clipboard-only |
| OUTPUT-03 | X11 paste via `xdotool type --clearmodifiers` with 150ms delay after clipboard set |
| OUTPUT-04 | `ydotool` path checks socket at `/run/ydotool.sock` for accessibility before invoking |
| OUTPUT-05 | `xdotool` exit code checked; Wayland silent-success (exit 0, no paste) is treated as failure |
| OUTPUT-06 | Clipboard saved before paste via `github.com/atotto/clipboard`; restored after confirmed paste success |
| OUTPUT-07 | Clipboard restoration only occurs after paste is confirmed successful (not on failure) |

### NOTIFICATIONS — Error Feedback

| ID | Requirement |
|----|-------------|
| NOTIFY-01 | OS error notifications via `github.com/gen2brain/beeep`; falls back to `notify-send` |
| NOTIFY-02 | Notification sent on: transcription API error, device permission error, audio device not found |

### CONFIG — Configuration System

| ID | Requirement |
|----|-------------|
| CONFIG-01 | Config file at `$XDG_CONFIG_HOME/yap/config.toml`; XDG paths resolved via `github.com/adrg/xdg` (not stdlib `os.UserConfigDir()`) |
| CONFIG-02 | Config parsed via `github.com/BurntSushi/toml` |
| CONFIG-03 | Config keys: `api_key`, `hotkey`, `language`, `mic_device`, `timeout_seconds` |
| CONFIG-04 | Environment variable overrides: `GROQ_API_KEY` overrides `api_key`; `YAP_HOTKEY` overrides `hotkey` |
| CONFIG-05 | Config struct passed via dependency injection; no global mutable config |
| CONFIG-06 | `yap config set <key> <value>` updates config file |
| CONFIG-07 | `yap config get <key>` reads and prints config value |
| CONFIG-08 | `yap config path` prints resolved path to config file |

### FIRSTRUN — Setup Wizard

| ID | Requirement |
|----|-------------|
| FIRSTRUN-01 | On first run (no config file exists), interactive wizard prompts for: Groq API key, hotkey binding, language |
| FIRSTRUN-02 | Wizard writes values to config file and confirms path to user |
| FIRSTRUN-03 | Wizard skippable if `GROQ_API_KEY` env var already set |

### ASSETS — Embedded Audio Feedback

| ID | Requirement |
|----|-------------|
| ASSETS-01 | Start and stop chime WAV files embedded in binary via `//go:embed` |
| ASSETS-02 | Chime files encoded at 16kHz mono PCM; each under 100KB |
| ASSETS-03 | Chime playback is async (does not block recording start/stop path) |

### DISTRIBUTION — Install + Packaging

| ID | Requirement |
|----|-------------|
| DIST-01 | Nix flake with `packages.default` producing the static binary |
| DIST-02 | Nix build sets: `buildInputs = [pkgs.portaudio]`, `nativeBuildInputs = [pkgs.pkg-config]`, `CGO_ENABLED = "1"` |
| DIST-03 | NixOS module: enables `services.pipewire.alsa.enable = true`; adds user to `input` group via `users.users.${user}.extraGroups = ["input"]` |
| DIST-04 | GitHub Releases CI: produces static binary artifact on tag push |
| DIST-05 | `install.sh` curl script: downloads binary from GitHub Releases, installs to `~/.local/bin/yap` |

---

## Non-Functional Requirements

| ID | Requirement |
|----|-------------|
| NFR-01 | Binary is fully statically linked; `ldd ./yap` outputs `not a dynamic executable` |
| NFR-02 | Build command: `CGO_ENABLED=1 CC=musl-gcc go build -tags netgo,osusergo -ldflags="-linkmode external -extldflags '-static'" ./cmd/yap`; zig cc alternative for cross-compilation |
| NFR-03 | Idle RAM usage under 15MB (target: 5-10MB) when daemon running with no active recording |
| NFR-04 | End-to-end latency (hotkey release → text at cursor) under 2 seconds on a typical broadband connection |
| NFR-05 | Binary size small enough for curl install to be practical (target: under 20MB) |
| NFR-06 | No temp files written to disk during normal operation |
| NFR-07 | No telemetry, usage tracking, or network calls except Groq API transcription |

---

## Confirmed Stack Decisions

These replace any conflicting suggestions from the original PRD.

| Component | Library | Reason |
|-----------|---------|--------|
| Audio capture | `gordonklaus/portaudio` | Only mature Go audio capture option; sole CGo boundary |
| Linux hotkeys | `github.com/holoplot/go-evdev` | Pure Go; maintained; replaces unmaintained CGo `gvalkov/golang-evdev` |
| Clipboard | `github.com/atotto/clipboard` | Pure Go; replaces `golang-design/clipboard` (CGo on Linux) |
| Notifications | `github.com/gen2brain/beeep` | Pure Go via dbus; replaces generic "libnotify" mention |
| XDG paths | `github.com/adrg/xdg` | Required; `os.UserConfigDir()` has known XDG_CONFIG_HOME bug (Go issue #76320) |
| Config parsing | `github.com/BurntSushi/toml` | Simpler API than go-toml/v2 for yap's config surface |
| WAV encoding | `github.com/go-audio/wav` | Pure Go; Whisper-compatible 16kHz 16-bit mono |
| Asset embedding | stdlib `//go:embed` | Zero dependencies |
| CLI framework | `github.com/spf13/cobra` | Standard for Go CLI tools |
| Groq API client | stdlib `net/http` + `mime/multipart` | No SDK needed for single endpoint |
| Static linking | musl-gcc or zig cc | Verified strategy for CGo static binary |

**Explicitly avoided:**
- `gvalkov/golang-evdev` — unmaintained + CGo
- `golang-design/clipboard` — CGo on Linux
- `os.UserConfigDir()` — known XDG_CONFIG_HOME bug
- Any Groq SDK — overkill for one endpoint
- `xdotool` on Wayland without Wayland detection

---

## Out of Scope for v0.1

- macOS and Windows support (v0.3)
- LLM post-processing of transcripts (v0.2)
- Press-to-toggle mode (v0.3)
- Silence detection auto-stop (v0.3)
- Transcription history log (v0.3)
- System tray icon (v0.3)
- GUI application of any kind
- Built-in local speech model (never)
- Always-listening / wake-word (never)
- Streaming real-time transcription (never)
- Telemetry (never)

---

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| DAEMON-01 | Phase 3 | Pending |
| DAEMON-02 | Phase 3 | Pending |
| DAEMON-03 | Phase 3 | Pending |
| DAEMON-04 | Phase 3 | Pending |
| DAEMON-05 | Phase 3 | Pending |
| DAEMON-06 | Phase 3 | Pending |
| IPC-01 | Phase 3 | Pending |
| IPC-02 | Phase 3 | Pending |
| IPC-03 | Phase 3 | Pending |
| IPC-04 | Phase 3 | Pending |
| AUDIO-01 | Phase 2 | Pending |
| AUDIO-02 | Phase 2 | Pending |
| AUDIO-03 | Phase 2 | Pending |
| AUDIO-04 | Phase 2 | Pending |
| AUDIO-05 | Phase 2 | Pending |
| AUDIO-06 | Phase 2 | Pending |
| AUDIO-07 | Phase 3 | Pending |
| AUDIO-08 | Phase 5 | Pending |
| TRANS-01 | Phase 4 | Pending |
| TRANS-02 | Phase 4 | Pending |
| TRANS-03 | Phase 4 | Pending |
| TRANS-04 | Phase 4 | Pending |
| TRANS-05 | Phase 4 | Pending |
| TRANS-06 | Phase 4 | Pending |
| INPUT-01 | Phase 4 | Pending |
| INPUT-02 | Phase 4 | Pending |
| INPUT-03 | Phase 4 | Pending |
| INPUT-04 | Phase 4 | Pending |
| INPUT-05 | Phase 4 | Pending |
| INPUT-06 | Phase 4 | Pending |
| OUTPUT-01 | Phase 4 | Pending |
| OUTPUT-02 | Phase 4 | Pending |
| OUTPUT-03 | Phase 4 | Pending |
| OUTPUT-04 | Phase 4 | Pending |
| OUTPUT-05 | Phase 4 | Pending |
| OUTPUT-06 | Phase 4 | Pending |
| OUTPUT-07 | Phase 4 | Pending |
| NOTIFY-01 | Phase 4 | Pending |
| NOTIFY-02 | Phase 4 | Pending |
| CONFIG-01 | Phase 1 | Pending |
| CONFIG-02 | Phase 1 | Pending |
| CONFIG-03 | Phase 1 | Pending |
| CONFIG-04 | Phase 1 | Pending |
| CONFIG-05 | Phase 1 | Pending |
| CONFIG-06 | Phase 5 | Pending |
| CONFIG-07 | Phase 5 | Pending |
| CONFIG-08 | Phase 5 | Pending |
| FIRSTRUN-01 | Phase 5 | Pending |
| FIRSTRUN-02 | Phase 5 | Pending |
| FIRSTRUN-03 | Phase 5 | Pending |
| ASSETS-01 | Phase 1 | Pending |
| ASSETS-02 | Phase 1 | Pending |
| ASSETS-03 | Phase 2 | Pending |
| DIST-01 | Phase 1 | Pending |
| DIST-02 | Phase 1 | Pending |
| DIST-03 | Phase 5 | Pending |
| DIST-04 | Phase 5 | Pending |
| DIST-05 | Phase 5 | Pending |
| NFR-01 | Phase 1 | Pending |
| NFR-02 | Phase 1 | Pending |
| NFR-03 | Phase 2 | Pending |
| NFR-04 | Phase 4 | Pending |
| NFR-05 | Phase 1 | Pending |
| NFR-06 | Phase 2 | Pending |
| NFR-07 | Phase 1 | Pending |
