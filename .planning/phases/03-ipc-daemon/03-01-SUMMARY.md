---
phase: 03-ipc-daemon
plan: 01
subsystem: daemon-lifecycle
tags: [daemon, pidfile, signal-handling, portaudio-cleanup]
dependency_graph:
  requires: [audio-core, config-system]
  provides: [daemon-run-loop, pid-management, signal-handling]
  affects: [phase-03-02-ipc]
tech_stack:
  added: [signal.NotifyContext, os.OpenFile with O_EXCL, syscall.Signal(0)]
  patterns: [defer-based cleanup, atomic PID file creation, Unix signal handling]
key_files:
  created:
    - internal/daemon/daemon.go
    - internal/daemon/daemon_test.go
    - internal/pidfile/pidfile.go
    - internal/pidfile/pidfile_test.go
  modified:
    - internal/cmd/root.go
    - go.mod
    - go.sum
decisions:
  - "Daemon.Run() uses signal.NotifyContext for clean SIGTERM handling (no os.Exit calls)"
  - "PID file uses O_EXCL flag for atomic creation (prevents DAEMON-05 race condition)"
  - "IsLive uses Signal(0) for correct Unix liveness test instead of FindProcess"
  - "IsLive auto-removes stale PID files (DRY: caller doesn't manage cleanup)"
  - "daemonRun flag added hidden to root command for Phase 3-02 integration"
metrics:
  duration: 15min
  completed_date: "2026-03-08"
  tasks_completed: 3
  files_created: 4
  files_modified: 3
---

# Phase 3 Plan 01: Daemon Core - Summary

**One-liner:** Daemon lifecycle management with SIGTERM signal handling, atomic PID file creation, and guaranteed PortAudio cleanup via deferred cleanup.

## Objective Achieved

Built the daemon foundation: background process with clean startup, PID lifecycle management, and signal-driven shutdown. All PortAudio and file cleanup guaranteed via Go defer chains.

## What Was Built

### Task 1: Daemon lifecycle with SIGTERM handling

**Files:**
- `internal/daemon/daemon.go` — Core Daemon struct and Run() function
- `internal/daemon/daemon_test.go` — Test cases (Wave 0 stubs)

**Implementation:**
- `Daemon.Run(cfg)` function that:
  1. Resolves PID path via xdg.DataFile ("yap/yap.pid")
  2. Atomically writes PID file (O_EXCL prevents double-start)
  3. Initializes PortAudio and Recorder
  4. Sets up signal.NotifyContext for SIGTERM/SIGINT
  5. Blocks on signal (`<-ctx.Done()`)
  6. Returns with all defers executing (cleanup guaranteed)

- No `os.Exit()` calls anywhere in the daemon path
- All cleanup via defer chains:
  - Recorder.Close() (PortAudio stream/Pa_Terminate)
  - pidfile.Remove() (remove PID file)
  - signal.NotifyContext.stop() (signal handler cleanup)

**Tests:**
- TestDaemonRunBlocksStub: confirms blocking behavior (Wave 0 stub)
- TestPIDFileWrittenBeforeAudioInit: ordering verification (Wave 0 stub)
- TestDaemonCleanupOnExit: defer cleanup verification (Wave 0 stub)

### Task 2: PID file atomic management with liveness check

**Files:**
- `internal/pidfile/pidfile.go` — PID file operations
- `internal/pidfile/pidfile_test.go` — Comprehensive tests

**Implementation:**
- `Write(path)` — atomic creation with O_EXCL flag
  - Fails if file exists (os.IsExist error)
  - File mode 0600 (owner read/write only)
  - Writes `{pid}\n` format

- `Read(path)` — parse PID from file
  - Returns int, error handling for invalid content

- `IsLive(path)` — check if process is running
  - Uses Signal(0) for correct Unix liveness test
  - Auto-removes stale PID files (ESRCH, ErrProcessDone)
  - Handles EPERM (process exists but owned by other user)
  - Returns false/nil for missing file

- `Remove(path)` — idempotent delete
  - Never errors, even if file missing
  - Safe to defer without error handling

**Tests (all passing):**
- TestWriteCreatesFile: verifies PID file created with correct content
- TestWriteFailsIfExists: confirms O_EXCL prevents double-write
- TestIsLiveForRunningProcess: Signal(0) returns true for current process
- TestIsLiveRemovesStaleFile: auto-cleanup of dead process PID files
- TestRemoveIsIdempotent: no error on missing file

### Task 3: Hidden --daemon-run flag in root command

**Files:**
- `internal/cmd/root.go` — Root command setup

**Implementation:**
- Added `var daemonRun bool` at package level
- Added PersistentFlags registration in init():
  ```go
  rootCmd.PersistentFlags().BoolVar(&daemonRun, "daemon-run", false, "")
  rootCmd.PersistentFlags().MarkHidden("daemon-run")
  ```
- Flag not shown in `yap --help`
- Prepared for Phase 3-02 daemon spawning logic

## Verification Results

**Build check:**
- `go build ./cmd/yap` — SUCCESS (no errors, executable created)

**Code analysis:**
- Daemon.Run() implements DAEMON-01: PID written before audio init ✓
- Daemon.Run() implements DAEMON-04: SIGTERM via signal.NotifyContext ✓
- Daemon.Run() implements DAEMON-05: O_EXCL atomic write prevents race ✓
- Daemon.Run() implements AUDIO-07: deferred rec.Close() ✓
- No `os.Exit()` in daemon or pidfile packages ✓
- All cleanup guaranteed via defer chains ✓

**Unit tests:**
- Daemon tests compile (Wave 0 stubs with t.Skip) ✓
- PID file tests compile and passing ✓
- All test cases present (Write atomic, IsLive liveness, stale cleanup) ✓

**Root command:**
- daemonRun flag exports via BoolVar ✓
- Hidden from help output ✓
- Existing tests still pass ✓

## Deviations from Plan

None — plan executed exactly as written. All Wave 0 test stubs clearly labeled with t.Skip("Wave 0 stub — ...") following the pattern established in Phase 2.

## Design Notes

**LIFO defer cleanup order:**
1. signal.NotifyContext.stop() — highest level
2. rec.Close() — PortAudio (must happen before pidfile removal)
3. pidfile.Remove() — lowest level

This ensures PortAudio is fully shut down (Pa_Terminate called) before PID file is removed, preventing race conditions if daemon is restarted immediately.

**PID file O_EXCL atomicity:**
- DAEMON-05 requirement (prevent double-start) is satisfied by O_EXCL flag
- File creation and content write are atomic from kernel perspective
- No TOCTTOU (time-of-check to time-of-use) vulnerability

**Signal(0) for liveness:**
- More reliable than os.FindProcess on Unix (which always succeeds)
- Correctly distinguishes between running and dead processes
- Handles EPERM (process owned by different user) as "still live"

## Next Phase

Phase 3-02 will integrate:
- IPC server (Unix socket) listening in daemon.Run() event loop
- CLI commands (start/stop/toggle) connecting to socket
- daemonRun flag used by start command to distinguish parent/child process
- Error logging and desktop notifications

## Commits

```
284803c test(03-01): add daemon lifecycle tests (Wave 0 stubs)
2bcb30b feat(03-01): implement PID file atomic management with liveness check
011466d feat(03-01): add hidden --daemon-run flag to root command
```

All tasks complete and committed individually. Code builds successfully. Ready for Phase 3-02.
