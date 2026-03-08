# Phase 3: IPC + Daemon — Research

**Researched:** 2026-03-07
**Domain:** Go daemon lifecycle, Unix domain socket IPC, PID file management, signal handling
**Confidence:** HIGH

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| DAEMON-01 | `yap start` backgrounds daemon; PID file at `$XDG_DATA_HOME/yap/yap.pid` | `xdg.DataFile` + `exec.Command` + `SysProcAttr.Setsid` pattern |
| DAEMON-02 | `yap stop` sends IPC stop, waits for clean shutdown | IPC client sends `{"cmd":"stop"}`, polls PID removal with timeout |
| DAEMON-03 | `yap status` returns JSON in running/stopped states | IPC client sends `{"cmd":"status"}`; socket-not-found = stopped |
| DAEMON-04 | SIGTERM causes clean shutdown within 2 seconds | `signal.NotifyContext` + deferred `stream.Close` → `pa.Terminate` → socket + PID remove |
| DAEMON-05 | Second `yap start` detects live PID, exits non-zero | Read PID file, `os.FindProcess` + `Signal(0)` liveness check |
| DAEMON-06 | `yap toggle` sends toggle IPC command | IPC client sends `{"cmd":"toggle"}` |
| IPC-01 | Unix socket at `$XDG_DATA_HOME/yap/yap.sock` mode 0600 | `net.Listen("unix", path)` + `os.Chmod(path, 0600)` |
| IPC-02 | Newline-delimited JSON protocol | `json.NewEncoder(conn).Encode(v)` + `json.NewDecoder(conn).Decode(&v)` |
| IPC-03 | CLI commands exit 0 on success, 1 on error | Cobra `RunE` returns error on IPC failure |
| IPC-04 | Socket removed on shutdown; stale socket cleaned at startup | `os.Remove(sockPath)` before `net.Listen`; deferred `os.Remove` on shutdown |
| AUDIO-07 | PortAudio stream + `Pa_Terminate()` always via deferred cleanup; no `os.Exit()` | Existing `Recorder.Close()` + `defer` in daemon main loop |
</phase_requirements>

---

## Summary

Phase 3 builds the daemon backbone: a long-running process that starts in the background, listens for IPC commands on a Unix socket, and shuts down cleanly. All three pieces — daemonization, IPC protocol, and signal-driven shutdown — use stdlib-only Go patterns with no new dependencies.

The daemonization strategy is `exec.Command(os.Args[0], "daemon-run")` with `SysProcAttr{Setsid: true}` — the parent (`yap start`) spawns a detached child that runs the actual daemon loop, then the parent exits. This is the correct approach for Go because Go's runtime cannot safely `fork()`. No double-fork is needed because `yap` is not started from a terminal in normal use; the session detach via `Setsid` is sufficient.

The IPC protocol is newline-delimited JSON over a `SOCK_STREAM` Unix socket — `json.NewEncoder` already appends `\n` after each `Encode` call, so the framing is free. The socket lives at `$XDG_DATA_HOME/yap/yap.sock` (mode 0600) and the PID file at `$XDG_DATA_HOME/yap/yap.pid`. Both are removed on clean shutdown; stale socket is proactively removed before each `net.Listen`.

**Primary recommendation:** Use stdlib `net`, `os/exec`, `os/signal`, `syscall`, `encoding/json`, `bufio` — zero new dependencies for this entire phase.

