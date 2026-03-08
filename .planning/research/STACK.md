# Stack Research — yap

## Audio Capture

**Choice:** `github.com/gordonklaus/portaudio`

- Only mature Go audio capture option with active maintenance
- Requires CGo and `portaudio19-dev` at build time
- No viable pure-Go alternative exists for real-time audio capture
- **CGo boundary:** PortAudio is the sole CGo boundary in the entire stack

**CGo / Static Linking (Critical)**

Static builds require `musl-gcc` or `zig cc -target x86_64-linux-musl`. Cross-compilation requires architecture-specific PortAudio headers. The Nix package must include `pkgs.portaudio` in `buildInputs`. To produce fully static binary: `CGO_ENABLED=1 CC=musl-gcc go build -ldflags="-linkmode external -extldflags '-static'"`.

## Linux Hotkeys (evdev)

**Choice:** `github.com/holoplot/go-evdev`

- Pure Go, no CGo
- Supports `EVIOCGRAB` for exclusive device access
- The original `gvalkov/golang-evdev` is unmaintained and CGo-based — avoid
- Requires `/dev/input` access (group `input` or `uinput`)

## Input Simulation

**Choice:** Invoke `xdotool type` (X11) or `ydotool type` (Wayland) via `os/exec`

- No Go library exists for this
- Must detect display server at runtime (`$WAYLAND_DISPLAY` vs `$DISPLAY`)
- **Gotcha:** `xdotool` returns exit code 0 even when it silently fails on Wayland native windows — must verify paste succeeded

## Clipboard

**Choice:** `github.com/atotto/clipboard`

- Pure Go, handles X11 and Wayland via subprocess
- Avoid `golang-design/clipboard` — CGo on Linux, adds unnecessary complexity
- Handles clipboard save/restore for preservation

## Notifications

**Choice:** `github.com/gen2brain/beeep`

- Pure Go via `godbus/dbus`, falls back to `notify-send`
- Updated December 2025
- No CGo

## Config / TOML

**Choice:** `github.com/BurntSushi/toml`

- Simpler API than go-toml/v2, adequate for yap's small config surface
- Battle-tested, widely used

## XDG Paths

**Choice:** `github.com/adrg/xdg`

- Full XDG spec implementation
- `os.UserConfigDir()` has a known bug (Go issue #76320) where it does not correctly respect `XDG_CONFIG_HOME` — do not use stdlib for this

## Transcription (Groq Whisper API)

**Choice:** stdlib `net/http` + `mime/multipart`

- Groq API is a single multipart POST — no client library needed
- Zero dependencies for this component

## WAV Encoding

**Choice:** `github.com/go-audio/wav`

- Battle-tested, pure Go
- Encodes 16kHz 16-bit mono PCM directly (Whisper-compatible)

## Audio Asset Embedding

**Choice:** stdlib `//go:embed`

- Zero dependencies
- WAV chime files compiled into binary at build time

## CLI Framework

**Choice:** `github.com/spf13/cobra`

- Handles `yap start/stop/toggle/config` subcommand structure
- Standard choice for Go CLI tools

## What NOT to Use

| Library | Reason to Avoid |
|---------|-----------------|
| `gvalkov/golang-evdev` | Unmaintained, CGo-based |
| `golang-design/clipboard` | CGo on Linux |
| `os.UserConfigDir()` | Known XDG_CONFIG_HOME bug |
| `xdotool` on Wayland | Silent failures, wrong exit codes |
| Any Groq SDK | Overkill for single API endpoint |

## Nix Packaging Notes

- `buildInputs`: `pkgs.portaudio`
- `nativeBuildInputs`: `pkgs.pkg-config`
- Must set `CGO_ENABLED=1` in Nix build
- `PKG_CONFIG_PATH` must point to portaudio.pc

---
*Researched: 2026-03-07 | Confidence: High*
