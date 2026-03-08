# Research Summary — yap

**Project:** yap
**Domain:** Linux CLI voice-to-text daemon
**Researched:** 2026-03-07
**Confidence:** HIGH

## Executive Summary

yap is a hold-to-talk voice dictation daemon targeting Linux power users who want zero runtime dependencies and minimal idle overhead. The research confirms a clean Go implementation is viable using a small set of well-chosen libraries, with PortAudio as the sole CGo boundary. Every design decision flows from the core value proposition: a single static binary, curl-installable, ~5-10MB RAM idle, no Electron, no Python, no system packages required.

The recommended approach is a 5-phase build ordered by dependency: Foundation (config + build toolchain + Nix) before Audio Pipeline, Audio Pipeline before the IPC/daemon layer, and the full Input/Output integration (hotkeys, paste, transcription) last. This ordering is not arbitrary — CGo static linking must be solved at Phase 1 or the entire binary distribution story collapses. The paste-at-cursor fallback chain is the highest-complexity feature and must be treated as a first-class concern in Phase 4, not an afterthought.

The three highest-risk items are: (1) CGo static linking with musl-gcc (breaks the no-dependencies promise if wrong), (2) PortAudio silently succeeding but enumerating zero devices on PipeWire-only systems (silent failure, hard to debug), and (3) the Wayland paste path where xdotool returns exit code 0 while failing silently. All three have known mitigations documented in the research — they are risks only if ignored.

## Key Findings

### Recommended Stack

The stack is intentionally minimal. PortAudio (`gordonklaus/portaudio`) is the only CGo dependency and is unavoidable — no pure-Go real-time audio capture library exists. Everything else is pure Go or stdlib. The build strategy to produce a static binary from day one is: `CGO_ENABLED=1 CC=musl-gcc go build -tags netgo,osusergo -ldflags="-linkmode external -extldflags '-static'"`. Zig toolchain is an alternative for cross-compilation. This must be configured in both the Makefile and Nix flake in Phase 1.

