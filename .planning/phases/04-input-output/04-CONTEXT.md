# Phase 4: Input + Output - Context

**Gathered:** 2026-03-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Hold-to-talk pipeline: Listen for hotkey press via evdev → start recording (play start chime) → on release, stop recording (play stop chime) → transcribe audio via Groq Whisper API → paste transcribed text at cursor using the correct display server method (xdotool for X11, wtype/ydotool for Wayland, clipboard fallback). Handle API failures and missing display server tooling gracefully via desktop notifications.

</domain>

<decisions>
## Implementation Decisions

### Error Handling & Retry Strategy
- Groq API transient failures (timeout, 5xx): Retry up to 3 times with exponential backoff (500ms, 1s, 2s)
- Groq 4xx errors (invalid API key, bad request): Fail immediately with error notification (don't retry)
- Failed transcription after all retries: Notify user with the exact error message from API
- If transcription fails, leave audio data in memory (don't write to temp file); offer no recovery UI (user re-records)

### Recording Feedback During Capture
- At 50 seconds of 60-second max: play a warning beep via PortAudio (same stream as start/stop chimes)
- Warning beep is non-blocking (doesn't pause recording)
- At 60 seconds exactly: auto-stop recording (force release), transcribe, and paste
- User cannot extend recording beyond 60s; timeout is absolute

### Wayland Paste Method Selection
- Priority order for Wayland: `wtype` first → `ydotool` second → clipboard-only fallback
- Auto-detect at runtime: use ydotool socket check + executable existence
- Not user-configurable (yap philosophy: sensible defaults, minimal config surface)
- Switch to next method immediately on failure; don't retry same method

### Paste Verification & Clipboard Safety
- Save clipboard before any paste attempt via `atotto/clipboard`
- After paste attempt (xdotool, wtype, or ydotool): check exit code
- If paste exit code indicates success (0), restore clipboard content after 100ms delay
- If paste exit code indicates failure: leave clipboard unchanged (text stays available for manual paste)
- No retry logic on paste failure (single attempt per method in the chain)

### Hotkey Listener Initialization
- Scan `/dev/input/event*` for devices with keyboard capability (KEY_A–KEY_Z in bitmask)
- If no keyboard devices found: error with exact `usermod -aG input $USER` command
- If permission denied on any device: emit same actionable error message and exit non-zero
- Once listener starts, never grab (EVIOCGRAB) — allow other apps to receive input
- Non-blocking mode on event file descriptor; poll-based loop via goroutine

### Transcription Timeout
- HTTP client timeout for Groq API: 30 seconds (accounts for slow networks and API latency)
- If timeout: treat as retryable failure (counts toward 3-retry limit)

### Clipboard Preservation on X11
- X11 paste via `xdotool type --clearmodifiers` after 150ms delay
- After paste: restore clipboard using `atotto/clipboard`
- Delay justifies: xdotool window focus + keyboard event delivery takes ~50–100ms
- Full cycle ensures user clipboard state is safe even if paste is interrupted

### Claude's Discretion
- Exact backoff timing for retries (user chose exponential backoff, Claude picks specific ms values)
- Warning beep frequency/duration (consistent with start/stop chimes)
- Clipboard restore delay jitter (to avoid race conditions)
- Error notification UI formatting (title, body, icons)

</decisions>

<specifics>
## Specific Ideas

- Hold-to-talk should feel responsive: chime feedback immediately on key press, no lag
- Timeout warning (50/60s beep) should be noticeable but not startling (similar pitch/duration to start chime)
- Error notifications should be specific: "API error: invalid API key" not "transcription failed"
- Wayland fallback should be transparent: if wtype fails, silently try ydotool without notifying user of the switch
- Clipboard safety is non-negotiable: user never loses their previous clipboard content

</specifics>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/audio/Recorder`: blocking stream pattern; can add recording timeout check in main loop
- `internal/daemon/Daemon`: signal-based lifecycle already handles SIGTERM; hotkey loop runs in main goroutine
- `internal/ipc/Server`: idempotent stop/status already implemented (safe for retries)
- Chime playback goroutines: async non-blocking pattern; warning beep uses same PlayChime infrastructure

### Established Patterns
- Closure injection for config (cfg passed to all command constructors)
- XDG paths via `adrg/xdg` (already used for data/config dirs)
- NDJSON IPC protocol with json.Encoder (proven in Phase 3)
- Error notifications via `gen2brain/beeep` (already available)
- Defer-based cleanup (deferred PortAudio/pidfile cleanup in daemon)

### Integration Points
- Hotkey loop runs in `Daemon.Run()` after PortAudio init (uses audio.Recorder context)
- Recording timeout checked in main record loop (incremental timestamp check every 100ms)
- Transcription happens after recording stops (Recorder.Data() returns WAV bytes)
- Paste runs after transcription succeeds (part of hold-to-talk sequence)
- All errors (hotkey, API, paste) trigger desktop notification before returning to idle state

</code_context>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 04-input-output*
*Context gathered: 2026-03-08*
