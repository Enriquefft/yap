package inject

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// osc52Strategy delivers text to a terminal emulator via the OSC 52
// escape sequence. The sequence is written to the slave pseudo-tty
// owned by a descendant shell of the focused terminal emulator
// process — the terminal sees it on its master side and copies the
// payload to the system clipboard.
//
// This strategy works without anything installed on the remote when
// dictating into an SSH session in a modern terminal (foot, kitty,
// wezterm, ghostty, etc.) because the escape sequence travels over
// the SSH tty as application input.
//
// The OSC52 payload is always the raw text bytes, base64-encoded.
// Bracketed-paste markers are framing control sequences — terminals
// wrap them around a paste on the wire, they are not data. Embedding
// them inside the clipboard payload would silently corrupt every
// multi-line dictation on paste: the markers would end up in the
// clipboard bytes, the terminal would add its own pair on top, and
// the shell would interpret the inner markers as paste-end-start
// delimiters. Callers that need paste framing get it from the
// terminal, not from yap.
//
// The tty is opened with O_NONBLOCK so a wedged terminal (e.g. the
// kernel tty write buffer full) surfaces EAGAIN rather than blocking
// the caller indefinitely. EAGAIN is treated as
// ErrStrategyUnsupported so the orchestrator falls through to the
// next strategy.
type osc52Strategy struct {
	deps Deps
	opts platform.InjectionOptions
	// chosenTTY captures the /dev/pts/<N> path resolved on the most
	// recent successful Deliver call. The injector type-asserts the
	// strategy against ttyReporter to include the value in the audit
	// log. It is reset at the top of every Deliver call, and because
	// Injector serialises Inject/InjectStream via its mutex the field
	// is never read concurrently.
	chosenTTY string
}

// maxResolveTTYNodes caps the breadth-first walk of /proc descendants
// during OSC52 tty resolution. On a machine with many shells under a
// single terminal server (kitty, gnome-terminal, foot, wezterm) or a
// pathological fork tree, the walk could otherwise continue until
// OSReadDir fails. Exceeding the cap surfaces as
// ErrStrategyUnsupported so the selector falls through to the next
// strategy cleanly.
const maxResolveTTYNodes = 256

// newOSC52Strategy constructs an OSC52 strategy bound to deps and
// opts.
func newOSC52Strategy(deps Deps, opts platform.InjectionOptions) *osc52Strategy {
	return &osc52Strategy{deps: deps, opts: opts}
}

// Name returns the strategy identifier used in audit logs and
// app_overrides lookups.
func (s *osc52Strategy) Name() string { return "osc52" }

// Supports returns true for terminal targets when PreferOSC52 is on
// in the InjectionOptions.
func (s *osc52Strategy) Supports(target yinject.Target) bool {
	return target.AppType == yinject.AppTerminal && s.opts.PreferOSC52
}

// LastChosenTTY returns the /dev/pts/<N> path the most recent
// successful Deliver call wrote to, or the empty string when no
// successful delivery has happened yet or the last call failed before
// the tty was resolved. The injector uses this to enrich the audit
// log via a ttyReporter type assertion.
func (s *osc52Strategy) LastChosenTTY() string { return s.chosenTTY }

// Deliver writes the OSC52 escape sequence to the slave pty of a
// descendant shell. Returns ErrStrategyUnsupported when the target
// PID cannot be parsed, /proc cannot be walked, the BFS hits the
// descendant cap, no descendant shell is currently bound to a
// /dev/pts/N, or the non-blocking write returns EAGAIN — the
// orchestrator falls through to the next strategy in those cases.
func (s *osc52Strategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	// Reset before every attempt so a failed call cannot leak a stale
	// tty into the audit log of a later successful call by a different
	// strategy.
	s.chosenTTY = ""

	if target.WindowID == "" {
		return yinject.ErrStrategyUnsupported
	}
	pid, err := strconv.Atoi(target.WindowID)
	if err != nil || pid <= 0 {
		return yinject.ErrStrategyUnsupported
	}

	tty, err := s.resolveTTY(pid)
	if err != nil {
		return yinject.ErrStrategyUnsupported
	}

	// Raw text only — bracketed-paste markers are framing, not data.
	// See the type doc block above.
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := "\x1b]52;c;" + encoded + "\x07"

	w, err := s.deps.OSOpenFile(tty, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("osc52: open %s: %w", tty, err)
	}
	defer func() { _ = w.Close() }()
	if _, err := w.Write([]byte(seq)); err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return yinject.ErrStrategyUnsupported
		}
		return fmt.Errorf("osc52: write %s: %w", tty, err)
	}
	s.chosenTTY = tty
	return nil
}

