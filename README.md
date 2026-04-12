# yap — Context-Aware Voice Dictation for Your Desktop

![Build Status](https://img.shields.io/github/actions/workflow/status/Enriquefft/yap/release.yml?style=flat-square)
![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)
![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go)
![Release](https://img.shields.io/github/v/release/Enriquefft/yap?style=flat-square)

Hold a key. Speak. Text appears wherever you're typing — with your project's vocabulary intact.

yap is a lightweight voice-to-text daemon that reads your project docs and conversation history so domain terms transcribe correctly. Local whisper.cpp by default, terminal-native, near-zero idle footprint.

> **Status:** pre-1.0. Linux is the current platform. macOS and Windows are on the roadmap. Breaking changes are possible until 1.0.

## Why yap

- **Context-aware transcription** — yap reads your project docs (CLAUDE.md, README.md) and conversation history (Claude Code sessions, tmux scrollback) to bias Whisper's vocabulary. Domain terms transcribe correctly instead of being garbled.
- **Terminal-native** — OSC52, bracketed paste, tmux, SSH, Claude Code, VS Code, and browsers are first-class injection targets, not afterthoughts.
- **Local-first** — whisper.cpp runs on your machine by default. Audio never leaves your hardware. Swap to Groq for ~1s cloud latency with one line of config.
- **Zero idle footprint** — a few MB of RAM and negligible CPU when not recording.
- **Single binary** — one static executable, no runtime dependencies.
- **Composable primitives** — `Record`, `Transcribe`, `Transform`, `Inject` are independent operations exposed through a public Go library.
- **Clipboard safe** — your clipboard is preserved across paste operations that need it.

For the full architectural picture, see [`ARCHITECTURE.md`](ARCHITECTURE.md). For where the project is going, see [`ROADMAP.md`](ROADMAP.md).

## Install

### One-line install (Linux)

```bash
curl -fsSL https://yap.enriquefft.com/install | bash
```

### Manual download

Grab the latest binary from [GitHub Releases](https://github.com/Enriquefft/yap/releases) and put it on your `PATH`.

### From source

```bash
git clone https://github.com/Enriquefft/yap.git
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
yap record                       # one-shot record → transcribe → inject, no daemon
yap transcribe <file.wav>        # transcribe an audio file (--json for structured output)
yap transform "text"             # run the LLM transform on text (stdin or arg)
yap paste "text"                 # exercise the injection layer directly
yap devices                      # list audio input devices
yap hint                         # debug: show resolved target, provider, and vocabulary
```

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

[hint]
enabled = true                  # context-aware transcription (reads project docs + app state)
vocabulary_files = ["CLAUDE.md", "AGENTS.md", "README.md"]  # project docs to read for domain terms
providers = ["claudecode", "termscroll"]                       # conversation context providers, first-match wins
vocabulary_max_chars = 1000     # Whisper prompt budget (~250 tokens)
conversation_max_chars = 8000   # transform context budget
timeout_ms = 300                # max wall-time for provider fetch

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

## Context-Aware Transcription

yap reads project docs and application state to bias Whisper toward your domain vocabulary. This fixes common misrecognitions — "yap" no longer transcribes as "jump" or "chap" when you're working in the yap repo.

**Two layers, both configurable in `~/.config/yap/config.toml`:**

| Layer | Source | Feeds | Always-on? |
|---|---|---|---|
| Vocabulary | Project docs (`CLAUDE.md`, `AGENTS.md`, `README.md`) walked from cwd to git root | Whisper `prompt` parameter (lexical bias) | Yes (when `hint.enabled`) |
| Conversation | App-specific state (Claude Code session, terminal scrollback) | LLM transform context (intent grounding) | Only when a provider matches |

**Providers** supply conversation context. They run in priority order; first match wins:

| Provider | Matches | Source |
|---|---|---|
| `claudecode` | Terminal apps | `~/.claude/projects/<cwd-slug>/<latest>.jsonl` — recent user/assistant messages |
| `termscroll` | Terminal apps | Terminal scrollback via API (kitty `allow_remote_control`; wezterm, ghostty, tmux coming soon) |

**All `[hint]` options** (in `~/.config/yap/config.toml`):

```toml
[hint]
enabled = true                                              # master switch
vocabulary_files = ["CLAUDE.md", "AGENTS.md", "README.md"]  # project doc filenames, walked from cwd to git root
providers = ["claudecode", "termscroll"]                     # conversation providers, first-match wins
vocabulary_max_chars = 1000                                  # Whisper prompt budget (~250 tokens)
conversation_max_chars = 8000                                # transform context budget
timeout_ms = 300                                             # max ms for provider fetch before recording starts
```

**Per-project config:** drop a `.yap.toml` in your repo root. It overrides global hint settings for that project:

```toml
# .yap.toml (in repo root, committed or gitignored — your call)
[hint]
vocabulary_files = ["GLOSSARY.md", "CLAUDE.md", "API.md"]
vocabulary_max_chars = 2000
```

Any field not set in `.yap.toml` keeps its global default. yap walks from cwd to the nearest `.git` root to find this file.

**Disable:** `yap config set hint.enabled false` (global) or set `enabled = false` in `.yap.toml` (per-project)

**Debug:** `yap hint` prints the resolved target, winning provider, and vocabulary/conversation sizes for the currently focused window.

## Privacy

yap is local-first by default. The default transcription backend is **whisperlocal**, which runs `whisper.cpp` on your machine via a long-lived `whisper-server` subprocess. With the local backend, your audio never leaves your machine.

The model file (currently `base.en`, ~150 MB) is downloaded once into `$XDG_CACHE_HOME/yap/models/` from Hugging Face, with a SHA256 verified against the pinned manifest in `pkg/yap/transcribe/whisperlocal/models/manifest.go`. After the download, no further network calls are made for transcription.

Remote backends are available as a swap. **Groq** is the supported remote: set `transcription.backend = "groq"` and provide `YAP_API_KEY`. Any other OpenAI-compatible endpoint works via `transcription.backend = "openai"` (or `"custom"`) plus `transcription.api_url`.

yap has no accounts, no telemetry, no analytics, and no cloud sync. Ever.

## Platform Status

| Platform | Status |
|---|---|
| Linux   | Supported. |
| macOS   | Coming soon ([`ROADMAP.md`](ROADMAP.md) phase 14). |
| Windows | Planned ([`ROADMAP.md`](ROADMAP.md) phase 15). |

## Contributing

Issues and PRs are welcome. See [`ROADMAP.md`](ROADMAP.md) for the phased plan and [`ARCHITECTURE.md`](ARCHITECTURE.md) for the design contract any contribution should respect.

## License

[MIT](LICENSE).

## Links

- [Architecture](ARCHITECTURE.md)
- [Roadmap](ROADMAP.md)
- [Changelog](CHANGELOG.md)
- [GitHub Issues](https://github.com/Enriquefft/yap/issues)
