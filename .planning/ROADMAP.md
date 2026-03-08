# Roadmap — yap v0.1

## Phases

- [x] **Phase 1: Foundation** — Static binary scaffold, Nix flake, config/XDG, embedded assets (completed 2026-03-08)
- [x] **Phase 2: Audio Pipeline** — PortAudio capture, ring buffer, WAV encoding, chime playback (completed 2026-03-08)
- [x] **Phase 3: IPC + Daemon** — Unix socket IPC, daemon lifecycle, signal handling, PID management (completed 2026-03-08)
- [x] **Phase 4: Input + Output** — evdev hotkeys, hold-to-talk loop, Groq transcription, paste fallback chain (completed 2026-03-08)
- [ ] **Phase 5: Polish + Distribution** — First-run wizard, config CLI, recording timeout, curl install, NixOS module

---

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation | 3/3 | Complete | 2026-03-08 |
| 2. Audio Pipeline | 3/3 | Complete | 2026-03-08 |
| 3. IPC + Daemon | 2/2 | Complete | 2026-03-08 |
| 4. Input + Output | 3/3 | Complete | 2026-03-08 |
| 5. Polish + Distribution | 0/3 | Not Started | - |

---

## Phase Details

### Phase 1: Foundation

**Goal:** Produce a verified static binary from day one with config loading, XDG paths, embedded assets, and a Nix flake — so every subsequent phase builds on a deployable scaffold.

**Depends on:** Nothing (first phase)

**Requirements:** CONFIG-01, CONFIG-02, CONFIG-03, CONFIG-04, CONFIG-05, ASSETS-01, ASSETS-02, DIST-01, DIST-02, NFR-01, NFR-02, NFR-05, NFR-07

**Success Criteria** (what must be TRUE when this phase completes):
1. `go build ./cmd/yap` produces a binary; `ldd ./yap` outputs `not a dynamic executable`
2. `nix build` completes without error and produces a runnable binary
3. `yap --help` prints the Cobra subcommand tree (`start`, `stop`, `status`, `toggle`, `config`)
4. Config file is read from `$XDG_CONFIG_HOME/yap/config.toml`; missing file does not crash, it triggers defaults
5. Embedded chime WAV assets are present in the binary (verifiable via a `--list-assets` debug flag or unit test)

**Plans:** 3/3 plans complete

Plans:
- [x] 01-01-PLAN.md — Go module scaffold, Cobra CLI subcommand stubs, Wave 0 test stubs, Makefile
- [x] 01-02-PLAN.md — Config package (XDG + TOML + env overrides) and assets package (embedded WAV chimes)
- [x] 01-03-PLAN.md — Nix flake (static + dynamic packages) and static binary verification gate

**Pitfalls addressed:** #1 CGo static linking, #10 Nix CGo headers, #14 PortAudio CGo pointer, #15 Chime size

---

### Phase 2: Audio Pipeline

**Goal:** Capture audio from the microphone into an in-memory ring buffer, encode to a valid WAV file, and play chime feedback — fully functional and tested in isolation before the daemon exists.

**Depends on:** Phase 1

**Requirements:** AUDIO-01, AUDIO-02, AUDIO-03, AUDIO-04, AUDIO-05, AUDIO-06, ASSETS-03, NFR-03, NFR-04, NFR-07

**Success Criteria** (what must be TRUE when this phase completes):
1. `go test -v ./internal/audio` passes with CGO_ENABLED=0 (no runtime deps)
2. Audio ring buffer accepts 16kHz 16-bit PCM samples without data loss
3. WAV encoder produces valid RIFF headers matching 16kHz mono PCM spec
4. PlayChime() plays 440Hz chime at start and 770Hz warning at 50s
5. `go test` demonstrates timeout cancellation via context cancellation

**Plans:** 3/3 plans complete

Plans:
- [x] 02-01-PLAN.md — Wave 0 test stubs for all audio requirements + add go-audio/wav to go.mod
- [x] 02-02-PLAN.md — ReadWriteSeeker + WAV encoder + Recorder struct with blocking stream and PipeWire guard
- [x] 02-03-PLAN.md — Async chime playback (PlayChime goroutine) + NFR-03 benchmark

**Pitfalls addressed:** #2 PipeWire compat, #7 Temp files, #12 WAV headers

---

### Phase 3: IPC + Daemon

**Goal:** A daemon that starts in the background, responds to IPC commands over a Unix socket, and shuts down cleanly — with PortAudio stream properly closed on exit.

**Depends on:** Phase 2

**Requirements:** DAEMON-01, DAEMON-02, DAEMON-03, DAEMON-04, DAEMON-05, DAEMON-06, IPC-01, IPC-02, IPC-03, IPC-04, AUDIO-07