**Core libraries:**
- `gordonklaus/portaudio` — audio capture; sole CGo boundary; no alternative exists
- `holoplot/go-evdev` — Linux hotkeys; pure Go; replaces unmaintained CGo-based `gvalkov/golang-evdev`
- `atotto/clipboard` — clipboard save/restore; pure Go; handles X11 + Wayland via subprocess
- `gen2brain/beeep` — OS notifications; pure Go via dbus; falls back to `notify-send`
- `BurntSushi/toml` — config parsing; simpler API than go-toml/v2 for yap's config surface
- `adrg/xdg` — XDG paths; required because `os.UserConfigDir()` has a known XDG_CONFIG_HOME bug (Go issue #76320)
- `go-audio/wav` — WAV encoding; pure Go; produces Whisper-compatible 16kHz 16-bit mono
- `spf13/cobra` — CLI subcommand structure (`yap start/stop/toggle/config`)
- `net/http` + `mime/multipart` — Groq API client; stdlib only; no SDK needed for single endpoint
- `//go:embed` — WAV chime assets compiled into binary; zero dependencies

**Avoid:** `gvalkov/golang-evdev` (unmaintained + CGo), `golang-design/clipboard` (CGo on Linux), `os.UserConfigDir()` (XDG bug), any Groq SDK (overkill), `xdotool` without Wayland detection.

### Expected Features

The hold-to-talk pattern is universal across all surveyed voice tools (Voxtype, Wispr Flow, Superwhisper, OpenWhispr). Users understand it immediately — any other UX model adds friction. Paste-at-cursor (not clipboard drop) is a hard requirement: forcing Ctrl+V breaks the hands-free contract.

**Must have (v0.1 table stakes):**
- Hold-to-talk via evdev hotkey (daemon mode)
- Paste at cursor with clipboard preservation (xdotool/ydotool fallback chain)
- Audio chime feedback on start/stop (embedded WAV)
- PortAudio recording → WAV → Groq Whisper API transcription
- Sub-2 second end-to-end latency (architecture-driven, not code-driven)
- First-run wizard + TOML config + env var override
- OS error notifications (libnotify)
- Recording timeout (default 60s)
- Nix flake + NixOS module

**Differentiators (competitive advantage):**
- Single static binary, curl-installable — core positioning vs Electron tools
- ~5-10MB idle RAM vs 200-600MB for Electron competitors
- CLI-composable (`yap toggle` works with sxhkd, hyprland, i3, any keybind system)
- NixOS module auto-adds user to `input` group

**Defer to v0.2+:**
- LLM post-processing of transcripts
- Press-to-toggle mode
- Silence detection auto-stop
- Transcription history log
- macOS/Windows support

**Anti-features (never build):** GUI/system tray, built-in local Whisper model, always-listening/wake-word, telemetry, streaming real-time transcription.

### Architecture Approach

The architecture is a Unix daemon with clean component boundaries and a Unix socket IPC layer. The daemon owns the event loop and goroutine lifecycle. CLI commands (`yap stop`, `yap toggle`) send newline-delimited JSON over a Unix socket at `$XDG_DATA_HOME/yap/yap.sock`. Platform-specific behavior is behind three interfaces (`HotkeyListener`, `OutputDriver`, `Notifier`) selected at compile time via build tags — this keeps the Linux implementation isolated and makes future macOS support additive.

**Major components:**
1. `cmd/yap` — Cobra CLI; dispatches to daemon or runs one-shot
2. `daemon` — Event loop; goroutine supervisor; signal handling (SIGTERM → graceful shutdown)
3. `ipc` — Unix socket server (daemon) + client (CLI); newline-delimited JSON
4. `hotkey` — evdev device scanner + key event listener; emits hold/release events
5. `audio` — PortAudio stream; ring buffer; WAV accumulation (in-memory, no temp files)
6. `transcription` — Groq API client; multipart POST; explicit HTTP status checks
7. `output` — Paste-at-cursor driver; clipboard save/restore; fallback chain
8. `config` — TOML loader; XDG paths via `adrg/xdg`; env var overrides; first-run wizard
9. `platform` — Interface definitions + Linux impls; build-tag-selected
10. `assets` — Embedded WAV chimes via `//go:embed`

**Key pattern:** Audio ring buffer in PortAudio callback, drained by goroutine — never put Go channels in the CGo callback. Keep PCM in memory; encode WAV in-memory before POST. Never call `EVIOCGRAB` on evdev device (locks entire keyboard).

### Critical Pitfalls

1. **CGo breaks static binary** — Must configure musl-gcc + `-ldflags="-linkmode external -extldflags '-static'"` in Phase 1. Verify with `ldd ./yap` → `not a dynamic executable`. Also set in Nix flake (`buildInputs = [pkgs.portaudio]`, `nativeBuildInputs = [pkgs.pkg-config]`, `CGO_ENABLED = "1"`).

2. **PortAudio + PipeWire silent failure** — `Pa_Initialize()` returns `paNoError` but enumerates 0 devices on PipeWire-only systems. After init, check device count explicitly. NixOS module must enable `services.pipewire.alsa.enable = true`.

3. **Wayland paste silent failure** — `xdotool type` exits 0 on Wayland native windows but pastes nothing. Must detect `$WAYLAND_DISPLAY` at runtime. Implement full fallback chain: `wtype → ydotool → xdotool → clipboard-only`. Verify `ydotool` socket at `/run/ydotool.sock`. Only restore clipboard after confirmed paste.

4. **evdev permission denied on first run** — `/dev/input/event*` requires group `input`. Emit actionable error with exact `usermod` command. NixOS module must add user to `input` group automatically.

5. **PortAudio stream not closed on exit** — Leaving `Pa_Terminate()` uncalled locks audio device. Use `context.WithCancel` + `signal.NotifyContext`; always deferred `stream.Close()` and `pa.Terminate()`; never call `os.Exit()` directly.

## Implications for Roadmap

### Phase 1: Foundation
**Rationale:** CGo static linking is a load-bearing constraint — if the build system is wrong, every subsequent phase produces an unusable binary. Nix packaging must also be solved here because it requires the same portaudio/pkg-config/CGO setup. Config and XDG paths are a dependency of every other component.
**Delivers:** Compilable static binary scaffold; Nix flake with portaudio; config loading; XDG paths; embedded chime assets
**Addresses:** Single binary differentiator; Nix flake requirement
**Avoids:** Pitfall #1 (CGo static linking), #10 (Nix CGo headers), #14 (PortAudio CGo pointer), #15 (chime WAV size)

### Phase 2: Audio Pipeline
**Rationale:** Audio is the core value driver and has the most hardware-dependent failure modes. Isolate and validate the PortAudio + PipeWire behavior early before building the layers that depend on it.
**Delivers:** Working audio capture; in-memory ring buffer; WAV encoding; chime playback
**Uses:** `gordonklaus/portaudio`, `go-audio/wav`, `//go:embed`
**Avoids:** Pitfall #2 (PipeWire compat), #7 (no temp files), #12 (WAV headers)

### Phase 3: IPC + Daemon
**Rationale:** The daemon lifecycle (start/stop/status) and Unix socket IPC must exist before hotkeys and output can be wired together. Signal handling and graceful shutdown belong here because audio stream cleanup depends on it.
**Delivers:** Background daemon; Unix socket IPC; `yap start/stop/status`; PID management; graceful SIGTERM shutdown
**Avoids:** Pitfall #6 (stream cleanup on exit)

### Phase 4: Input + Output
**Rationale:** This phase has the highest integration complexity — evdev device scanning, hold-to-talk event loop, Groq API, and the Wayland/X11 paste fallback chain all converge here. All Phase 1-3 infrastructure must be stable before this phase.
**Delivers:** Hold-to-talk end-to-end; evdev hotkeys; Groq transcription; paste at cursor; clipboard preservation; error notifications
**Avoids:** Pitfall #3 (evdev permissions), #4 (wrong device), #5 (clipboard race), #8 (API errors), #9 (evdev grab), #11 (NonBlock mode), #13 (xdotool timing)
**Research flag:** Paste fallback chain (wtype/ydotool/xdotool behavior differences) may benefit from phase-specific research

### Phase 5: Polish + Distribution
**Rationale:** First-run wizard, config CLI, recording timeout, and distribution (curl script, NixOS module, CI releases) are all independently deliverable polish. NixOS module must set `input` group membership and `pipewire.alsa`.
**Delivers:** First-run wizard; `yap config set/get/path`; recording timeout; curl install script; GitHub releases CI; NixOS module
**Avoids:** Pitfall #3 NixOS auto-fix (input group)

### Phase Ordering Rationale

- Foundation first because CGo/musl-gcc is a binary constraint — wrong build = no deployment
- Audio before daemon because you need to know PipeWire behavior before writing daemon restart logic around it
- Daemon before Input/Output because the hold-to-talk event loop needs the IPC layer to be stoppable
- Input/Output last because it depends on all three prior layers and carries all the Linux-specific complexity
- Polish/Distribution deferred because it cannot be finalized until the core pipeline is end-to-end

### Research Flags

Needs deeper research during planning:
- **Phase 4:** Paste fallback chain — `wtype` vs `ydotool` socket permissions vary by distro/compositor; `ydotool` requires `uinput` group; behavior on XWayland vs native Wayland windows differs
- **Phase 4:** evdev device filtering heuristics — capability bitmask approach may need testing against real hardware configurations
- **Phase 1:** Zig cross-compilation for non-musl targets — if GitHub CI runners lack musl-gcc, Zig cc may be needed

Standard patterns (skip research-phase):
- **Phase 2:** WAV encoding and PortAudio stream management — well-documented, ring buffer pattern is established
- **Phase 3:** Unix socket IPC — standard `net` package, Docker-precedent pattern, no surprises
- **Phase 5:** Cobra CLI and TOML config — trivially well-documented

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All library choices verified against alternatives; unmaintained libs identified and avoided; CGo strategy confirmed with known build flags |
| Features | MEDIUM-HIGH | Based on competitive analysis of 4+ similar tools; v0.1 scope aligns with project constraints; Wayland fallback chain complexity is known but implementation details need testing |
| Architecture | HIGH | Component boundaries are clean; IPC mechanism is standard; platform interface pattern is proven; build order is dependency-driven |
| Pitfalls | HIGH | 15 specific pitfalls identified with phase assignments; all have concrete prevention steps; PipeWire and Wayland issues are well-documented failure modes |

**Overall confidence:** HIGH

### Gaps to Address

- **ydotool socket path variability** — `/run/ydotool.sock` is the default but may differ by distro or session manager. Validate during Phase 4 implementation on at least GNOME Wayland and Hyprland.
- **wtype availability** — `wtype` is Sway-specific; not available on GNOME Wayland. Fallback chain order should be: `ydotool` (universal Wayland) → `wtype` (Sway) → `xdotool` (X11/XWayland) → clipboard-only. Confirm during Phase 4 research.
- **Groq API rate limits** — Free tier limits not fully characterized. May need backoff/retry logic. Low risk for v0.1 single-user tool.
- **musl-gcc availability on CI** — GitHub Actions Ubuntu runners may not have musl-gcc by default. May need `apt-get install musl-tools` step or Zig toolchain. Confirm in Phase 1 CI setup.

## Sources

### Primary (HIGH confidence)
- STACK.md research (2026-03-07) — library selection, CGo strategy, Nix packaging
- ARCHITECTURE.md research (2026-03-07) — component boundaries, IPC design, build order, static linking
- PITFALLS.md research (2026-03-07) — 15 pitfalls with phase assignments and prevention strategies

### Secondary (MEDIUM confidence)
- FEATURES.md research (2026-03-07) — competitive analysis of Voxtype, Wispr Flow, Superwhisper, OpenWhispr
- PROJECT.md — validated requirements and out-of-scope items

---
*Research completed: 2026-03-07*
*Ready for roadmap: yes*
