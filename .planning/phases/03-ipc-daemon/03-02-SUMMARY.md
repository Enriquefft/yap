---
phase: 03-ipc-daemon
plan: 02
subsystem: IPC + CLI Integration
tags: [daemon, ipc, unix-socket, cli]
dependency_graph:
  requires: [03-01]
  provides: [03-03]
  affects: []
tech_stack:
  added: [encoding/json, net.Listener]
  patterns: [NDJSON protocol, detached process spawning, Unix domain socket]
key_files:
  created:
    - internal/ipc/protocol.go
    - internal/ipc/server.go
    - internal/ipc/server_test.go
    - internal/ipc/client.go
    - internal/ipc/client_test.go
  modified:
    - internal/cmd/start.go
    - internal/cmd/stop.go
    - internal/cmd/status.go
    - internal/cmd/toggle.go
decisions:
  - "NDJSON protocol via json.Encoder.Encode (appends \\n automatically)"
  - "Server accepts concurrent connections in goroutines"
  - "Stale socket removed unconditionally at startup (IPC-04)"
  - "CLI timeouts: 5s for stop/toggle, 1s for status"
  - "stop/status are idempotent (exit 0 if daemon not running)"
metrics:
  duration: "25min"
  completed_at: "2026-03-08T04:50:00Z"
  tasks_completed: 4
  files_modified: 9
---

# Phase 3 Plan 2: IPC Server + CLI Integration Summary

**Completed:** Unix socket IPC server with newline-delimited JSON protocol, client communication layer, and CLI command updates to communicate with daemon.

## What Was Built

### 1. IPC Protocol (`internal/ipc/protocol.go`)
- `Request` struct with Cmd field (stop, status, toggle)
- `Response` struct with Ok, State, Error fields
- Constants: CmdStop, CmdStatus, CmdToggle

### 2. IPC Server (`internal/ipc/server.go`)
- `NewServer(sockPath)` creates Unix domain socket with mode 0600 (IPC-01)
- Auto-removes stale socket before listening (IPC-04)
- `Serve(ctx)` accepts connections in loop, handles each in goroutine
- `dispatch()` routes commands to handlers (stubs for Phase 4)
- NDJSON encoding/decoding via json.Encoder/Decoder (IPC-02)

### 3. IPC Client (`internal/ipc/client.go`)
- `Send(sockPath, cmd, timeout)` — single operation: connect → send → receive → close
- Timeout covers dial + read/write (5s for stop/toggle, 1s for status)
- Graceful error on connection refused (daemon not running)

### 4. CLI Commands Updated
- **start.go:** Spawns detached daemon via exec.Command + SysProcAttr.Setsid, detects duplicate live daemon (DAEMON-05), waits for PID file confirmation
- **stop.go:** Sends IPC stop command, polls for PID removal, idempotent if daemon not running (DAEMON-02, IPC-04)
- **status.go:** Sends IPC status command, prints JSON response, exits 1 if daemon not running (DAEMON-03)
- **toggle.go:** Sends IPC toggle command (DAEMON-06)

## Requirements Coverage

All 7 requirements met:
- DAEMON-02 ✓ (stop sends IPC command)
- DAEMON-03 ✓ (status returns JSON state)
- DAEMON-06 ✓ (toggle sends command)
- IPC-01 ✓ (socket at $XDG_DATA_HOME/yap/yap.sock, mode 0600)
- IPC-02 ✓ (NDJSON protocol via json.Encoder.Encode)
- IPC-03 ✓ (CLI exits 0 on success, 1 on error)
- IPC-04 ✓ (stale socket removed at startup, stale PID ignored if not live)

## Test Coverage

All tests passing:
- `TestNewServerCreatesSocket` — socket creation with 0600 permissions
- `TestNewServerRemovesStaleSocket` — stale socket cleanup
- `TestHandleConnNDJSON` — concurrent connections, NDJSON encoding
- `TestDispatchUnknownCommand` — error handling
- `TestSendStatusCommand` — client receives response from server
- `TestSendToNonExistentSocket` — graceful error on missing daemon
- `TestSendTimeout` — timeout enforcement
- `TestSendStopCommand` — stop command transmission

## Integration Behavior Verified

1. **Build:** `go build ./cmd/yap` completes without errors
2. **Start daemon:** `./yap start` spawns detached child, parent exits, child continues running
3. **Status:** `./yap status` connects to socket, returns `{"ok":true,"state":"idle"}`
4. **Stop daemon:** `./yap stop` sends IPC stop, waits for PID removal, confirms shutdown
5. **Duplicate start:** Second `./yap start` detects live daemon PID file, exits with error
6. **Clean shutdown:** `pkill -TERM yap` triggers SIGTERM handler, daemon shuts down within 2s, no zombies
7. **Idempotency:** `./yap stop` on non-running daemon exits 0 gracefully

## Deviations from Plan

None — plan executed exactly as written. All TDD tasks passed red→green→refactor flow.

## Next Steps

Phase 3-03: Input handling (evdev hotkey listener) and output (desktop notifications). Phase 4 will implement state machine dispatch logic in server.dispatch() to respond to toggle commands.

## Commits

- `8a0601a` feat(03-02): update CLI commands to use IPC (start, stop, status, toggle)
- `4196397` feat(03-02): implement IPC client with timeout handling
- `2a68c8a` test(03-02): add server tests for IPC protocol + socket handling
