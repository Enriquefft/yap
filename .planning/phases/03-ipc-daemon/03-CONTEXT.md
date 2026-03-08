# Phase 3: IPC + Daemon - Context

**Gathered:** 2026-03-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Daemon process lifecycle: starting in background, responding to IPC commands over a Unix socket, and shutting down cleanly with proper PortAudio stream cleanup. Handles signal-based shutdown and recovery from crashed daemon state.

</domain>

<decisions>
## Implementation Decisions

### Daemon Logging Strategy
- Daemon runs silent by default (no terminal or file output)
- Errors trigger both desktop notification (via beeep) AND error log file write
- Error log at `$XDG_DATA_HOME/yap/error.log` includes error message + context (e.g., "error during hotkey listener init")
- Logs rotated automatically; keep only last 7 days of error history
- No stack traces in error log (standard context level)

### IPC Client Timeout & Retry
- `yap stop` and `yap toggle`: 5-second timeout with up to 3 retries (exponential backoff: 500ms, 1s, 2s)
- `yap status`: faster check (1-second timeout, no retries) — fail fast if daemon seems unresponsive
- If socket doesn't exist (daemon never started): warn message ("Daemon not running; nothing to stop") but exit 0 for idempotency
- Applies to all IPC commands consistently

### Shutdown Behavior & Feedback
- SIGTERM-triggered shutdown: respond to incoming IPC requests with error message "Daemon shutting down" before disconnecting
- Graceful shutdown with 2-second timeout: attempt full cleanup (close PortAudio stream, remove socket, remove PID file), then force exit if timeout occurs
- If actively recording when SIGTERM arrives: send desktop notification "Recording in progress, daemon exiting in 1s" and wait 1s for user to release hotkey
- Successful shutdown shows desktop notification "Daemon stopped" for user confirmation

### Force Shutdown Fallback
- If graceful SIGTERM shutdown times out after 2s: automatically attempt `kill -9` (force kill) without prompting
- On daemon startup: check for stale socket file; if found, remove it automatically (don't wait for failed connection attempt)
- On daemon startup: validate PID file by checking if process is alive (using `kill(pid, 0)`); if process is dead, remove stale PID file and proceed
- After force-killing a hung daemon: `yap stop` prints warning "Daemon was hung; force-killed" but exits 0 (idempotent behavior for scripts)

</decisions>

<specifics>
## Specific Ideas

- Daemon should be as invisible as possible — silent unless errors occur
- Error messages (via notification + log) should be actionable (include what was being attempted)
- IPC timeouts should be short enough for good UX (don't make users wait 10+ seconds) but long enough to handle system delays

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 03-ipc-daemon*
*Context gathered: 2026-03-08*
