# yap

## What This Is

yap is a lightweight, cross-platform CLI tool for voice-to-text input. Hold a hotkey to record, release to transcribe and paste — text appears at the cursor. It runs as a single static Go binary with near-zero idle footprint (~5-10MB RAM), targeting Linux desktop users and developers who want quick dictation without a heavyweight app.

## Core Value

Hold a key, speak, get text at cursor — with near-zero idle resource usage and no runtime dependencies.

## Requirements

### Validated

- ✓ Configuration via TOML config file + environment variables — Phase 1
- ✓ Audio feedback: embedded WAV chime on recording start/stop — Phase 1 (assets) + Phase 2 (playback)
- ✓ Audio recording via PortAudio (cross-platform bindings) — Phase 2
- ✓ Mic selection: system default, configurable via config — Phase 2
- ✓ Nix package: installable via Nix flake — Phase 1 (static binary gate passed, 2.64MB)

### Active

- [ ] Daemon mode: background process with global hotkey listening (Linux via evdev)
- [ ] CLI mode: `yap start`/`yap stop`/`yap toggle` for external keybind integration
- [ ] Hold-to-talk: record while hotkey held, stop on release, transcribe and paste
- [ ] Transcription via Groq Whisper API (whisper-large-v3-turbo)
- [ ] Paste at cursor via platform-native input simulation (xdotool/ydotool on Linux)
- [ ] Clipboard preservation: save and restore clipboard content after paste
- [ ] First-run setup wizard (prompts for API key, hotkey, language)
- [ ] CLI config management (`yap config set/get/path`)
- [ ] Error notifications via OS-native notification system (libnotify)
- [ ] Recording timeout: configurable max duration (default 60s)
- [ ] NixOS module with auto-input-group

### Out of Scope

- Full GUI application — this is a CLI tool, not a desktop app
- Built-in speech model — API-based transcription only
- Real-time/streaming transcription — batch after release
- Always-listening / wake-word activation — intentional for resource reasons
- macOS/Windows support (v0.1) — Linux first, other platforms in v0.3
- Post-transcription LLM transformation (v0.1) — deferred to v0.2
- Press-to-toggle mode (v0.1) — deferred to v0.3
- Silence detection auto-stop (v0.1) — deferred to v0.3
- Transcription history log (v0.1) — deferred to v0.3
- System tray icon (v0.1) — deferred to v0.3

## Context

- **Language**: Go — chosen for single static binary, trivial cross-compilation, strong ecosystem for audio/HTTP/hotkeys
- **Audio**: PortAudio via Go bindings — cross-platform, mature, supports all major audio backends
- **Transcription**: Groq Whisper API — free tier, fast inference, accurate
- **Input simulation**: Platform-native — xdotool/ydotool (Linux), AppleScript (macOS), SendInput (Windows)
- **Config format**: TOML at `~/.config/yap/config.toml` (XDG on Linux)
- **Audio format**: WAV 16-bit PCM, 16kHz mono — Whisper-compatible, ~1.9MB per 60s
- **Audio feedback**: Embedded WAV assets compiled into binary
- **Target users**: Linux desktop power users, developers, anyone frustrated by Electron/Tauri voice apps
- **Competing tools**: Whispering, Superwhisper, Wispr Flow — all heavy always-on desktop apps
- **Distribution**: curl install script (primary), Nix flake, GitHub Releases, Homebrew, AUR, `go install`

## Constraints

- **Tech stack**: Go only — no scripting languages, no runtime dependencies, single static binary
- **Linux-first**: evdev for hotkeys, xdotool/ydotool for input simulation, libnotify for notifications
- **API dependency**: Transcription requires Groq API key (no offline mode in v0.1)
- **Audio format**: Must use WAV 16-bit 16kHz mono for Whisper API compatibility
- **Binary size**: Must remain small enough for curl install to be practical

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Go for implementation | Single binary, cross-compilation, strong CLI ecosystem | ✓ Phase 1 — 2.64MB static binary |
| PortAudio for audio capture | Cross-platform, mature, supports all backends | ✓ Phase 2 — blocking stream pattern |
| Groq Whisper for transcription | Free tier, fast, accurate | — Pending (Phase 4) |
| evdev for Linux hotkeys | Direct kernel input, no X11/Wayland dependency | — Pending (Phase 4) |
| Embedded WAV assets | No external sound files, compiled into binary | ✓ Phase 1+2 — 9.5KB each, async playback |
| TOML config format | Readable, typed, standard for CLI tools | ✓ Phase 1 — BurntSushi/toml v1.6.0 |
| XDG config location | Follows platform conventions | ✓ Phase 1 — adrg/xdg v0.5.3 |
| Closure injection for config | No global mutable config; testable | ✓ Phase 1 — rootCfg + PersistentPreRunE |
| In-memory WAV encoding | No temp files; ReadWriteSeeker for wav.Encoder | ✓ Phase 2 — ~12.8MB peak for 60s |
| Async chime playback | Non-blocking; own PortAudio lifecycle per goroutine | ✓ Phase 2 — <5ms return time |

---
*Last updated: 2026-03-08 after Phase 2*
