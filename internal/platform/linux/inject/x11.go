package inject

import (
	"fmt"
	"strings"
	"time"

	"context"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// x11Strategy types text into the focused application via xdotool on
// X11. The strategy waits for the reported active window to stabilize
// before issuing the type command — two consecutive polls of
// `xdotool getactivewindow` returning the same window id is treated
// as "focus has settled". The polling caps at focusPollMaxAttempts
// iterations of focusPollInterval each, so the worst case is bounded
// at focusPollMaxAttempts * focusPollInterval (currently 100ms).
//
// There are no fixed sleeps in this strategy: every wait routes
// through Deps.Sleep so tests substitute a no-op.
type x11Strategy struct {
	deps Deps
}

const (
	// focusPollInterval is the time the strategy waits between
	// xdotool getactivewindow probes.
	focusPollInterval = 10 * time.Millisecond
	// focusPollMaxAttempts is the upper bound on the polling loop;
	// the strategy proceeds even if focus never reports stable to
	// avoid hanging on a flaky compositor.
	focusPollMaxAttempts = 10
)

// newX11Strategy constructs an X11 strategy bound to deps.
func newX11Strategy(deps Deps) *x11Strategy {
	return &x11Strategy{deps: deps}
}

// Name returns the strategy identifier used in audit logs and
// app_overrides lookups.
func (s *x11Strategy) Name() string { return "x11" }

// Supports returns true on X11 targets. The strategy is the
// generic-GUI fallback for X11 sessions and matches every app class.
func (s *x11Strategy) Supports(target yinject.Target) bool {
	return target.DisplayServer == "x11"
}

// Deliver waits for focus to stabilize then runs `xdotool type
// --clearmodifiers -- <text>`. The strategy uses `--` to ensure text
// beginning with a hyphen is not parsed as an xdotool flag.
func (s *x11Strategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	s.waitForStableFocus()
	cmd := s.deps.ExecCommand("xdotool", "type", "--clearmodifiers", "--", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("x11: xdotool type: %w", err)
	}
	return nil
}

// waitForStableFocus polls `xdotool getactivewindow` until two
// consecutive samples return the same window id, or until the polling
// budget is exhausted. The function never blocks beyond
// focusPollMaxAttempts * focusPollInterval.
func (s *x11Strategy) waitForStableFocus() {
	prev := s.activeWindow()
	if prev == "" {
		return
	}
	for i := 0; i < focusPollMaxAttempts; i++ {
		s.deps.Sleep(focusPollInterval)
		cur := s.activeWindow()
		if cur == prev {
			return
		}
		prev = cur
	}
}

// activeWindow runs `xdotool getactivewindow` and returns the trimmed
// output. Errors return the empty string — callers treat that as
// "no stable focus" and proceed.
func (s *x11Strategy) activeWindow() string {
	cmd := s.deps.ExecCommand("xdotool", "getactivewindow")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