// resolveTTY walks the descendant tree of pid (the focused terminal
// emulator) and returns the /dev/pts/N path of the first descendant
// whose stdin (fd/0) is a pseudo-terminal slave. The breadth-first
// walk handles tmux and other shell wrappers transparently — we just
// keep descending until we land on a process with a pts.
//
// Returns an error when /proc is unreadable (sandbox, container
// without procfs), when no descendant pts is found, or when the walk
// exceeds maxResolveTTYNodes descendants (pathological fork tree).
func (s *osc52Strategy) resolveTTY(rootPID int) (string, error) {
	queue := []int{rootPID}
	visited := map[int]bool{rootPID: true}

	for len(queue) > 0 {
		if len(visited) > maxResolveTTYNodes {
			return "", yinject.ErrStrategyUnsupported
		}
		pid := queue[0]
		queue = queue[1:]

		// First, try this pid's stdin. The terminal emulator itself
		// rarely has a pts on fd/0, but later descendants will.
		if tty, ok := s.ptsFromFD(pid); ok {
			return tty, nil
		}
		// Also check fd/1 and fd/2 — interactive shells have all
		// three pointing at the controlling tty.
		if tty, ok := s.ptsFromFDIndex(pid, 1); ok {
			return tty, nil
		}
		if tty, ok := s.ptsFromFDIndex(pid, 2); ok {
			return tty, nil
		}

		children, err := s.childrenOf(pid)
		if err != nil {
			// /proc traversal failure is fatal for OSC52 — bubble up
			// so the orchestrator falls through.
			return "", fmt.Errorf("osc52: read children of %d: %w", pid, err)
		}
		for _, child := range children {
			if visited[child] {
				continue
			}
			visited[child] = true
			queue = append(queue, child)
		}
	}
	return "", fmt.Errorf("osc52: no descendant pts found under pid %d", rootPID)
}

// ptsFromFD reads /proc/<pid>/fd/0 and returns the resolved /dev/pts/N
// path when the link points at one. The boolean indicates a successful
// match.
func (s *osc52Strategy) ptsFromFD(pid int) (string, bool) {
	return s.ptsFromFDIndex(pid, 0)
}

// ptsFromFDIndex generalises ptsFromFD over fd indices 0/1/2.
func (s *osc52Strategy) ptsFromFDIndex(pid, idx int) (string, bool) {
	link := "/proc/" + strconv.Itoa(pid) + "/fd/" + strconv.Itoa(idx)
	target, err := s.deps.OSReadlink(link)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(target, "/dev/pts/") {
		return target, true
	}
	return "", false
}

// childrenOf returns the immediate children of pid. The Linux kernel
// exposes this via /proc/<pid>/task/<tid>/children — a single
// task/<tid> directory holds the canonical list for the main thread,
// but extra threads can append; we union them all to be safe.
func (s *osc52Strategy) childrenOf(pid int) ([]int, error) {
	taskDir := "/proc/" + strconv.Itoa(pid) + "/task"
	entries, err := s.deps.OSReadDir(taskDir)
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		childrenFile := taskDir + "/" + e.Name() + "/children"
		data, err := s.deps.OSReadFile(childrenFile)
		if err != nil {
			// Some tasks may have already exited; treat as empty.
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			n, err := strconv.Atoi(field)
			if err != nil || n <= 0 {
				continue
			}
			if seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}