---

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `net` (stdlib) | Go 1.25 | Unix socket `net.Listen("unix", path)` / `net.Dial("unix", path)` | Zero deps; proven pattern (Docker, gopls) |
| `os/signal` (stdlib) | Go 1.25 | `signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)` | Added Go 1.16; idiomatic context-based shutdown |
| `syscall` (stdlib) | Go 1.25 | `SysProcAttr{Setsid: true}` for process detach; `Signal(0)` for PID liveness | No alternative needed |
| `encoding/json` (stdlib) | Go 1.25 | Newline-delimited JSON encoder/decoder for IPC protocol | `Encode` appends `\n` automatically |
| `os/exec` (stdlib) | Go 1.25 | `exec.Command(os.Args[0], "--daemon-run")` to spawn detached child | Standard Go daemonization pattern |
| `github.com/adrg/xdg` | v0.5.3 | `xdg.DataFile("yap/yap.pid")` and path for socket | Already in go.mod; creates dirs; correct XDG_DATA_HOME |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `bufio` (stdlib) | Go 1.25 | `bufio.NewReader(conn)` alternative to `json.Decoder` for line-oriented reading | Optional — `json.Decoder` handles NDJSON in a loop |
| `os` (stdlib) | Go 1.25 | PID file read/write, `os.Remove`, `os.Chmod`, `os.FindProcess` | All PID and socket file ops |
| `sync` (stdlib) | Go 1.25 | `sync.WaitGroup` for goroutine drain on shutdown | Ensures all handlers finish before cleanup |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `os/exec` spawn pattern | `github.com/sevlyar/go-daemon` | go-daemon is well-built but adds a dep; stdlib pattern is 20 lines |
| `xdg.DataFile` for socket+PID | `xdg.RuntimeFile` | RuntimeDir (`/run/user/1000`) is more correct by spec for sockets; but REQUIREMENTS.md locks `$XDG_DATA_HOME/yap/`; follow the requirement |
| `json.NewDecoder` loop | `bufio.Scanner` + `json.Unmarshal` | Both work; `json.Decoder` is simpler for request/response; Scanner avoids buffering issues on slow sends |

**Installation:** No new packages needed — all stdlib + `adrg/xdg` already in `go.mod`.

---

## Architecture Patterns

### Recommended Project Structure
```
internal/
├── daemon/
│   ├── daemon.go        # Daemon struct, Run(), SIGTERM loop, goroutine supervisor
│   └── daemon_test.go   # Unit tests for daemon lifecycle (no real daemonize)
├── ipc/
│   ├── server.go        # net.Listener, connection handler, command dispatch
│   ├── client.go        # net.Dial, Send(cmd) -> Response, timeout
│   ├── protocol.go      # Request/Response types, cmd constants
│   └── ipc_test.go      # In-process socket tests (no subprocess needed)
├── pidfile/
│   ├── pidfile.go       # Write, Read, IsLive, Remove
│   └── pidfile_test.go
└── cmd/
    ├── start.go         # Updated: spawn detached child OR detect duplicate
    ├── stop.go          # Updated: IPC client send stop
    ├── status.go        # Updated: IPC client send status, print JSON
    └── toggle.go        # Updated: IPC client send toggle
```

### Pattern 1: Daemon Spawning (Parent Side)
**What:** `yap start` spawns a detached copy of itself with a private flag, then exits.
**When to use:** Any time the CLI must background a long-running process in Go without a library.

```go
// internal/cmd/start.go (parent side)
// Source: Go stdlib os/exec docs + syscall.SysProcAttr

func runStart(cfg *config.Config) error {
    // Check for live daemon before spawning.
    pidPath, _ := xdg.DataFile("yap/yap.pid")
    if isLive, _ := pidfile.IsLive(pidPath); isLive {
        return fmt.Errorf("yap is already running (PID file: %s)", pidPath)
    }

    self, err := os.Executable()
    if err != nil {
        return fmt.Errorf("resolve executable: %w", err)
    }

    cmd := exec.Command(self, "--daemon-run")
    // Redirect stdio to /dev/null — daemon must not inherit terminal.
    cmd.Stdin = nil
    cmd.Stdout = nil
    cmd.Stderr = nil
    // Setsid creates a new session: child is detached from parent's
    // controlling terminal and process group.
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

    if err := cmd.Start(); err != nil {
        return fmt.Errorf("start daemon: %w", err)
    }
    // Release parent's reference — child runs independently.
    cmd.Process.Release()

    // Brief wait for PID file to appear (confirm daemon started).
    return waitForPIDFile(pidPath, 2*time.Second)
}
```

