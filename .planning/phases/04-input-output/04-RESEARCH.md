# Phase 4: Input + Output - Research

**Researched:** 2026-03-08
**Domain:** evdev hotkey input, Groq Whisper API transcription, display-server-aware paste, desktop notifications
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Error Handling & Retry Strategy
- Groq API transient failures (timeout, 5xx): Retry up to 3 times with exponential backoff (500ms, 1s, 2s)
- Groq 4xx errors (invalid API key, bad request): Fail immediately with error notification (don't retry)
- Failed transcription after all retries: Notify user with the exact error message from API
- If transcription fails, leave audio data in memory (don't write to temp file); offer no recovery UI (user re-records)

#### Recording Feedback During Capture
- At 50 seconds of 60-second max: play a warning beep via PortAudio (same stream as start/stop chimes)
- Warning beep is non-blocking (doesn't pause recording)
- At 60 seconds exactly: auto-stop recording (force release), transcribe, and paste
- User cannot extend recording beyond 60s; timeout is absolute

#### Wayland Paste Method Selection
- Priority order for Wayland: `wtype` first → `ydotool` second → clipboard-only fallback
- Auto-detect at runtime: use ydotool socket check + executable existence
- Not user-configurable (yap philosophy: sensible defaults, minimal config surface)
- Switch to next method immediately on failure; don't retry same method

#### Paste Verification & Clipboard Safety
- Save clipboard before any paste attempt via `atotto/clipboard`
- After paste attempt (xdotool, wtype, or ydotool): check exit code
- If paste exit code indicates success (0), restore clipboard content after 100ms delay
- If paste exit code indicates failure: leave clipboard unchanged (text stays available for manual paste)
- No retry logic on paste failure (single attempt per method in the chain)

#### Hotkey Listener Initialization
- Scan `/dev/input/event*` for devices with keyboard capability (KEY_A–KEY_Z in bitmask)
- If no keyboard devices found: error with exact `usermod -aG input $USER` command
- If permission denied on any device: emit same actionable error message and exit non-zero
- Once listener starts, never grab (EVIOCGRAB) — allow other apps to receive input
- Non-blocking mode on event file descriptor; poll-based loop via goroutine

#### Transcription Timeout
- HTTP client timeout for Groq API: 30 seconds (accounts for slow networks and API latency)
- If timeout: treat as retryable failure (counts toward 3-retry limit)

#### Clipboard Preservation on X11
- X11 paste via `xdotool type --clearmodifiers` after 150ms delay
- After paste: restore clipboard using `atotto/clipboard`
- Delay justifies: xdotool window focus + keyboard event delivery takes ~50–100ms
- Full cycle ensures user clipboard state is safe even if paste is interrupted

### Claude's Discretion
- Exact backoff timing for retries (user chose exponential backoff, Claude picks specific ms values)
- Warning beep frequency/duration (consistent with start/stop chimes)
- Clipboard restore delay jitter (to avoid race conditions)
- Error notification UI formatting (title, body, icons)

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| INPUT-01 | Hotkey detection via `github.com/holoplot/go-evdev`; pure Go, no CGo | evdev API confirmed: `evdev.Open()`, `dev.ReadOne()`, `evdev.ListDevicePaths()` |
| INPUT-02 | evdev device scanner filters by keyboard capability bitmask (KEY_A–KEY_Z); ignores non-keyboard devices | `dev.CapableEvents(evdev.EV_KEY)` returns `[]EvCode`; check for `KEY_A`..`KEY_Z` membership |
| INPUT-03 | EVIOCGRAB (exclusive grab) never used | Confirmed: `dev.Grab()` must NOT be called; other apps still receive input |
| INPUT-04 | `file.Fd()` never called after `NonBlock()` | Official pitfall in go-evdev docs: calling `file.Fd()` resets FD to blocking after `NonBlock()` |
| INPUT-05 | Hold-to-talk loop: key press → start + chime; release → stop + chime → transcribe → paste | Event value=1 (press), value=0 (release), value=2 (repeat/ignore); loop in goroutine |
| INPUT-06 | On permission denied, emit actionable `usermod -aG input $USER` command | `os.IsPermission(err)` on `evdev.Open()` errors |
| OUTPUT-01 | Paste method selected at runtime: detect `$WAYLAND_DISPLAY` for Wayland, `$DISPLAY` for X11 | `os.Getenv("WAYLAND_DISPLAY")` non-empty → Wayland; else `os.Getenv("DISPLAY")` → X11 |
| OUTPUT-02 | Wayland paste fallback chain: `ydotool` → `wtype` → clipboard-only | Note: CONTEXT.md says wtype first, REQUIREMENTS.md says ydotool first — CONTEXT.md is locked |
| OUTPUT-03 | X11 paste via `xdotool type --clearmodifiers` with 150ms delay | `exec.Command("xdotool", "type", "--clearmodifiers", text)` after 150ms sleep |
| OUTPUT-04 | `ydotool` path checks socket at `/run/ydotool.sock` for accessibility before invoking | Check `os.Stat(socketPath)` — default `/tmp/.ydotool_socket` or `$YDOTOOL_SOCKET` env |
| OUTPUT-05 | `xdotool` exit code checked; Wayland silent-success is treated as failure | `cmd.Run()` non-nil error = failure; also check for empty DISPLAY on Wayland |
| OUTPUT-06 | Clipboard saved before paste via `github.com/atotto/clipboard`; restored after confirmed success | `clipboard.ReadAll()` before paste; `clipboard.WriteAll(saved)` after success |
| OUTPUT-07 | Clipboard restoration only on confirmed paste success (not failure) | Conditional restore based on `cmd.Run()` exit code |
| TRANS-01 | Transcription via Groq Whisper API (`whisper-large-v3-turbo` model) | `POST https://api.groq.com/openai/v1/audio/transcriptions` with `model=whisper-large-v3-turbo` |
| TRANS-02 | API client uses stdlib `net/http` + `mime/multipart`; no third-party SDK | `multipart.NewWriter`, `writer.CreateFormFile("file", "audio.wav")`, `writer.WriteField("model", ...)` |
| TRANS-03 | HTTP client has explicit 30-second timeout | `&http.Client{Timeout: 30 * time.Second}` |
| TRANS-04 | `resp.StatusCode` checked explicitly; 4xx/5xx treated as errors | Groq error JSON: `{"error":{"message":"...","type":"..."}}` |
| TRANS-05 | API key read from config; falls back to `GROQ_API_KEY` env var | Already handled by `config.Load()` + `applyEnvOverrides()` |
| TRANS-06 | Transcription errors surfaced via OS notification | `beeep.Notify("yap error", errorMsg, "")` |
| NOTIFY-01 | OS error notifications via `github.com/gen2brain/beeep`; falls back to `notify-send` | `beeep.Notify(title, message, icon)` — already listed in go.mod dependencies (to be added) |
| NOTIFY-02 | Notification on: transcription API error, device permission error, audio device not found | Three notification call sites identified |
| NFR-04 | End-to-end latency (hotkey release → text at cursor) under 2 seconds on typical broadband | Groq whisper-large-v3-turbo: 216x real-time factor; WAV pre-encoded in memory; paste adds ~150ms |
</phase_requirements>

---

## Summary

Phase 4 wires together all previously built subsystems — audio recording (Phase 2), daemon lifecycle (Phase 3), and IPC (Phase 3) — into a complete hold-to-talk pipeline. The three major technical domains are: (1) evdev keyboard input handling with proper device scanning and permission error messaging, (2) Groq Whisper API transcription over a raw `net/http` multipart request with retry logic, and (3) display-server-aware text pasting via `xdotool` (X11) or `wtype`/`ydotool`/clipboard fallback (Wayland).

The primary integration complexity lies in the hold-to-talk state machine living inside `daemon.Run()`, coordinating context cancellation for recording, non-blocking chime playback, the 50s/60s timeout warning, and the transcribe-then-paste sequence. The clipboard save/restore cycle requires careful ordering to prevent clipboard corruption on failure paths.

Key constraint: `atotto/clipboard` is a runtime dependency on external tools (`xclip`, `xsel`, or `wl-clipboard`) — this is known, accepted, and consistent with `xdotool`/`wtype`/`ydotool` also being runtime external dependencies. The binary remains statically linked; runtime tools must exist on the user's system.

**Primary recommendation:** Build Phase 4 in three atomic plans: (1) evdev hotkey listener package, (2) Groq transcription client with retry, (3) hold-to-talk integration in daemon.Run() with paste fallback chain and clipboard safety.

---

## Standard Stack

### Core (all already chosen — locked decisions)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/holoplot/go-evdev` | latest (no semver tags) | Linux keyboard input via evdev | Pure Go, maintained, no CGo — locked in REQUIREMENTS.md |
| `github.com/atotto/clipboard` | v0.1.4 | Save/restore clipboard around paste | Pure Go API; delegates to `xclip`/`xsel`/`wl-clipboard` at runtime |
| `github.com/gen2brain/beeep` | latest | Desktop notifications | Pure Go via dbus; fallback to notify-send — locked in REQUIREMENTS.md |
| stdlib `net/http` + `mime/multipart` | Go stdlib | Groq Whisper API client | No SDK needed for single endpoint — locked in REQUIREMENTS.md |
| stdlib `os/exec` | Go stdlib | Invoke xdotool, wtype, ydotool | Standard Go subprocess invocation |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `time.Sleep` / `time.After` | stdlib | 150ms paste delay, 50s/60s warning timing | In hold-to-talk loop; within recording goroutine |
| `context.WithCancel` | stdlib | Stop recording on key release or timeout | Cancel passed to `audio.Recorder.Start()` |
| `encoding/json` | stdlib | Decode Groq API error responses | Parse `{"error":{"message":"..."}}` on non-200 |
| `fmt.Errorf` with `%w` | stdlib | Error wrapping through the call stack | Consistent with existing codebase patterns |

### Alternatives NOT Used (locked out)

| Instead of | Not Using | Reason |
|------------|-----------|--------|
| `gvalkov/golang-evdev` | CGo, unmaintained | Locked in REQUIREMENTS.md |
| `golang-design/clipboard` | CGo on Linux | Locked in REQUIREMENTS.md |
| Any Groq Go SDK | Overkill for one endpoint | Locked in REQUIREMENTS.md |
| `cenkalti/backoff` | External dependency | Simple 3-retry schedule; not worth a dep |

### New Dependencies to Add

```bash
go get github.com/holoplot/go-evdev@latest
go get github.com/atotto/clipboard@v0.1.4
go get github.com/gen2brain/beeep@latest
```

These are all currently absent from `go.mod`. All three must be added in Wave 0.

---

## Architecture Patterns

### Recommended Package Structure

```
internal/
├── hotkey/          # evdev device scanning + hold-to-talk event loop
│   ├── hotkey.go    # Listener struct, Open(), Run(ctx), device scanner
│   └── hotkey_test.go
├── transcribe/      # Groq Whisper API client
│   ├── transcribe.go  # Transcribe(ctx, apiKey, wavBytes, language) (string, error)
│   └── transcribe_test.go
├── paste/           # Display-server-aware paste + clipboard safety
│   ├── paste.go     # Paste(text string) error — auto-detects X11/Wayland
│   └── paste_test.go
└── daemon/
    └── daemon.go    # UPDATED: integrate hotkey loop + recording state machine
```

### Pattern 1: evdev Device Scanner

**What:** Scan all `/dev/input/event*` devices, filter for keyboard-capable ones (support `KEY_A`..`KEY_Z`), return all matches. Open them all — a user may have multiple keyboards.

**When to use:** At daemon startup, once.

```go
// Source: https://pkg.go.dev/github.com/holoplot/go-evdev
func findKeyboards() ([]*evdev.InputDevice, error) {
    paths, err := evdev.ListDevicePaths()
    if err != nil {
        return nil, fmt.Errorf("list input devices: %w", err)
    }

    var keyboards []*evdev.InputDevice
    var permErr error

    for _, p := range paths {
        dev, err := evdev.Open(p.Path)
        if err != nil {
            if os.IsPermission(err) {
                permErr = err // remember but continue
                continue
            }
            continue // skip unreadable devices
        }

        // Filter: must support KEY_A through KEY_Z
        codes := dev.CapableEvents(evdev.EV_KEY)
        if hasAlphaKeys(codes) {
            keyboards = append(keyboards, dev)
        } else {
            dev.Close()
        }
    }

    if len(keyboards) == 0 {
        if permErr != nil {
            return nil, fmt.Errorf(
                "permission denied on /dev/input/event* — fix with: usermod -aG input $USER",
            )
        }
        return nil, fmt.Errorf("no keyboard devices found")
    }

    return keyboards, nil
}

func hasAlphaKeys(codes []evdev.EvCode) bool {
    for _, c := range codes {
        if c >= evdev.KEY_A && c <= evdev.KEY_Z {
            return true
        }
    }
    return false
}
```

### Pattern 2: Hold-to-Talk Event Loop

**What:** Poll all keyboard devices in goroutines, send events to a channel. Main goroutine processes events: press=start recording, release=stop.

**When to use:** Inside `daemon.Run()` after device scan.

**Critical pitfall:** NEVER call `dev.NonBlock()` before reading if you intend to use `file.Fd()` afterward — `file.Fd()` resets the FD to blocking mode (INPUT-04). Use `dev.NonBlock()` and only use `dev.ReadOne()`.

```go
// Source: https://pkg.go.dev/github.com/holoplot/go-evdev (NonBlock pitfall)
// Pattern: each device gets its own goroutine; events sent to shared channel.

type keyEvent struct {
    code  evdev.EvCode
    value int32 // 1=press, 0=release, 2=repeat
}

func listenDevices(ctx context.Context, devs []*evdev.InputDevice, events chan<- keyEvent) {
    for _, dev := range devs {
        dev := dev
        dev.NonBlock() // INPUT-04: after this, NEVER call dev.Fd()
        go func() {
            for {
                select {
                case <-ctx.Done():
                    return
                default:
                }
                ev, err := dev.ReadOne()
                if err != nil {
                    // EAGAIN on non-blocking FD is normal (no event available)
                    time.Sleep(10 * time.Millisecond)
                    continue
                }
                if ev.Type == evdev.EV_KEY {
                    select {
                    case events <- keyEvent{code: evdev.EvCode(ev.Code), value: ev.Value}:
                    case <-ctx.Done():
                        return
                    }
                }
            }
        }()
    }
}
```

### Pattern 3: Hold-to-Talk State Machine

**What:** Process key events: match hotkey code → trigger recording start/stop. Include 50s warning and 60s absolute timeout.

**When to use:** In `daemon.Run()` main goroutine.

```go
// Source: project pattern; extends audio.Recorder.Start(ctx) with cancel
func (d *Daemon) holdToTalk(ctx context.Context, hotkeyCode evdev.EvCode, events <-chan keyEvent) {
    var recCancel context.CancelFunc
    var recDone chan struct{}

    for {
        select {
        case <-ctx.Done():
            if recCancel != nil {
                recCancel()
            }
            return
        case ev := <-events:
            if ev.code != hotkeyCode {
                continue
            }
            switch ev.value {
            case 1: // key press — start recording
                if recCancel != nil {
                    break // already recording; ignore repeat
                }
                audio.PlayChime(assets.StartChime())
                recCtx, cancel := context.WithCancel(ctx)
                recCancel = cancel
                recDone = make(chan struct{})
                go d.recordAndTranscribe(recCtx, recDone)

            case 0: // key release — stop recording
                if recCancel == nil {
                    break
                }
                recCancel()
                recCancel = nil
                audio.PlayChime(assets.StopChime())
                // recDone signals when transcription + paste is complete
            }
        }
    }
}
```

### Pattern 4: Recording with Timeout

**What:** Run `rec.Start(ctx)` in a goroutine. Separately track elapsed time; at 50s play warning beep; at 60s cancel recording context (auto-stop).

```go
// Source: internal/audio/audio.go blocking Start(ctx) pattern
func (d *Daemon) recordWithTimeout(ctx context.Context) ([]byte, error) {
    recCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
    defer cancel()

    // Warning beep at 50s
    go func() {
        select {
        case <-time.After(50 * time.Second):
            audio.PlayChime(assets.WarningChime()) // non-blocking
        case <-recCtx.Done():
        }
    }()

    // Start blocks until ctx cancelled or timeout
    if err := d.recorder.Start(recCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
        return nil, fmt.Errorf("recording: %w", err)
    }

    return d.recorder.Encode()
}
```

### Pattern 5: Groq Transcription with Retry

**What:** POST WAV bytes as multipart form to Groq API. Retry 5xx and timeouts up to 3 times with backoff 500ms→1s→2s. Fail immediately on 4xx.

```go
// Source: https://console.groq.com/docs/speech-to-text + net/http stdlib
func Transcribe(ctx context.Context, apiKey string, wavData []byte, language string) (string, error) {
    delays := []time.Duration{500 * time.Millisecond, 1 * time.Second, 2 * time.Second}
    var lastErr error

    for attempt := 0; attempt <= 3; attempt++ {
        text, err := transcribeOnce(ctx, apiKey, wavData, language)
        if err == nil {
            return text, nil
        }

        var apiErr *APIError
        if errors.As(err, &apiErr) && apiErr.StatusCode/100 == 4 {
            return "", err // 4xx: don't retry (TRANS-04)
        }

        lastErr = err
        if attempt < len(delays) {
            select {
            case <-time.After(delays[attempt]):
            case <-ctx.Done():
                return "", ctx.Err()
            }
        }
    }
    return "", lastErr
}

func transcribeOnce(ctx context.Context, apiKey string, wavData []byte, language string) (string, error) {
    var body bytes.Buffer
    w := multipart.NewWriter(&body)

    part, _ := w.CreateFormFile("file", "audio.wav")
    part.Write(wavData)
    w.WriteField("model", "whisper-large-v3-turbo")
    w.WriteField("language", language)
    w.WriteField("response_format", "json")
    w.Close()

    req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
        "https://api.groq.com/openai/v1/audio/transcriptions", &body)
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Content-Type", w.FormDataContentType())

    client := &http.Client{Timeout: 30 * time.Second} // TRANS-03
    resp, err := client.Do(req)
    if err != nil {
        return "", fmt.Errorf("http request: %w", err) // retryable
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)
    if resp.StatusCode != http.StatusOK {
        // Parse Groq error JSON: {"error":{"message":"...","type":"..."}}
        return "", &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
    }

    var result struct {
        Text string `json:"text"`
    }
    if err := json.Unmarshal(respBody, &result); err != nil {
        return "", fmt.Errorf("decode response: %w", err)
    }
    return result.Text, nil
}
```

### Pattern 6: Display-Server-Aware Paste

**What:** Detect display server from environment; pick paste method; save/restore clipboard.

**Important:** CONTEXT.md says Wayland priority is `wtype` first → `ydotool` second. REQUIREMENTS.md OUTPUT-02 says `ydotool` first — use CONTEXT.md (locked decision wins).

```go
// Source: os.Getenv detection + exec.Command patterns
func Paste(text string) error {
    saved, _ := clipboard.ReadAll() // OUTPUT-06: save clipboard before paste

    var pasteErr error
    if os.Getenv("WAYLAND_DISPLAY") != "" {
        pasteErr = pasteWayland(text) // OUTPUT-01
    } else if os.Getenv("DISPLAY") != "" {
        pasteErr = pasteX11(text) // OUTPUT-01
    } else {
        pasteErr = fmt.Errorf("no display server detected")
    }

    if pasteErr == nil && saved != "" {
        // OUTPUT-07: Only restore on success
        time.Sleep(100 * time.Millisecond) // let paste complete before restoring
        clipboard.WriteAll(saved)
    }
    // On failure: leave clipboard set to transcript text (user can paste manually)
    return pasteErr
}

func pasteWayland(text string) error {
    // Try wtype first (CONTEXT.md: wtype → ydotool → clipboard-only)
    if _, err := exec.LookPath("wtype"); err == nil {
        if err := exec.Command("wtype", "--", text).Run(); err == nil {
            return nil
        }
    }

    // Try ydotool (OUTPUT-04: check socket first)
    if canUseYdotool() {
        if err := exec.Command("ydotool", "type", "--", text).Run(); err == nil {
            return nil
        }
    }

    // Clipboard-only fallback (OUTPUT-02)
    return clipboard.WriteAll(text) // leaves text in clipboard, no auto-paste
}

func canUseYdotool() bool {
    // OUTPUT-04: check socket accessibility
    socketPath := os.Getenv("YDOTOOL_SOCKET")
    if socketPath == "" {
        socketPath = "/tmp/.ydotool_socket"
    }
    _, err := os.Stat(socketPath)
    if err != nil {
        return false
    }
    _, err = exec.LookPath("ydotool")
    return err == nil
}

func pasteX11(text string) error {
    // OUTPUT-03: 150ms delay; --clearmodifiers for layout safety
    time.Sleep(150 * time.Millisecond)
    return exec.Command("xdotool", "type", "--clearmodifiers", "--", text).Run()
}
```

### Anti-Patterns to Avoid

- **Calling `dev.Grab()` on evdev devices:** Prevents other applications from receiving keyboard input; violates INPUT-03. Never use it.
- **Calling `dev.Fd()` after `dev.NonBlock()`:** Resets the file descriptor to blocking mode silently; breaks the polling loop (INPUT-04). Once NonBlock() is called, only use `dev.ReadOne()`.
- **Checking `WAYLAND_DISPLAY` on a non-XWayland session and using xdotool:** xdotool does not work on Wayland even with `$DISPLAY` set via XWayland. Always check `WAYLAND_DISPLAY` first.
- **Retrying 4xx errors from Groq API:** A 401 (invalid API key) or 400 (bad request) will never succeed; retrying wastes time and confuses users.
- **Writing audio to disk before transcription:** Violates NFR-06. WAV bytes live in memory from `rec.Encode()` → direct to HTTP multipart body.
- **Blocking the main goroutine on recording:** `rec.Start(ctx)` must run in a goroutine; daemon must remain responsive to IPC commands and SIGTERM during recording.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Linux keyboard events | Custom ioctl syscalls | `holoplot/go-evdev` | ioctl bitmask parsing is subtle; NonBlock/Fd interaction is non-obvious |
| Clipboard read/write | xclip/xsel subprocess wrappers | `atotto/clipboard` | Handles X11 primary vs clipboard selection; cross-platform API |
| Desktop notifications | dbus connection management | `gen2brain/beeep` | dbus protocol setup is complex; beeep handles fallback chain |
| Multipart HTTP body | Custom boundary generation | stdlib `mime/multipart` | RFC 2046 compliance; boundary uniqueness — stdlib handles correctly |
| Exponential backoff | Custom sleep loop with no jitter | Inline delay slice `[500ms, 1s, 2s]` | For 3-retry limit, inline is simpler than a library dep; add jitter |

**Key insight:** The paste path (`xdotool`, `wtype`, `ydotool`) requires external system binaries — these are runtime deps that must be present. The binary is statically compiled but paste requires the display server tooling to be installed.

---

## Common Pitfalls

### Pitfall 1: NonBlock + Fd() Silent Regression
**What goes wrong:** After calling `dev.NonBlock()`, any subsequent call to `dev.Fd()` (even indirectly via Go's `os.File` internals) resets the file descriptor to blocking mode. The event loop appears to work but blocks on the first `ReadOne()` with no events.
**Why it happens:** Go's `os.File.Fd()` calls `syscall.SetNonblock(fd, false)` as part of its FD-to-uintptr conversion to prevent GC issues.
**How to avoid:** After `dev.NonBlock()`, use ONLY `dev.ReadOne()`. Never access `dev.Fd()`. Never pass the device to any function that touches the underlying FD.
**Warning signs:** Event loop goroutine never returns; CPU usage near 0% during key presses.

### Pitfall 2: EVIOCGRAB Blocks All Input
**What goes wrong:** Calling `dev.Grab()` causes other applications (browser, terminal, the compositor itself) to stop receiving keyboard input while yap is running.
**Why it happens:** EVIOCGRAB is an exclusive claim on the device at the kernel level.
**How to avoid:** Never call `dev.Grab()`. INPUT-03 is explicit. No exceptions.
**Warning signs:** User can't type in any other application while yap daemon is running.

### Pitfall 3: Wayland xdotool Silent Failure
**What goes wrong:** On Wayland sessions, `$DISPLAY` is often set (XWayland), so xdotool runs without error but types nothing — exit code 0, no paste.
**Why it happens:** xdotool uses the X11 XTEST extension which doesn't affect Wayland compositor focus.
**How to avoid:** Check `$WAYLAND_DISPLAY` first; if non-empty, use Wayland path regardless of whether `$DISPLAY` is also set.
**Warning signs:** User runs yap on Hyprland/Sway; transcription completes but nothing appears at cursor.

### Pitfall 4: ydotool Socket Path Variations
**What goes wrong:** The ydotool daemon socket path varies: `/tmp/.ydotool_socket` (old default), `/run/ydotool.sock` (proposed new default), `/run/user/$UID/.ydotool_socket` (per-user systemd). Hardcoding any one path breaks many setups.
**Why it happens:** ydotool has historically used different paths across versions and distros.
**How to avoid:** Check `$YDOTOOL_SOCKET` env var first; fall back to `/tmp/.ydotool_socket`. Use `os.Stat()` to check existence before invoking.
**Warning signs:** "failed to connect to socket" in ydotool stderr even when ydotoold is running.

### Pitfall 5: Clipboard Race on X11
**What goes wrong:** Clipboard save/restore happens so fast that the paste target window hasn't received the keyboard events yet. The clipboard gets restored before xdotool finishes typing.
**Why it happens:** xdotool is asynchronous — it enqueues X events; the target receives them asynchronously.
**How to avoid:** The 150ms delay before xdotool invocation handles focus acquisition. After xdotool returns (exit 0), wait an additional 100ms before restoring clipboard. The user's chosen flow already specifies both delays.
**Warning signs:** Clipboard contains transcribed text instead of original content after paste.

### Pitfall 6: Groq 4xx Retry Loop
**What goes wrong:** An invalid API key (401) causes yap to retry 3 times with backoff, resulting in a 3.5-second delay before showing the error notification. User thinks yap is frozen.
**Why it happens:** Error classification logic misses the "is this a 4xx?" check.
**How to avoid:** In `transcribeOnce()`, check `resp.StatusCode/100 == 4`; if true, return a non-retryable error type. The retry loop checks the type and bails immediately.
**Warning signs:** 3-4 second delay between recording release and error notification for invalid API key errors.

### Pitfall 7: Hold-to-Talk Goroutine Leak
**What goes wrong:** If the daemon receives SIGTERM while recording is in progress, the recording goroutine continues after `daemon.Run()` returns, because its context wasn't cancelled.
**Why it happens:** Recording context is derived from a local cancel — if the daemon's root context cancels, the local cancel must also be called.
**How to avoid:** Recording context must be derived from `ctx` passed to daemon (via `context.WithCancel(ctx)`) so SIGTERM cancels both.
**Warning signs:** `go test` hangs with timeout; goroutine leak detector reports leaked goroutines.

### Pitfall 8: atotto/clipboard Runtime Deps Missing
**What goes wrong:** On a minimal system without `xclip`, `xsel`, or `wl-clipboard`, clipboard save/restore fails silently (or panics). Transcribed text is lost.
**Why it happens:** `atotto/clipboard` shells out to these tools; if none are found, it returns an error.
**How to avoid:** Check `clipboard.ReadAll()` error before paste. If clipboard save fails, skip restoration (don't abort the paste attempt). Log a warning. This is consistent with the user's "no recovery UI" policy.
**Warning signs:** Clipboard restoration never happens on minimal container/CI environments.

---

## Code Examples

### evdev: Parse Hotkey String from Config

```go
// Source: https://pkg.go.dev/github.com/holoplot/go-evdev (KEYFromString)
// Config stores hotkey as "KEY_RIGHTCTRL"; convert to EvCode for comparison.

func hotkeyCode(name string) (evdev.EvCode, error) {
    code, ok := evdev.KEYFromString[name]
    if !ok {
        return 0, fmt.Errorf("unknown hotkey %q — use KEY_* names from evdev (e.g. KEY_RIGHTCTRL)", name)
    }
    return code, nil
}
```

### Groq API: Detect Retryable vs Fatal Errors

```go
// Source: https://console.groq.com/docs/errors
// Error JSON: {"error":{"message":"...","type":"..."}}

type groqErrorBody struct {
    Error struct {
        Message string `json:"message"`
        Type    string `json:"type"`
    } `json:"error"`
}

type APIError struct {
    StatusCode int
    Message    string
}

func (e *APIError) Error() string {
    return fmt.Sprintf("groq API error %d: %s", e.StatusCode, e.Message)
}

func parseAPIError(statusCode int, body []byte) *APIError {
    var eb groqErrorBody
    if err := json.Unmarshal(body, &eb); err != nil {
        return &APIError{StatusCode: statusCode, Message: string(body)}
    }
    return &APIError{StatusCode: statusCode, Message: eb.Error.Message}
}
```

### Notification: Actionable Error Format

```go
// Source: https://pkg.go.dev/github.com/gen2brain/beeep
// Consistent with CONTEXT.md: specific message, not "transcription failed"

func notifyError(title, detail string) {
    beeep.Notify("yap: "+title, detail, "")
}

// Usage:
notifyError("transcription failed", "API error 401: invalid API key")
notifyError("hotkey setup failed",
    "permission denied on /dev/input/event* — fix with: usermod -aG input $USER")
```

### IPC: Update daemon.Run() to Integrate Hotkey Loop

```go
// Source: internal/daemon/daemon.go (existing pattern to extend)
// Phase 4 adds hotkey loop after IPC server start.

func Run(cfg *config.Config) error {
    // ... existing PID file, PortAudio, signal setup ...

    // Phase 4: Start hotkey listener
    keyboards, err := hotkey.FindKeyboards()
    if err != nil {
        beeep.Notify("yap: hotkey setup failed", err.Error(), "")
        return fmt.Errorf("hotkey setup: %w", err)
    }
    defer func() {
        for _, dev := range keyboards { dev.Close() }
    }()

    // Phase 4: Hold-to-talk loop (blocks until ctx cancelled)
    go hotkey.Run(ctx, keyboards, cfg, rec)

    <-ctx.Done()
    return nil
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `gvalkov/golang-evdev` (CGo) | `holoplot/go-evdev` (pure Go) | Ongoing — locked in REQUIREMENTS.md | Pure Go = no CGo compilation complexity in evdev path |
| `golang-design/clipboard` (CGo) | `atotto/clipboard` (pure Go API) | Locked in REQUIREMENTS.md | No CGo in clipboard path; runtime tools required |
| Groq SDK | stdlib `net/http` + `mime/multipart` | Locked in REQUIREMENTS.md | Zero dep; full control over retry logic |
| Hardcoded `/tmp/.ydotool_socket` | `$YDOTOOL_SOCKET` env → `/tmp/.ydotool_socket` fallback | ydotool issue #284 (2025) | Needed for compatibility across distros |

**Deprecated/outdated:**
- ydotool socket at `/run/ydotool.sock`: This path was proposed (GitHub issue #284, May 2025) but is NOT yet the default. Current default remains `/tmp/.ydotool_socket` + `$YDOTOOL_SOCKET` env override.
- `xdotool` on Wayland: Works only if `$DISPLAY` is set via XWayland AND compositor supports XTEST — unreliable. Check `$WAYLAND_DISPLAY` first.

---

## Open Questions

1. **wtype vs ydotool priority discrepancy**
   - What we know: CONTEXT.md (locked decision) says `wtype` first → `ydotool` second. REQUIREMENTS.md OUTPUT-02 says `ydotool` first → `wtype` second.
   - What's unclear: Which document takes precedence?
   - Recommendation: CONTEXT.md is the locked decision from user discussion. Use CONTEXT.md order: `wtype` first → `ydotool` second. Note this discrepancy in plan comments.

2. **atotto/clipboard Wayland support scope**
   - What we know: `atotto/clipboard` uses `wl-clipboard` (`wl-copy`/`wl-paste`) on Wayland. The package returns error if no clipboard tool is found.
   - What's unclear: Does atotto/clipboard auto-detect Wayland vs X11 and pick `wl-copy` vs `xclip`?
   - Recommendation: Treat clipboard save/restore as best-effort. If `clipboard.ReadAll()` errors, skip restoration (log warning). This is consistent with the "no recovery UI" philosophy.

3. **Warning beep audio asset**
   - What we know: The warning beep at 50s should use the same `PlayChime` infrastructure; no dedicated asset was generated in Phase 1.
   - What's unclear: Is a separate WAV asset needed, or can the stop chime serve double duty?
   - Recommendation: Generate a third chime asset (`warning_chime.wav`) in Wave 0 at a pitch between start (880Hz) and stop (660Hz), e.g. 770Hz. Embed it like the existing chimes.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` package |
| Config file | none (Go built-in) |
| Quick run command | `go test ./internal/hotkey/... ./internal/transcribe/... ./internal/paste/... -count=1 -timeout 30s` |
| Full suite command | `go test ./... -count=1 -timeout 60s` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INPUT-01 | go-evdev import compiles | unit | `go build ./internal/hotkey/...` | ❌ Wave 0 |
| INPUT-02 | hasAlphaKeys() filters keyboard devices | unit | `go test ./internal/hotkey/... -run TestHasAlphaKeys` | ❌ Wave 0 |
| INPUT-03 | No Grab() call in production code | unit (static check) | `go vet ./...` + code review | ❌ Wave 0 |
| INPUT-04 | NonBlock() + ReadOne() only (no Fd()) | unit | `go test ./internal/hotkey/... -run TestNonBlockSafe` | ❌ Wave 0 |
| INPUT-05 | hold-to-talk state: press→start, release→stop | unit | `go test ./internal/hotkey/... -run TestHoldToTalk` | ❌ Wave 0 |
| INPUT-06 | Permission denied → exact usermod message | unit | `go test ./internal/hotkey/... -run TestPermissionError` | ❌ Wave 0 |
| OUTPUT-01 | Display server detection (WAYLAND_DISPLAY/DISPLAY) | unit | `go test ./internal/paste/... -run TestDisplayDetection` | ❌ Wave 0 |
| OUTPUT-02 | Wayland chain: wtype → ydotool → clipboard | unit | `go test ./internal/paste/... -run TestWaylandChain` | ❌ Wave 0 |
| OUTPUT-03 | X11 xdotool invoked with --clearmodifiers | unit | `go test ./internal/paste/... -run TestX11Paste` | ❌ Wave 0 |
| OUTPUT-04 | ydotool socket check before invoke | unit | `go test ./internal/paste/... -run TestYdotoolSocketCheck` | ❌ Wave 0 |
| OUTPUT-05 | xdotool exit code checked | unit | `go test ./internal/paste/... -run TestXdotoolExitCode` | ❌ Wave 0 |
| OUTPUT-06 | Clipboard saved before paste | unit | `go test ./internal/paste/... -run TestClipboardSave` | ❌ Wave 0 |
| OUTPUT-07 | Clipboard restored only on success | unit | `go test ./internal/paste/... -run TestClipboardRestoreOnSuccess` | ❌ Wave 0 |
| TRANS-01 | whisper-large-v3-turbo model param | unit | `go test ./internal/transcribe/... -run TestModelParam` | ❌ Wave 0 |
| TRANS-02 | multipart form construction | unit | `go test ./internal/transcribe/... -run TestMultipartForm` | ❌ Wave 0 |
| TRANS-03 | 30-second HTTP timeout | unit | `go test ./internal/transcribe/... -run TestHTTPTimeout` | ❌ Wave 0 |
| TRANS-04 | 4xx = no retry; 5xx = retry | unit | `go test ./internal/transcribe/... -run TestRetryClassification` | ❌ Wave 0 |
| TRANS-05 | API key from config/env | unit | `go test ./internal/transcribe/... -run TestAPIKey` | ❌ Wave 0 |
| TRANS-06 | Error notification on failure | unit | `go test ./internal/transcribe/... -run TestErrorNotification` | ❌ Wave 0 |
| NOTIFY-01 | beeep.Notify() called with expected args | unit | `go test ./... -run TestNotification` | ❌ Wave 0 |
| NOTIFY-02 | All 3 notification trigger sites covered | unit | `go test ./... -run TestNotifyOnPermError/APIError/DeviceError` | ❌ Wave 0 |
| NFR-04 | Latency: Groq turbo ~216x real-time; paste ~150ms | manual | time a 3s recording release to paste | N/A |

### Sampling Rate
- **Per task commit:** `go test ./internal/hotkey/... ./internal/transcribe/... ./internal/paste/... -count=1 -timeout 30s`
- **Per wave merge:** `go test ./... -count=1 -timeout 60s`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/hotkey/hotkey.go` — covers INPUT-01 through INPUT-06
- [ ] `internal/hotkey/hotkey_test.go` — unit tests with fake device/event injection
- [ ] `internal/transcribe/transcribe.go` — covers TRANS-01 through TRANS-06
- [ ] `internal/transcribe/transcribe_test.go` — uses `httptest.NewServer` for fake Groq API
- [ ] `internal/paste/paste.go` — covers OUTPUT-01 through OUTPUT-07
- [ ] `internal/paste/paste_test.go` — unit tests with exec mock/stub pattern
- [ ] Add to `go.mod`: `go get github.com/holoplot/go-evdev@latest github.com/atotto/clipboard@v0.1.4 github.com/gen2brain/beeep@latest`

---

## Sources

### Primary (HIGH confidence)
- `pkg.go.dev/github.com/holoplot/go-evdev` — full API including `ListDevicePaths`, `CapableEvents`, `NonBlock` pitfall, `KEYFromString`
- `pkg.go.dev/github.com/atotto/clipboard` — `ReadAll()`/`WriteAll()` API, Linux runtime deps (xclip/xsel/wl-clipboard)
- `pkg.go.dev/github.com/gen2brain/beeep` — `Notify(title, message, icon)` signature, Linux dbus + notify-send fallback
- `console.groq.com/docs/speech-to-text` — endpoint, models, supported formats, response format
- `console.groq.com/docs/errors` — HTTP status codes (400/401/403/429/498/500/502/503), error JSON structure
- Existing codebase: `internal/audio/audio.go`, `internal/audio/chime.go`, `internal/daemon/daemon.go`, `internal/ipc/server.go`, `internal/config/config.go`

### Secondary (MEDIUM confidence)
- `github.com/holoplot/go-evdev` GitHub — confirmed pure Go, no CGo, MIT license, maintained
- `github.com/atotto/clipboard/blob/master/clipboard_unix.go` — confirmed Linux runtime deps: xclip, xsel, wl-clipboard
- `github.com/ReimuNotMoe/ydotool` — socket path `/tmp/.ydotool_socket`, `$YDOTOOL_SOCKET` env, `--socket-path` flag
- `github.com/atx/wtype` — Wayland-only, uses virtual-keyboard protocol, simple `wtype -- text` invocation
- `manpages.ubuntu.com/manpages/trusty/man1/xdotool.1.html` — `xdotool type --clearmodifiers` confirmed

### Tertiary (LOW confidence)
- ydotool GitHub issue #284 (May 2025): `/run/ydotool.sock` as proposed new default — not yet merged/default, flag only
- Latency estimate for NFR-04: Groq whisper-large-v3-turbo 216x real-time factor from Groq docs; actual network latency unverified

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries confirmed via pkg.go.dev; versions verified
- Architecture: HIGH — evdev patterns confirmed from official docs; Groq multipart pattern verified; paste patterns from manpages
- Pitfalls: HIGH — NonBlock/Fd() pitfall from official go-evdev docs; Wayland/xdotool confirmed from multiple sources; ydotool socket from GitHub issues

**Research date:** 2026-03-08
**Valid until:** 2026-04-08 (30 days for stable Go libraries; ydotool socket default may change sooner)
