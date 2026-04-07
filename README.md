# yap — Hold-to-Talk Voice Dictation for Your Desktop

![Build Status](https://img.shields.io/github/actions/workflow/status/hybridz/yap/release.yml?style=flat-square)
![License](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)
![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go)
![Release](https://img.shields.io/github/v/release/hybridz/yap?style=flat-square)

Hold a key. Speak. Text appears wherever you're typing.

yap is a lightweight voice-to-text daemon with near-zero idle footprint. Its sole job is to let you drive any app — terminals and Claude Code first — with your voice, reliably and fast.

> **Status:** pre-1.0. Linux is the current platform. macOS and Windows are on the roadmap. Breaking changes are possible until 1.0.

## Why yap

- **Zero idle footprint** — a few MB of RAM and negligible CPU when not recording.
- **Fast transcription** — short clips return in roughly a second.
- **Single binary** — one static executable, no runtime dependencies.
- **App-aware text injection** — terminals, Electron editors, browsers, tmux, and SSH sessions are first-class targets, not afterthoughts.
- **Composable primitives** — `Record`, `Transcribe`, `Transform`, `Inject` are independent operations exposed through a public Go library.
- **Clipboard safe** — your clipboard is preserved across paste operations that need it.

For the full architectural picture, see [`ARCHITECTURE.md`](ARCHITECTURE.md). For where the project is going, see [`ROADMAP.md`](ROADMAP.md).

## Install

### One-line install (Linux)

```bash
curl -fsSL https://yap.sh/install | bash
```

### Manual download

Grab the latest binary from [GitHub Releases](https://github.com/hybridz/yap/releases) and put it on your `PATH`.

### From source

```bash
git clone https://github.com/hybridz/yap.git
cd yap
make build           # dynamic build
make build-static    # static binary (musl)
make test
```

## Getting Started

The first time you launch the daemon, yap runs an interactive wizard to set up your hotkey, transcription backend, and language preference.

```bash
yap listen
```

> The current binary still ships `yap start` as a synonym for `yap listen` while the CLI rework lands. Both work today; new docs and scripts should use `yap listen`.

The wizard will ask for:

1. Your transcription backend. The default is **whisperlocal** (local whisper.cpp via the `whisper-server` subprocess); **groq** is available as a remote fallback.
2. The API key for the chosen backend, if it's a remote one.
3. Your preferred hotkey (default: Right Ctrl).
4. Your language preference (default: auto-detect).

If you pick the local backend on first run, download the model with:

```bash
yap models download base.en
```

The model file is ~150 MB. The daemon refuses to start with the local backend until the file is in the cache (or `transcription.model_path` points at one).

Once it's running:

1. **Hold the hotkey** — a chime confirms recording started.
2. **Speak** — talk naturally.
3. **Release the hotkey** — recording stops, a chime confirms, and the transcribed text is injected at your cursor.

## CLI

```bash
yap listen                       # start the daemon (background, hotkey-driven)
yap listen --foreground          # foreground mode (systemd, launchd, containers)
yap stop                         # stop the daemon (idempotent)
yap status                       # daemon state, mode, version, backend (JSON)
yap toggle                       # toggle recording from a script or external keybind
yap config get <key>             # dot-notation: yap config get transcription.backend
yap config set <key> <value>     # dot-notation
yap config path                  # print resolved config file path
yap models list                  # list pinned whisper.cpp models and install state
yap models download <name>       # download a model into the cache (SHA256-verified)
yap models path [name]           # print the cache directory or a specific model path
```

Additional commands (`yap record`, `yap transcribe`, `yap transform`, `yap paste`, `yap devices`, `yap history`) ship as the corresponding roadmap phases land. See [`ROADMAP.md`](ROADMAP.md).

## Configuration

Config file location: `$XDG_CONFIG_HOME/yap/config.toml` (typically `~/.config/yap/config.toml`).

The schema is nested. Each section has its own well-defined surface:

```toml
[general]
hotkey = "KEY_RIGHTCTRL"        # single key today; combos land in a later phase
mode = "hold"                   # "hold" | "toggle"
max_duration = 60               # max recording seconds
audio_feedback = true           # chime on start/stop
audio_device = ""               # empty = system default
silence_detection = false
silence_threshold = 0.02
silence_duration = 2.0
history = false
stream_partials = true

[transcription]
backend = "whisperlocal"        # default. Set to "groq" or "openai" for a remote backend.
model = "base.en"               # whisperlocal: base.en (only pinned). remote: backend-specific.
model_path = ""                 # explicit local model path; empty auto-resolves from the cache
whisper_server_path = ""        # explicit whisper-server binary; empty resolves via PATH
language = ""                   # empty = auto-detect
prompt = ""

# Used by remote backends:
api_url = ""                    # empty = backend default
api_key = ""                    # or env: YAP_API_KEY / GROQ_API_KEY

[transform]
enabled = false
backend = "passthrough"         # "passthrough" | "local" | "openai"
model = ""
system_prompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."
api_url = ""
api_key = ""                    # or env: YAP_TRANSFORM_API_KEY

[injection]
prefer_osc52 = true             # OSC52 for terminals when supported
bracketed_paste = true          # wrap multi-line text for shells
electron_strategy = "clipboard" # "clipboard" | "keystroke"
# app_overrides = [
#   { match = "firefox", strategy = "clipboard" },
#   { match = "kitty",   strategy = "osc52" },
# ]

[tray]
enabled = false
```

The TOML schema, the NixOS module, the wizard prompts, and the validation logic all generate from the same Go types in `pkg/yap/config/`. There is no hand-maintained drift across surfaces.

### Environment Variables

| Variable | Effect |
|---|---|
| `YAP_API_KEY` | Sets `transcription.api_key`. Primary, used by remote backends. |
| `GROQ_API_KEY` | Compat alias for `YAP_API_KEY`. |
| `YAP_TRANSFORM_API_KEY` | Sets `transform.api_key`. |
| `YAP_HOTKEY` | Compat alias for `general.hotkey`. |
| `YAP_CONFIG` | Override config file path. |

## Privacy

yap is local-first by default. The default transcription backend is **whisperlocal**, which runs `whisper.cpp` on your machine via a long-lived `whisper-server` subprocess. With the local backend, your audio never leaves your machine.

The model file (currently `base.en`, ~150 MB) is downloaded once into `$XDG_CACHE_HOME/yap/models/` from Hugging Face, with a SHA256 verified against the pinned manifest in `pkg/yap/transcribe/whisperlocal/models/manifest.go`. After the download, no further network calls are made for transcription.

Remote backends are available as a swap. **Groq** is the supported remote: set `transcription.backend = "groq"` and provide `YAP_API_KEY`. Any other OpenAI-compatible endpoint works via `transcription.backend = "openai"` (or `"custom"`) plus `transcription.api_url`.

yap has no accounts, no telemetry, no analytics, and no cloud sync. Ever.

## Platform Status

| Platform | Status |
|---|---|
| Linux   | Supported. |
| macOS   | Planned ([`ROADMAP.md`](ROADMAP.md) phase 13). |
| Windows | Planned ([`ROADMAP.md`](ROADMAP.md) phase 14). |

## Contributing

Issues and PRs are welcome. See [`ROADMAP.md`](ROADMAP.md) for the phased plan and [`ARCHITECTURE.md`](ARCHITECTURE.md) for the design contract any contribution should respect.

## License

[AGPL-3.0](LICENSE).

## Links

- [Architecture](ARCHITECTURE.md)
- [Roadmap](ROADMAP.md)
- [Changelog](CHANGELOG.md)
- [GitHub Issues](https://github.com/hybridz/yap/issues)