### Pattern 2: Daemon Run Loop (Child/Daemon Side)
**What:** The `--daemon-run` child initializes, writes PID, starts IPC server, waits for SIGTERM.
**When to use:** The main event loop inside the detached daemon process.

```go
// internal/daemon/daemon.go
// Source: signal.NotifyContext Go docs (1.16+)

func Run(cfg *config.Config) error {
    // Resolve paths via xdg (already xdg.Reload() called in Load)
    pidPath, err := xdg.DataFile("yap/yap.pid")
    if err != nil {
        return fmt.Errorf("resolve pid path: %w", err)
    }
    sockPath, err := xdg.DataFile("yap/yap.sock")
    if err != nil {
        return fmt.Errorf("resolve socket path: %w", err)
    }

    // Write PID file immediately.
    if err := pidfile.Write(pidPath); err != nil {
        return fmt.Errorf("write pid file: %w", err)
    }
    defer pidfile.Remove(pidPath)

    // Init PortAudio (AUDIO-07: always via defer, never os.Exit)
    rec, err := audio.NewRecorder(cfg.MicDevice)
    if err != nil {
        return fmt.Errorf("init audio: %w", err)
    }
    defer rec.Close() // pa.Terminate() — runs even on signal-driven exit

    // Start IPC server (handles socket setup + os.Remove on shutdown)
    srv, err := ipc.NewServer(sockPath)
    if err != nil {
        return fmt.Errorf("start ipc server: %w", err)
    }
    defer srv.Close() // removes socket file

    // Signal-driven shutdown via context.
    ctx, stop := signal.NotifyContext(context.Background(),
        syscall.SIGTERM, syscall.SIGINT)
    defer stop()

    // Run IPC accept loop in goroutine.
    go srv.Serve(ctx)

    // Block until signal or IPC stop command.
    <-ctx.Done()
    return nil // defers run: rec.Close, srv.Close, pidfile.Remove
}
```

### Pattern 3: Unix Socket Server (IPC Server Side)
**What:** `net.Listener` on a Unix socket path with `os.Chmod(0600)` and stale-socket cleanup.
**When to use:** Any Go program that serves commands from other processes on the same host.

```go
// internal/ipc/server.go
// Source: verified pattern from Go net docs + issue #11822

func NewServer(sockPath string) (*Server, error) {
    // IPC-04: Remove stale socket file (crashed daemon leaves it behind).
    os.Remove(sockPath)

    ln, err := net.Listen("unix", sockPath)
    if err != nil {
        return nil, fmt.Errorf("listen unix %s: %w", sockPath, err)
    }

    // IPC-01: Restrict socket to owner only.
    // net.Listen creates the socket with 0644 by default — must chmod after.
    if err := os.Chmod(sockPath, 0600); err != nil {
        ln.Close()
        return nil, fmt.Errorf("chmod socket: %w", err)
    }

    return &Server{ln: ln, sockPath: sockPath}, nil
}

func (s *Server) Close() error {
    err := s.ln.Close()
    os.Remove(s.sockPath)
    return err
}

func (s *Server) Serve(ctx context.Context) {
    for {
        conn, err := s.ln.Accept()
        if err != nil {
            // Listener closed — normal shutdown path.
            return
        }
        go s.handleConn(ctx, conn)
    }
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
    defer conn.Close()
    dec := json.NewDecoder(conn)
    enc := json.NewEncoder(conn)

    var req Request
    if err := dec.Decode(&req); err != nil {
        return
    }
    resp := s.dispatch(ctx, req)
    enc.Encode(resp) //nolint:errcheck
}
```

### Pattern 4: IPC Client
**What:** CLI side dials the socket, sends one command, reads one response, exits.
**When to use:** `yap stop`, `yap status`, `yap toggle` subcommands.

