# Phase 4: Input + Output - Nyquist Validation

**Phase:** 04-input-output
**Validation Architecture:** Nyquist-style acceptance testing with UAT probes
**Generated:** 2026-03-08

## Summary

Phase 4 implements the complete hold-to-talk pipeline: evdev hotkey listening → PortAudio recording → Groq Whisper transcription → cross-platform pasting at cursor. This section defines test points to verify the end-to-end feature works correctly.

<validation_architecture>
## Validation Architecture

Phase 4 Nyquist validation follows the "user-in-loop" pattern:

1. **Unit Tests** (isolated components)
   - Hotkey device scanning and event handling
   - Groq API client retry logic and error classification
   - Paste method detection and execution
   - Desktop notification triggers

2. **Integration Tests** (2-component interactions)
   - Hotkey → recording state machine
   - Recording stop → transcription trigger
   - Transcription success → paste invocation
   - Paste failure → clipboard safety guarantee

3. **System Tests** (user-facing workflows)
   - Hold-to-talk end-to-end with chimes
   - Permission error handling with actionable error
   - Wayland paste method switching
   - Recording timeout behavior
   - API failure recovery

4. **UAT Probes** (real-world scenarios)
   - X11 hold-to-talk with clipboard preservation
   - Wayland GNOME hold-to-talk with ydotool/wtype fallbacks
   - Long recording (>50s) with warning chime
   - API key invalidation scenario
   - Multiple hotkey presses/releases

</validation_architecture>

## Test Scenarios by Requirement

### INPUT Requirements

#### INPUT-01: hotkey capture via evdev
**Test:** `yap hotkey config Ctrl+Shift+S` then press Ctrl+Shift+S
- **Given:** evdev device exists and is readable
- **When:** User presses configured hotkey combination
- **Then:** Daemon starts recording (play start chime)
- **And:** Callback registered via `hotkey.Listener.Run()` is invoked

#### INPUT-02: race-free hotkey (non-blocking)
**Test:** Rapid tap hotkey (500ms intervals, 10 taps)
- **Given:** Hotkey is configured
- **When:** User rapidly taps hotkey 10 times
- **Then:** Each press triggers recording start
- **And:** Each release triggers recording stop
- **And:** No goroutine leaks or blocked event descriptors

#### INPUT-03: device scanning
**Test:** Verify keyboard devices detected
- **Given:** System has multiple input devices
- **When:** Daemon starts
- **Then:** All devices with KEY_A–KEY_Z capabilities are found
- **And:** Non-keyboard devices (mouse, touchpad) are ignored

#### INPUT-04: NonBlock() safety
**Test:** Verify event loop remains responsive after NonBlock()
- **Given:** Hotkey listener initialized with NonBlock()
- **When:** User presses/releases hotkey 10 times
- **Then:** Event loop processes all events without blocking
- **And:** No Fd() called after NonBlock() (verified by unit tests)

#### INPUT-05: permission errors
**Test:** Trigger permission error scenario
- **Given:** User not in input group
- **When:** Attempt to read /dev/input/event*
- **Then:** Daemon prints exact: "usermod -aG input $USER"
- **And:** Daemon exits non-zero with error notification

#### INPUT-06: evdev grab safety
**Test:** Verify no exclusive grab
- **Given:** Hotkey listener running
- **When:** User opens text editor
- **Then:** Hotkey events delivered to both yap and text editor
- **And:** EVIOCGRAB never called on any device

### TRANS Requirements

#### TRANS-01: Groq API client
**Test:** Transcribe simple phrase
- **Given:** Groq API key configured
- **When:** Record 5-second "hello world" audio
- **Then:** Transcription returns "hello world"
- **And:** HTTP request uses model=whisper-large-vurbo

#### TRANS-02: API key fallback
**Test:** Use env var API key
- **Given:** GROQ_API_KEY env var set
- **When:** Config.APIKey is empty
- **Then:** Transcription uses env var key
- **And:** Same behavior as configured key

#### TRANS-03: error classification
**Test:** Verify error retry logic
- **Given:** Network timeout scenario
- **When:** Groq API returns 503 Service Unavailable
- **Then:** Retry after 500ms, then 1s, then 2s
- **And:** After 3 retries, fail with notification

#### TRANS-04: 4xx immediate fail
**Test:** Verify no retry on 4xx
- **Given:** Groq API returns 401 Invalid API Key
- **When:** Transcription attempted
- **Then:** Fail immediately (no retry)
- **And:** Desktop notification shows "API error: invalid API key"

