# yap — Hold-to-Talk Voice Dictation for Your Desktop

![Build Status](https://img.shields.io/github/actions/workflow/status/hybridz/yap/release.yml?style=flat-square)
![License](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)
![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8?style=flat-square&logo=go)
![Release](https://img.shields.io/github/v/release/hybridz/yap?style=flat-square)

Hold key, speak, text appears at cursor.

yap is a lightweight voice-to-text tool that runs as a daemon with near-zero idle footprint. Hold a hotkey to record, release to transcribe, and watch your text appear exactly where you're typing.

## Why yap?

- **Zero idle footprint** — ~5-10MB RAM when not recording, negligible CPU
- **Fast transcription** — ~1-2 seconds after you release the key
- **Single static binary** — No runtime dependencies, just one executable
- **Privacy-focused** — Audio never leaves your machine without consent
- **Audio feedback** — Subtle chimes confirm recording state
- **Clipboard safe** — Your clipboard is preserved after pasting

## Quick Install

### One-line install (Linux)

```bash
curl -fsSL https://yap.sh/install | bash
```

### Nix

```bash
nix profile install github:hybridz/yap
```

### Manual download

Download the latest binary from [GitHub Releases](https://github.com/hybridz/yap/releases) and add it to your PATH.

## Getting Started

1. **Start yap** — The daemon runs an interactive first-run wizard on first launch:

   ```bash
   yap start
   ```

   The wizard will prompt you for:
   - Your Groq API key (get it free at [console.groq.com](https://console.groq.com))
   - Your preferred hotkey (default: Right Ctrl)
   - Your language preference (default: auto-detect)

2. **Hold your hotkey** — A chime confirms recording has started

3. **Speak** — Talk naturally, no need to rush

4. **Release the hotkey** — Recording stops, a chime confirms completion, and your transcribed text appears at your cursor

## Usage

```bash
# Start the daemon (runs wizard on first run)
yap start

# Stop the daemon (idempotent, safe for scripts)
yap stop

# Check daemon status
yap status

# Toggle recording (start if idle, stop if recording)
yap toggle

# Configuration management
yap config get hotkey
yap config set hotkey KEY_RIGHTCTRL
yap config set timeout_seconds 60
yap config path
```

## Configuration

Config file location: `~/.config/yap/config.toml`

Example config:

```toml
# ~/.config/yap/config.toml

api_key = "your-groq-api-key-here"
hotkey = "KEY_RIGHTCTRL"
language = "en"
mic_device = ""
timeout_seconds = 60
```

### Environment Variables

- `GROQ_API_KEY` — Groq API key for transcription (overrides config file)
- `YAP_HOTKEY` — Keyboard key for hold-to-talk (overrides config file)

### First-Run Wizard

On first run without a config, yap launches an interactive setup wizard that:

1. Prompts for your Groq API key (required)
2. Asks for your preferred hotkey (default: Right Ctrl)
3. Asks for language preference (default: auto-detect)
4. Creates `~/.config/yap/config.toml`
5. Prints "You're ready! Hold [hotkey] to speak."

## Features

- **Daemon mode** — Background process with global hotkey listening
- **CLI mode** — `yap start`/`yap stop` for external keybind integration
- **Hold-to-talk** — Record while hotkey is held, stop on release
- **Audio recording** — Via PortAudio (cross-platform)
- **Transcription** — Via Groq Whisper API (free tier available)
- **Paste at cursor** — Platform-native input simulation
- **Clipboard preservation** — Save and restore clipboard after paste
- **Audio feedback** — Subtle chime on recording start/stop
- **Configurable timeout** — Max duration (default: 60s)
- **Multiple languages** — Support for all Whisper languages

## Platform Status

| Platform | Status | Notes |
|----------|--------|-------|
| Linux | Full support | Current stable release |
| macOS | Planned | Coming soon |
| Windows | Planned | Coming soon |

## Development

### Build from source

```bash
# Clone the repository
git clone https://github.com/hybridz/yap.git
cd yap

# Build
make build

# Build static binary
make build-static

# Run tests
make test
```

### Nix development shell

```bash
nix develop
```

### Contributing

Contributions are welcome! Please read [LICENSE](LICENSE) (AGPL-3.0) and check [Issues](https://github.com/hybridz/yap/issues) for open work.

## Links

- [GitHub Issues](https://github.com/hybridz/yap/issues)
- [PRD](PRD.md) — Product Requirements Document
- [Roadmap](.planning/ROADMAP.md)

## License

AGPL-3.0 — See [LICENSE](LICENSE) for details.