```go
// internal/ipc/client.go
// Source: Go net.Dial docs

func Send(sockPath string, cmd string) (Response, error) {
    conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
    if err != nil {
        return Response{}, fmt.Errorf("connect to daemon: %w", err)
    }
    defer conn.Close()

    conn.SetDeadline(time.Now().Add(5 * time.Second))

    enc := json.NewEncoder(conn)
    dec := json.NewDecoder(conn)

    if err := enc.Encode(Request{Cmd: cmd}); err != nil {
        return Response{}, fmt.Errorf("send command: %w", err)
    }

    var resp Response
    if err := dec.Decode(&resp); err != nil {
        return Response{}, fmt.Errorf("read response: %w", err)
    }
    return resp, nil
}
```

### Pattern 5: PID File Management
**What:** Write PID, check liveness via `Signal(0)`, detect stale, remove on exit.
**When to use:** Single-instance daemon enforcement (DAEMON-05).

```go
// internal/pidfile/pidfile.go
// Source: Unix signal(0) liveness pattern — standard on Linux

func Write(path string) error {
    return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0600)
}

func Read(path string) (int, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0, err
    }
    pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        return 0, fmt.Errorf("invalid pid file: %w", err)
    }
    return pid, nil
}

// IsLive returns true if the PID file exists and the process is running.
// Uses Signal(0) — the standard Unix liveness check: no error = process exists.
// Note: os.FindProcess on Unix always succeeds; Signal(0) is required.
func IsLive(path string) (bool, error) {
    pid, err := Read(path)
    if os.IsNotExist(err) {
        return false, nil // No PID file — not running.
    }
    if err != nil {
        return false, err
    }
    proc, err := os.FindProcess(pid)
    if err != nil {
        return false, nil // Can't find process.
    }
    // Signal(0) tests process existence without sending a real signal.
    err = proc.Signal(syscall.Signal(0))
    if err == nil {
        return true, nil // Process exists and we can signal it.
    }
    if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
        // Stale PID file — process is gone.
        os.Remove(path)
        return false, nil
    }
    // EPERM means process exists but is owned by another user — still "live".
    if errors.Is(err, syscall.EPERM) {
        return true, nil
    }
    return false, nil
}

func Remove(path string) {
    os.Remove(path)
}
```

### Pattern 6: IPC Protocol Types
**What:** Shared request/response structs for NDJSON encoding.

```go
// internal/ipc/protocol.go

const (
    CmdStop   = "stop"
    CmdStatus = "status"
    CmdToggle = "toggle"
)

type Request struct {
    Cmd string `json:"cmd"`
}

type Response struct {
    OK    bool   `json:"ok"`
    State string `json:"state,omitempty"` // "idle" | "recording" | "stopped"
    Error string `json:"error,omitempty"`
}
```

### Pattern 7: `--daemon-run` Flag in Cobra Root
**What:** Hidden flag that signals to the binary it is running as the daemon child.
**When to use:** Distinguishing the parent (`yap start`) from the child (daemon process).

```go
// internal/cmd/root.go addition

var daemonRun bool

func init() {
    // Hidden flag — used internally by the daemon spawn pattern.
    // "yap --daemon-run" is what the detached child process executes.
    rootCmd.PersistentFlags().BoolVar(&daemonRun, "daemon-run", false, "")
    rootCmd.PersistentFlags().MarkHidden("daemon-run")
}

// In PersistentPreRunE (or in root Execute):
// if daemonRun { return daemon.Run(cfg) }
```

