# Pitfalls Research — yap

## Critical Pitfalls

### 1. CGo Destroys Static Binary
**Problem:** `gordonklaus/portaudio` forces dynamic libc linking by default. Standard `go build` produces a dynamically linked binary requiring `libc.so.6`, `libportaudio.so.2`, etc. on the target system.

**Warning signs:** `ldd ./yap` shows dynamic deps; installs fail on systems with different libc versions.

**Prevention:**
```bash
CGO_ENABLED=1 CC=musl-gcc go build \
  -tags netgo,osusergo \
  -ldflags="-linkmode external -extldflags '-static'" \
  ./cmd/yap
```
Verify with `ldd ./yap` → must output `not a dynamic executable`.

**Phase:** Phase 1 (Foundation) — set up in Nix flake and Makefile from day one.

---

### 2. PortAudio Fails Silently on PipeWire-Only Systems
**Problem:** PortAudio has no native PipeWire backend. On modern systems with PipeWire as sole audio server (no PulseAudio compat layer), `Pa_Initialize()` returns `paNoError` but no devices are enumerated.

**Warning signs:** Device count is 0 after `Initialize()` succeeds; recording produces silence.

**Prevention:** After `Pa_Initialize()`, enumerate devices and fail explicitly if count is 0. Document that `pipewire-alsa` or `pipewire-pulse` must be enabled. Add to NixOS module: `services.pipewire.alsa.enable = true`.

**Phase:** Phase 2 (Audio Pipeline)

---

### 3. evdev `permission denied` on First Run
**Problem:** `/dev/input/event*` devices are owned by group `input`. A new user install fails immediately with `open /dev/input/event0: permission denied`.

**Warning signs:** Any evdev open fails on fresh system.

**Prevention:** Emit actionable error: `"Add user to 'input' group: sudo usermod -aG input $USER, then log out/in"`. NixOS module auto-adds: `users.users.${user}.extraGroups = ["input"]`.

**Phase:** Phase 4 (Input/Output) + Phase 5 (NixOS module)

---

### 4. evdev Picks Wrong Input Device
**Problem:** Power buttons, media keys, and USB hubs all appear as `/dev/input/event*`. Without filtering, yap may bind to a device that never emits the target key.

**Warning signs:** Hotkey never fires; no events in debug mode.

**Prevention:** Filter by capability bitmask — device must have `KEY_A`–`KEY_Z` capability. Use `/dev/input/by-id/` paths in config (stable across reboots). Implement `yap config detect-hotkey` to interactively scan.

**Phase:** Phase 4 (Input/Output)

---

### 5. Clipboard Restore Race on Wayland
**Problem:** `xdotool type` silently fails on native Wayland windows (GNOME security model blocks injection). Exit code is 0, so the error is invisible. Clipboard may be overwritten before paste completes.

**Warning signs:** Text never appears at cursor on Wayland; clipboard content corrupted after transcription.

**Prevention:**
1. Detect session: `if $WAYLAND_DISPLAY != ""` → use `ydotool` path
2. Check `ydotool` socket permissions: `/run/ydotool.sock` must be accessible
3. Implement fallback chain: `wtype → ydotool → xdotool → clipboard-only`
4. Only restore clipboard after confirmed paste success (check exit code + verify)

**Phase:** Phase 4 (Input/Output)

---

## Moderate Pitfalls

### 6. PortAudio Stream Not Closed on SIGKILL
**Problem:** If daemon exits without calling `Pa_Terminate()`, audio device stays locked. Other apps can't record.

**Prevention:** Deferred `stream.Close()` and `pa.Terminate()` in cleanup. Never call `os.Exit()` directly — always `return` through cleanup path. Use `context.WithCancel` + `signal.NotifyContext`.

**Phase:** Phase 3 (Daemon)

---

### 7. Orphaned Temp WAV Files After Crash
**Problem:** If yap crashes mid-recording with temp file on disk, disk space leaks silently.

**Prevention:** Keep audio in memory ring buffer (no temp files). If WAV must hit disk for any reason, scan and clean `$XDG_RUNTIME_DIR/yap-recording-*.wav` at daemon startup.

**Phase:** Phase 2 (Audio Pipeline)

---