**Success Criteria** (what must be TRUE when this phase completes):
1. `yap start` backgrounds the daemon; `ps aux | grep yap` shows it running; PID file exists at `$XDG_DATA_HOME/yap/yap.pid`
2. `yap stop` terminates the daemon; PID file and socket are removed; no zombie process remains
3. `yap status` returns `{"ok":true,"state":"idle"}` when daemon is running and `{"ok":false,"error":"not running"}` when it is not
4. Sending SIGTERM to the daemon process causes clean shutdown (PortAudio terminated, socket removed) within 2 seconds
5. Running `yap start` twice prints an error and exits non-zero without starting a second daemon

**Plans:** 2/2 plans complete

Plans:
- [x] 03-01-PLAN.md — Daemon lifecycle (daemonization, PID file management, SIGTERM handling, PortAudio cleanup)
- [x] 03-02-PLAN.md — IPC server (Unix socket, NDJSON protocol, client library) and CLI command integration

**Pitfalls addressed:** #6 Stream cleanup on exit

---

### Phase 4: Input + Output

**Goal:** End-to-end hold-to-talk works: hold a hotkey → audio records with chime → release → transcript appears at cursor via the correct paste method for the active display server.

**Depends on:** Phase 3

**Requirements:** INPUT-01, INPUT-02, INPUT-03, INPUT-04, INPUT-05, INPUT-06, OUTPUT-01, OUTPUT-02, OUTPUT-03, OUTPUT-04, OUTPUT-05, OUTPUT-06, OUTPUT-07, TRANS-01, TRANS-02, TRANS-03, TRANS-04, TRANS-05, TRANS-06, NOTIFY-01, NOTIFY-02, NFR-04

**Success Criteria** (what must be TRUE when this phase completes):
1. Holding the configured hotkey starts recording (start chime plays); releasing stops recording (stop chime plays); transcribed text appears at cursor within 2 seconds of release
2. On X11: text appears via `xdotool`; original clipboard content is preserved after paste
3. On Wayland (GNOME or Hyprland): text appears via `ydotool` or `wtype`; clipboard is preserved
4. If Groq API returns 4xx/5xx, an OS desktop notification appears with the error message; no silent failure
5. If `/dev/input/event*` cannot be opened, yap prints the exact `usermod -aG input $USER` command and exits non-zero

**Plans:** 3/3 plans complete

Plans:
- [x] 04-01-PLAN.md — Evdev hotkey listener with device scanning, alpha key detection, permission error handling
- [x] 04-02-PLAN.md — Groq Whisper API transcription client with retry logic and desktop notification package
- [x] 04-03-PLAN.md — Display-server-aware paste package with Wayland fallback chain and complete hold-to-talk pipeline

**Pitfalls addressed:** #3 evdev permissions, #4 Wrong device, #5 Clipboard race, #8 API errors, #9 evdev grab, #11 NonBlock, #13 xdotool timing

---

### Phase 5: Polish + Distribution

**Goal:** The tool is shippable: first-run wizard guides new users to a working config, `yap config` subcommands manage settings, recording timeout prevents runaway capture, and the binary is installable via curl or Nix.

**Depends on:** Phase 4

**Requirements:** AUDIO-08, CONFIG-06, CONFIG-07, CONFIG-08, FIRSTRUN-01, FIRSTRUN-02, FIRSTRUN-03, DIST-03, DIST-04, DIST-05

**Success Criteria** (what must be TRUE when this phase completes):
1. On a system with no `~/.config/yap/config.toml`, running `yap start` launches an interactive wizard that prompts for API key and hotkey and writes a valid config file before starting the daemon
2. `yap config set api_key sk-xxx` updates the config file; `yap config get api_key` returns `sk-xxx`; `yap config path` prints the resolved path
3. A recording that exceeds 60 seconds (default) auto-stops, plays the stop chime, and submits the audio for transcription
4. `curl -fsSL https://raw.githubusercontent.com/.../install.sh | bash` installs `yap` to `~/.local/bin/yap` on a fresh Debian/Ubuntu system with no prior yap installation
5. `nix build .#nixosModules.default` succeeds; the generated NixOS module adds the user to `input` group and enables `services.pipewire.alsa`

**Plans:** 3 plans

Plans:
- [ ] 05-01-PLAN.md — First-run wizard with interactive prompts for API key, hotkey, and language
- [ ] 05-02-PLAN.md — Config CLI subcommands (set/get/path) and configurable recording timeout
- [ ] 05-03-PLAN.md — NixOS module with auto-input-group, GitHub Releases CI, and curl install script

**Pitfalls addressed:** #3 NixOS module auto-adds input group

---