### Anti-Patterns to Avoid
- **Calling `os.Exit()` anywhere in daemon path:** Bypasses `defer` — PortAudio stream stays locked, PID file and socket are never removed. Always `return err` up the call stack.
- **Not calling `os.Chmod` after `net.Listen`:** Go's `net.Listen("unix", ...)` creates the socket with mode 0644. Without explicit `os.Chmod(path, 0600)` the socket is readable by other users on the system.
- **`os.FindProcess` without `Signal(0)`:** On Unix, `os.FindProcess` always succeeds regardless of whether the process exists. PID liveness check MUST use `Signal(0)`.
- **Double-fork daemonization:** Not needed and breaks the Go runtime. `Setsid: true` is sufficient for terminal detachment.
- **Storing socket at `$XDG_RUNTIME_DIR`:** The requirements specify `$XDG_DATA_HOME/yap/`. Follow REQUIREMENTS.md (DAEMON-01, IPC-01).
- **Using `json.Marshal` + `conn.Write` manually:** `json.NewEncoder(conn).Encode(v)` is simpler and already appends `\n` — the framing delimiter is automatic.
- **Blocking `srv.Serve` in the main goroutine without context cancellation:** Must pass context into `Serve`; close the listener from the signal handler to unblock `Accept`.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON framing | Manual `\n`-terminated `Write` | `json.NewEncoder(conn).Encode(v)` | `Encode` already appends `\n`; automatic framing |
| XDG path creation | `os.MkdirAll` + path join | `xdg.DataFile("yap/yap.pid")` | Creates dirs, respects XDG_DATA_HOME, already in go.mod |
| Signal handling | `signal.Notify` + `chan os.Signal` | `signal.NotifyContext` | Returns a context — composes directly with `<-ctx.Done()` |
| Process liveness | `ps aux | grep pid` subprocess | `proc.Signal(syscall.Signal(0))` | Pure Go, no subprocess, standard Unix idiom |

**Key insight:** All IPC and daemon plumbing in this phase is stdlib. Adding any third-party daemon library would introduce complexity without benefit — the patterns are 20-50 lines each.

---

## Common Pitfalls

### Pitfall 1: Socket File Not Removed After Crash (IPC-04)
**What goes wrong:** Daemon is killed with SIGKILL or crashes; socket file remains at `yap.sock`; next `yap start` fails with `bind: address already in use`.
**Why it happens:** `defer os.Remove()` never runs on SIGKILL. The `listener.Close()` in the signal handler also only runs for SIGTERM.
**How to avoid:** Always call `os.Remove(sockPath)` BEFORE `net.Listen`. This is the standard defensive pattern — unconditionally clear a stale socket at startup, not just at shutdown.
**Warning signs:** `listen unix yap.sock: bind: address already in use` on second start after a crash.

### Pitfall 2: PortAudio Stream Not Closed on Daemon Exit (AUDIO-07, Pitfall #6)
**What goes wrong:** Daemon exits via `os.Exit()` or without returning through deferred cleanup. `Pa_Terminate()` never called. Audio device stays locked — other apps can't record.
**Why it happens:** Signal handler calls `os.Exit(1)` directly, or a goroutine panics and takes down the process.
**How to avoid:** Never call `os.Exit()`. Use `signal.NotifyContext` + `<-ctx.Done()` to unblock the main goroutine, then return normally so all `defer`s execute. The existing `Recorder.Close()` (calls `portaudio.Terminate()`) must be deferred at the top of `daemon.Run()`.

### Pitfall 3: Race Between PID File Write and Parent `waitForPIDFile`
**What goes wrong:** Parent polls for PID file existence to confirm daemon started. If daemon is slow to write PID (e.g., audio init takes time), parent reports failure even though daemon starts correctly moments later.
**Why it happens:** Audio `NewRecorder` → `portaudio.Initialize()` + device enum can take ~100-300ms on some systems.
**How to avoid:** Write the PID file BEFORE initializing PortAudio in the daemon. The sequence must be: write PID → init audio → start IPC server. Parent polls with 2-second timeout and ~50ms interval.

### Pitfall 4: `net.Listen` Sets Socket Mode 0644 (IPC-01)
**What goes wrong:** Socket is created with default umask-derived permissions, not 0600. Other local users can connect.
**Why it happens:** Go's `net.Listen("unix", path)` does not accept a mode parameter (Go issue #11822 — filed 2015, not yet resolved as of 2026).
**How to avoid:** Always call `os.Chmod(sockPath, 0600)` immediately after `net.Listen` returns.

