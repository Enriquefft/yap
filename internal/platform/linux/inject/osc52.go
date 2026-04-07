package inject

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

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
type osc52Strategy struct {
	deps Deps
	opts platform.InjectionOptions
}

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

// Deliver writes the OSC52 escape sequence to the slave pty of a
// descendant shell. Returns ErrStrategyUnsupported when the target
// PID cannot be parsed, /proc cannot be walked, or no descendant
// shell is currently bound to a /dev/pts/N — the orchestrator falls
// through to the next strategy in those cases.
func (s *osc52Strategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
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

	payload := text
	if s.opts.BracketedPaste && strings.Contains(text, "\n") {
		payload = wrapBracketed(text)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))
	seq := "\x1b]52;c;" + encoded + "\x07"

	w, err := s.deps.OSOpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("osc52: open %s: %w", tty, err)
	}
	defer func() { _ = w.Close() }()
	if _, err := w.Write([]byte(seq)); err != nil {
		return fmt.Errorf("osc52: write %s: %w", tty, err)
	}
	return nil
}

// resolveTTY walks the descendant tree of pid (the focused terminal
// emulator) and returns the /dev/pts/N path of the first descendant
// whose stdin (fd/0) is a pseudo-terminal slave. The breadth-first
// walk handles tmux and other shell wrappers transparently — we just
// keep descending until we land on a process with a pts.
//
// Returns an error when /proc is unreadable (sandbox, container
// without procfs) or when no descendant pts is found.
func (s *osc52Strategy) resolveTTY(rootPID int) (string, error) {
	queue := []int{rootPID}
	visited := map[int]bool{rootPID: true}

	for len(queue) > 0 {
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