### 8. Groq API Errors Treated as Success
**Problem:** HTTP client returns `err == nil` on 4xx/5xx responses. Without explicit status check, error body is deserialized as transcript or silently dropped.

**Prevention:** Always check `resp.StatusCode` explicitly. Wrap in helper: `if resp.StatusCode != 200 { return fmt.Errorf("groq api: %d %s", ...) }`. Implement 30s timeout on HTTP client.

**Phase:** Phase 4 (Input/Output)

---

### 9. evdev Exclusive Grab Locks Desktop
**Problem:** `EVIOCGRAB` gives exclusive access to input device — no other application (including desktop) receives events. Accidentally grabbing locks the entire keyboard.

**Prevention:** Never use `EVIOCGRAB` in yap. evdev read without grab is sufficient for hotkey detection.

**Phase:** Phase 4 (Input/Output)

---

### 10. Nix Packaging — CGo Headers Not Found
**Problem:** `pkg-config --libs portaudio-2.0` fails in Nix build sandbox because portaudio pkg-config file is not on `PKG_CONFIG_PATH`.

**Warning signs:** `# cgo pkg-config: exit status 1` during `nix build`.

**Prevention:**
```nix
buildInputs = [ pkgs.portaudio ];
nativeBuildInputs = [ pkgs.pkg-config ];
```
Also set `CGO_ENABLED = "1"` in `buildPhase`.

**Phase:** Phase 1 (Foundation)

---

### 11. `file.Fd()` Resets evdev Nonblocking Mode
**Problem:** `go-evdev` uses `NonBlock()` for non-blocking reads. Calling `file.Fd()` on the underlying file resets it to blocking mode (Go runtime behavior).

**Prevention:** Never call `Fd()` after `NonBlock()`. Use `go-evdev`'s own API exclusively after opening.

**Phase:** Phase 4 (Input/Output)

---

## Minor Pitfalls

### 12. WAV Written Without RIFF Header
**Problem:** Raw PCM bytes submitted to Groq API return HTTP 400. Groq expects a valid WAV file with RIFF/fmt/data headers.

**Prevention:** Always use `github.com/go-audio/wav` encoder, never write raw PCM to disk/memory.

**Phase:** Phase 2 (Audio Pipeline)

---

### 13. xdotool Paste Timing Race on X11
**Problem:** `xdotool type` on X11 can fire before clipboard is fully set, pasting old content.

**Prevention:** Add 150ms minimum delay between `xclip -in` and `xdotool type --clearmodifiers`. Use `xdotool type` with `--delay 0` for the type itself (delay is between keystrokes, not before first).

**Phase:** Phase 4 (Input/Output)

---

### 14. PortAudio CGo Pointer Crash (Pre-Go 1.6)
**Problem:** Older `gordonklaus/portaudio` versions pass Go pointers to CGo across callbacks — illegal in Go 1.6+ cgo rules, causes runtime panic.

**Prevention:** Pin to a recent commit (post-2018). The fix uses `cgo.Handle` correctly. Check the callback implementation passes only C-allocated pointers.

**Phase:** Phase 1 (Foundation)

---

### 15. Embedded Chime WAVs Too Large
**Problem:** Embedding 1MB+ WAV files bloats the binary and slows curl install.

**Prevention:** Encode chimes at 16kHz mono PCM. Target under 100KB per chime (< 3 seconds). Use `ffmpeg -ar 16000 -ac 1` to downsample.

**Phase:** Phase 1 (Foundation)

---

## Phase Mapping Summary

| Phase | Pitfalls to Address |
|-------|---------------------|
| Phase 1 (Foundation) | #1 CGo static linking, #10 Nix CGo headers, #14 PortAudio CGo pointer, #15 Chime size |
| Phase 2 (Audio) | #2 PipeWire compat, #7 Temp files, #12 WAV headers |
| Phase 3 (Daemon) | #6 Stream cleanup on exit |
| Phase 4 (Input/Output) | #3 evdev permissions, #4 Wrong device, #5 Clipboard race, #8 API errors, #9 evdev grab, #11 NonBlock, #13 xdotool timing |
| Phase 5 (Polish) | #3 NixOS module auto-adds input group |

---
*Researched: 2026-03-07 | Confidence: High*