### Pitfall 5: `yap stop` Hangs Waiting for Daemon That Crashed
**What goes wrong:** `yap stop` sends IPC command, then waits indefinitely for daemon to exit (polls for PID file removal). Daemon is already dead (crash, SIGKILL), so PID file stays.
**Why it happens:** Stop logic blocks on PID file removal without a timeout.
**How to avoid:** `yap stop` should: (1) send IPC `stop` command, (2) if IPC fails (connection refused) — daemon is already down, report success. If IPC succeeds, poll for PID file removal with 3-second timeout, then SIGTERM fallback.

### Pitfall 6: Second `yap start` Races With PID File Stale Check
**What goes wrong:** Two `yap start` invocations happen within milliseconds of each other. Both read the PID file as absent, both spawn a child. Two daemons run.
**Why it happens:** No file locking on PID file check-then-write.
**How to avoid:** The daemon child should use an `O_EXCL` write for the PID file (exclusive create). If the file already exists, the second daemon exits immediately. Use `os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)` for atomic exclusive creation.

---

## Code Examples

### NDJSON over Unix Socket — Decoder Loop
```go
// Source: encoding/json pkg.go.dev docs
// json.Decoder handles multiple consecutive JSON values from a reader — ideal for NDJSON.
// json.Encoder.Encode automatically appends \n after each call.

dec := json.NewDecoder(conn)
enc := json.NewEncoder(conn)

var req ipc.Request
if err := dec.Decode(&req); err != nil {
    return // client disconnected or malformed JSON
}
resp := dispatch(req)
enc.Encode(resp) // automatically writes {"ok":true,"state":"idle"}\n
```

### Signal-Driven Shutdown — Full Pattern
```go
// Source: os/signal pkg.go.dev docs (signal.NotifyContext added Go 1.16)

ctx, stop := signal.NotifyContext(context.Background(),
    syscall.SIGTERM, syscall.SIGINT)
defer stop() // Unregister signal handler — allow a second Ctrl+C to force-quit

// Start work in goroutines that accept ctx...
go srv.Serve(ctx)

// Block until signal.
<-ctx.Done()
// Defers run here: rec.Close(), srv.Close(), pidfile.Remove()
```

### Daemon Spawn — Detached Child
```go
// Source: os/exec, syscall docs — SysProcAttr.Setsid is the standard Go daemonize pattern

cmd := exec.Command(os.Args[0], "--daemon-run")
cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
cmd.Stdin = nil
cmd.Stdout = nil
cmd.Stderr = nil
if err := cmd.Start(); err != nil {
    return err
}
cmd.Process.Release() // Release — child runs independently of parent
```

### PID File Exclusive Create (Atomicity)
```go
// Source: os.OpenFile docs — O_EXCL prevents TOCTOU race
f, err := os.OpenFile(pidPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
if err != nil {
    if os.IsExist(err) {
        return fmt.Errorf("pid file already exists — is yap already running?")
    }
    return fmt.Errorf("create pid file: %w", err)
}
defer f.Close()
fmt.Fprintf(f, "%d\n", os.Getpid())
```

