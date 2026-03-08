# Features Research — yap

## Table Stakes (Must-Have)

| Feature | Rationale | Complexity |
|---------|-----------|------------|
| Hold-to-talk UX | Dominant pattern across all surveyed tools (Voxtype, VOXD, OpenWhispr, Superwhisper, Wispr Flow). Users understand it immediately — anything else adds cognitive load. | Low |
| Accurate transcription | If error rate forces more editing than typing, the tool is worthless. Groq Whisper whisper-large-v3-turbo is 164x real-time with ~8-10% WER. | External (API) |
| Paste at cursor, not clipboard | Manual Ctrl+V breaks the hands-free contract. Every Linux tool surveyed implements direct paste. | Medium |
| Audio chime feedback (start/stop) | Users cannot tell if recording started/stopped without it. Causes repeated hotkey presses and confusion. | Low |
| Sub-2 second end-to-end latency | Under 300ms feels instant; 800ms is acceptable; beyond 1500ms users perceive the tool as broken. | Architecture concern |
| Clipboard preservation | Silent data loss if clipboard is not saved/restored around paste operation. Every tool that pastes at cursor implements this. | Low |
| API key config (first-run wizard) | Any cloud-backed tool requires frictionless key setup. Three-layer: first-run wizard + env var + config file. | Medium |

## Differentiators (Competitive Advantage)

| Feature | Rationale | Complexity |
|---------|-----------|------------|
| Single static binary | Core value proposition for target audience (Linux power users, devs frustrated by Electron tools). No install friction, curl-installable. | Architecture (Phase 1) |
| ~5-10MB idle RAM | Electron-based tools (OpenWhispr) sit at 200-600MB idle. Go daemon with evdev listener is fundamentally different category. | Architecture |
| No runtime dependencies | Zero `apt install`, no Python, no Node. Binary runs on any Linux x86_64. | Architecture |
| Nix flake + NixOS module | Target audience heavily overlaps with NixOS users. Auto-adds user to `input` group via module. | Medium |
| CLI-composable | `yap start`/`yap stop`/`yap toggle` enables integration with any keybind system (sxhkd, hyprland, i3). | Low |

## Anti-Features (Deliberately Avoid)

| Feature | Why to Avoid |
|---------|--------------|
| GUI / system tray | Kills CLI positioning, adds Electron/GTK dependency, destroys idle RAM story |
| Built-in local Whisper model | Destroys binary size (~GB for whisper-large), incompatible with static binary goal |
| Always-listening / wake-word | CPU drain, privacy concerns, defeats resource-efficiency positioning |
| Voice correction commands ("scratch that") | Wispr Flow took years to implement correctly — not for v0.1 |
| LLM post-processing | Deferred to v0.2; adds API complexity and latency |
| Telemetry / usage tracking | Destroys open-source trust with target audience |
| Streaming/real-time transcription | Batch-after-release is simpler, sufficient, and what Groq API supports well |

## Feature Dependencies

```
Config loading
    └── First-run wizard
         └── API key storage
              └── Transcription
                   └── Paste at cursor
                        └── Clipboard preservation

evdev hotkey listener
    └── Hold-to-talk UX
         └── Audio recording
              └── WAV encoding
                   └── Transcription
                        └── Audio feedback (chime on stop)

Audio feedback (chime on start) ─── Audio recording
```

## Highest-Risk Feature

**Paste-at-cursor fallback chain** (wtype → ydotool → xdotool → clipboard-only) is the most operationally complex feature. Wayland's security model means no single method works everywhere. Every Linux voice tool surveyed implements a multi-method fallback. Must be a first-class implementation concern, not an afterthought.

## v0.1 MVP Scope

- Hold-to-talk via evdev hotkey (daemon mode)
- PortAudio recording → WAV → Groq Whisper API
- Paste at cursor (xdotool/ydotool with fallback chain)
- Clipboard preservation
- Audio chime feedback
- First-run setup wizard
- TOML config + env var override
- `yap config get/set/path` subcommands
- OS notification on error (libnotify)
- Recording timeout (default 60s)
- Nix flake + NixOS module

---
*Researched: 2026-03-07 | Confidence: Medium-High*