#### TRANS-05: timeout handling
**Test:** Verify 30s timeout
- **Given:** Slow network (>30s to API)
- **When:** Transcription attempted
- **Then:** HTTP timeout after 30s
- **And:** Counts toward retry limit

#### TRANS-06: error notifications
**Test:** Transcription error notification
- **Given:** Transcription fails after all retries
- **When:** Error occurs
- **Then:** Desktop notification with exact API error
- **And:** Error logged to daemon log

### OUTPUT Requirements

#### OUTPUT-01: X11 pasting
**Test:** Paste on X11 system
- **Given:** DISPLAY env var set
- **When:** Transcription successful
- **Then:** Text pasted via xdotool type --clearmodifiers
- **And:** xdotool invoked with 150ms delay after focus

#### OUTPUT-02: Wayland paste priority
**Test:** Wayland paste method selection
- **Given:** WAYLAND_DISPLAY set, wtype and ydotool available
- **When:** Transcription successful
- **Then:** Attempt wtype first (CONTEXT.md decision)
- **And:** On wtype failure, try ydotool
- **And:** On both failures, use clipboard-only fallback

#### OUTPUT-03: Wayland auto-detection
**Test:** Wayland vs X11 detection
- **Given:** WAYLAND_DISPLAY empty, DISPLAY set
- **When:** Transcription successful
- **Then:** Use X11 path (xdotool)
- **And:** No attempt at wtype/ydotool

#### OUTPUT-04: ydotool socket check
**Test:** ydotool socket detection
- **Given:** YDOTOOL_SOCKET or /tmp/.ydotool_socket exists
- **When:** Attempting ydotool fallback
- **Then:** Socket check passes, ydotool invoked
- **And:** If no socket, skip to clipboard fallback

#### OUTPUT-05: clipboard preservation
**Test:** Clipboard save/restore
- **Given:** User has text in clipboard ("test")
- **When:** Transcription successful
- **Then:** Clipboard saved via atotto/clipboard
- **And:** After paste, restore to "test"
- **And:** Only if paste succeeded (exit code 0)

#### OUTPUT-06: clipboard on failure
**Test:** Clipboard preservation on failure
- **Given:** Transcription successful, paste fails
- **When:** Attempt paste with wtype fails
- **Then:** Do NOT restore original clipboard
- **And:** Transcription text remains in clipboard

#### OUTPUT-07: NFR-04 (Resource cleanup)
**Test:** Verify resource cleanup
- **Given:** Daemon receives SIGTERM during recording
- **When:** Cleanup runs
- **Then:** PortAudio stream closed
- **And:** PID file removed
- **And:** Hotkey goroutine terminated

### NOTIFY Requirements

#### NOTIFY-01: error notifications
**Test:** Desktop notifications on errors
- **Given:** Various error scenarios
- **When:** Error occurs (API, permission, device)
- **Then:** Desktop notification appears
- **And:** Notification contains specific error message

#### NOTIFY-02: no silent failures
**Test:** No silent failures
- **Given:** Any failure scenario
- **When:** Failure occurs
- **Then:** User receives notification (not silent)
- **And:** Either CLI output or desktop notification

## UAT Scenarios

### Scenario 1: Basic Hold-to-Talk (X11)
1. `yap start`
2. Hold Ctrl+Shift+S
3. Hear start chime
4. Release after 2 seconds
5. Hear stop chime
6. See "transcribed text" appear at cursor
7. Original clipboard preserved

### Scenario 2: Long Recording with Warning
1. `yap start`
2. Hold hotkey
3. At 50s: hear warning chime
4. At 60s: auto-release (stop chime)
5. Text transcribed and pasted

### Scenario 3: Wayland with Fallback
1. `yap start` on GNOME
2. Hold hotkey → stop → transcribe
3. Try wtype → fails
4. Try ydotool → succeeds
5. Text pasted at cursor

### Scenario 4: API Failure Recovery
1. `yap start`
2. Hold hotkey → stop
3. Groq API timeout (503)
4. Retry after 500ms
5. Retry after 1s
6. Retry after 2s
7. Fail with notification: "API error: timeout"

### Scenario 5: Permission Error
1. `yap start`
2: Exit with error and notification
3: Error shows exact: "usermod -aG input $USER"

## Verification Strategy

- **Unit tests:** 6 per plan (TDD-driven, written before implementation)
- **Integration tests:** Verify component interactions
- **System tests:** Real hardware with actual input/output
- **UAT probes:** Test actual user workflows on target systems

All tests must pass for the phase to be complete.