### waitForPIDFile — Parent Confirmation Poll
```go
// Poll for daemon PID file to confirm successful start.
func waitForPIDFile(path string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        if _, err := os.Stat(path); err == nil {
            return nil // PID file appeared — daemon is running
        }
        time.Sleep(50 * time.Millisecond)
    }
    return fmt.Errorf("daemon did not start within %s", timeout)
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `signal.Notify` + channel loop | `signal.NotifyContext` | Go 1.16 (2021) | Context integrates cleanly with cancellation tree |
| Manual PID lock files | `O_EXCL` atomic create | Always correct | No library needed |
| `os.FindProcess` for liveness | `Signal(0)` after FindProcess | Always (Unix behavior) | FindProcess always returns non-nil on Unix |
| Double-fork daemonization | `Setsid: true` in `SysProcAttr` | Go-idiomatic since beginning | Double-fork is unsafe with Go runtime; Setsid is sufficient |

**Deprecated/outdated:**
- `os.Exit()` from cleanup paths — never use in daemon; bypasses defer
- Manual `\n`-appended JSON writes — use `json.NewEncoder.Encode` which adds it automatically
- Third-party daemon libraries (`go-daemon`, etc.) — unnecessary complexity for this use case

---

## Open Questions

1. **Log destination for daemon stderr**
   - What we know: Parent redirects `cmd.Stderr = nil`. Daemon has no terminal after detach.
   - What's unclear: Where should daemon log output go? A log file under `$XDG_STATE_HOME/yap/yap.log`? Syslog? Silently discard?
   - Recommendation: For v0.1, discard daemon stderr (set to `os.DevNull` or nil). Log to `log.Printf` which goes nowhere after detach. Defer log file to Phase 5 polish.

2. **Daemon shutdown wait in `yap stop`**
   - What we know: IPC stop command triggers `ctx.Cancel()` in daemon; `signal.NotifyContext` unblocks `<-ctx.Done()`; defers run.
   - What's unclear: Should `yap stop` wait for PID file removal to confirm clean shutdown, or just send IPC and exit?
   - Recommendation: Poll for PID file removal for up to 3 seconds after sending IPC stop. Success criterion in REQUIREMENTS says "no zombie" — polling is the minimal correct check.

3. **`yap status` output format**
   - What we know: IPC-02 says response is `{"ok":true,"state":"idle"}`.
   - What's unclear: Does `yap status` print the raw JSON or pretty-print for humans?
   - Recommendation: Print raw JSON (matches DAEMON-03 "reports JSON"). Human formatting is Phase 5 polish scope.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | go test (stdlib) |
| Config file | none |
| Quick run command | `go test ./internal/daemon/... ./internal/ipc/... ./internal/pidfile/...` |
| Full suite command | `go test ./...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DAEMON-01 | PID file written at correct XDG path | unit | `go test ./internal/pidfile/... -run TestWrite` | ❌ Wave 0 |
| DAEMON-02 | Stop command received by daemon causes shutdown | unit | `go test ./internal/ipc/... -run TestStopCommand` | ❌ Wave 0 |
| DAEMON-03 | Status returns correct JSON for running/stopped | unit | `go test ./internal/ipc/... -run TestStatusCommand` | ❌ Wave 0 |
| DAEMON-04 | SIGTERM triggers clean shutdown via context | unit | `go test ./internal/daemon/... -run TestSIGTERMShutdown` | ❌ Wave 0 |
| DAEMON-05 | Second start detects live PID, exits non-zero | unit | `go test ./internal/pidfile/... -run TestIsLive` | ❌ Wave 0 |
| DAEMON-06 | Toggle command dispatched correctly | unit | `go test ./internal/ipc/... -run TestToggleCommand` | ❌ Wave 0 |
| IPC-01 | Socket created at correct path with mode 0600 | unit | `go test ./internal/ipc/... -run TestSocketPermissions` | ❌ Wave 0 |
| IPC-02 | Request/response encoded as NDJSON | unit | `go test ./internal/ipc/... -run TestNDJSON` | ❌ Wave 0 |
| IPC-03 | CLI exits 0 on success, 1 on error | integration | `go test ./internal/cmd/... -run TestStopExitCode` | ❌ Wave 0 |
| IPC-04 | Stale socket removed at daemon startup | unit | `go test ./internal/ipc/... -run TestStaleSocketCleanup` | ❌ Wave 0 |
| AUDIO-07 | PortAudio Terminate called on daemon exit (no os.Exit) | unit | `go test ./internal/daemon/... -run TestAudioCleanupOnExit` | ❌ Wave 0 |

### Key Testing Insight: No Real Daemonization Needed in Tests
Tests for IPC and daemon lifecycle do NOT need to spawn a real detached process. Use the `GO_WANT_HELPER_PROCESS` subprocess pattern or simply run the daemon loop in a goroutine within the test:

```go
// In-process daemon loop test — no subprocess, no daemonize
func TestDaemonLifecycle(t *testing.T) {
    sockPath := filepath.Join(t.TempDir(), "test.sock")
    pidPath := filepath.Join(t.TempDir(), "test.pid")

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    go func() {
        daemon.RunWithPaths(ctx, sockPath, pidPath)
    }()

    // Wait for socket to appear
    require.Eventually(t, func() bool {
        _, err := os.Stat(sockPath)
        return err == nil
    }, 2*time.Second, 50*time.Millisecond)

    // Send IPC command
    resp, err := ipc.Send(sockPath, ipc.CmdStatus)
    require.NoError(t, err)
    require.True(t, resp.OK)

    // Trigger shutdown
    cancel()
    // Verify cleanup
    require.Eventually(t, func() bool {
        _, err := os.Stat(sockPath)
        return os.IsNotExist(err)
    }, 2*time.Second, 50*time.Millisecond)
}
```

This requires `daemon.RunWithPaths` to accept injected paths — design the daemon constructor to take paths as parameters, not resolve them internally. This is the critical testability decision for this phase.

### Sampling Rate
- **Per task commit:** `go test ./internal/daemon/... ./internal/ipc/... ./internal/pidfile/... -count=1`
- **Per wave merge:** `go test ./... -count=1`
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `internal/daemon/daemon_test.go` — covers DAEMON-01, DAEMON-04, AUDIO-07
- [ ] `internal/ipc/ipc_test.go` — covers IPC-01, IPC-02, IPC-03, IPC-04, DAEMON-02, DAEMON-03, DAEMON-06
- [ ] `internal/pidfile/pidfile_test.go` — covers DAEMON-01, DAEMON-05
- [ ] `internal/cmd/start_test.go` — covers DAEMON-05 (duplicate start error)

---

## Sources

### Primary (HIGH confidence)
- `pkg.go.dev/net` — `net.Listen("unix", path)`, `net.Dial("unix", path)`, `net.DialTimeout`
- `pkg.go.dev/os/signal` — `signal.NotifyContext` (Go 1.16+), SIGTERM handling
- `pkg.go.dev/github.com/adrg/xdg` — `xdg.DataFile`, `xdg.DataHome`, `xdg.Reload` — verified via WebFetch
- `pkg.go.dev/encoding/json` — `json.NewEncoder.Encode` appends `\n` (Go issue #7767, confirmed behavior)
- `pkg.go.dev/os/exec` — `exec.Command`, `SysProcAttr{Setsid: true}`, `Process.Release`
- Go issue #11822 — `net.Listen` does not accept mode parameter; `os.Chmod` required after listen

### Secondary (MEDIUM confidence)
- [Understanding Unix Domain Sockets in Golang - DEV Community](https://dev.to/douglasmakey/understanding-unix-domain-sockets-in-golang-32n8) — stale socket + chmod pattern confirmed against net pkg docs
- [signal.NotifyContext: handling cancelation with Unix signals using context](https://henvic.dev/posts/signal-notify-context/) — confirmed against stdlib docs
- [Graceful Shutdown in Go: Practical Patterns - VictoriaMetrics](https://victoriametrics.com/blog/go-graceful-shutdown/) — shutdown ordering confirmed against Go docs
- [Gopls daemon pattern](https://go.dev/gopls/daemon) — confirmed test-subprocess pattern for daemon integration testing

### Tertiary (LOW confidence — for awareness only)
- `github.com/sevlyar/go-daemon` — surveyed API; NOT used (stdlib is sufficient)

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all stdlib; `adrg/xdg` already in go.mod and verified via pkg.go.dev
- Architecture: HIGH — patterns from Docker, gopls, Go stdlib itself; no speculative choices
- Pitfalls: HIGH — all grounded in filed Go issues or observable behaviors (issue #11822, os.FindProcess Unix behavior)
- Testing strategy: HIGH — in-process goroutine pattern is established by Go's own test suite

**Research date:** 2026-03-07
**Valid until:** 2026-09-07 (stable stdlib APIs; 6-month estimate)
