package inject

import (
	"context"
	"fmt"
	"strings"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// waylandStrategy types text into the focused application via wtype on
// Wayland. wtype is a libei / virtual-keyboard client that delivers
// real keystrokes; the focused application sees them the same as if
// the user typed them.
//
// When wtype is unavailable, the strategy falls back to ydotool —
// the kernel uinput-based input simulator that requires the daemon
// socket. The strategy returns ErrStrategyUnsupported only when
// neither tool is installed and the user is on Wayland; the
// orchestrator then walks to the X11 strategy (which fails on
// Wayland because xdotool needs DISPLAY) and finally surfaces the
// fall-through failure to the audit log.
type waylandStrategy struct {
	deps Deps
}

// newWaylandStrategy constructs a Wayland strategy bound to deps.
func newWaylandStrategy(deps Deps) *waylandStrategy {
	return &waylandStrategy{deps: deps}
}

// Name returns the strategy identifier used in audit logs and
// app_overrides lookups.
func (s *waylandStrategy) Name() string { return "wayland" }

// Supports returns true on Wayland targets. The strategy is
// general-purpose and matches every app class; the orchestrator
// orders it after the per-app strategies so it only fires when
// nothing more specific applies.
func (s *waylandStrategy) Supports(target yinject.Target) bool {
	return target.DisplayServer == "wayland"
}

// Deliver pipes text into wtype's stdin via `wtype -`. When wtype is
// missing, falls back to `ydotool type --file -` after checking the
// ydotool socket. Returns ErrStrategyUnsupported when neither tool is
// available.
func (s *waylandStrategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	if _, err := s.deps.LookPath("wtype"); err == nil {
		cmd := s.deps.ExecCommand("wtype", "-")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("wayland: wtype: %w", err)
		}
		return nil
	}
	if s.canUseYdotool() {
		cmd := s.deps.ExecCommand("ydotool", "type", "--file", "-")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("wayland: ydotool: %w", err)
		}
		return nil
	}
	return yinject.ErrStrategyUnsupported
}

// canUseYdotool returns true when both the ydotool binary is in PATH
// and the ydotool daemon socket exists. The socket path is configurable
// via the YDOTOOL_SOCKET environment variable; default is
// /tmp/.ydotool_socket.
func (s *waylandStrategy) canUseYdotool() bool {
	if _, err := s.deps.LookPath("ydotool"); err != nil {
		return false
	}
	socket := s.deps.EnvGet("YDOTOOL_SOCKET")
	if socket == "" {
		socket = "/tmp/.ydotool_socket"
	}
	if _, err := s.deps.OSStat(socket); err != nil {
		return false
	}
	return true
}
