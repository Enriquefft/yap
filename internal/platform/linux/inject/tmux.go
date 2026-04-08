package inject

import (
	"context"
	"fmt"
	"strings"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// tmuxStrategy delivers text via tmux's load-buffer + paste-buffer
// commands. This is the safest path inside a tmux session because
// the buffer mechanism is atomic — multi-line commands paste as a
// single block instead of executing line-by-line.
//
// The strategy is gated on AppTerminal + Tmux additive bit.
//
// Paste framing: the strategy runs `tmux paste-buffer -p`, which
// tells tmux to wrap the paste in the pane's bracketed-paste markers
// when the destination pane has bracketed paste enabled. This is
// the idiomatic tmux mechanism — it respects the pane's state and
// does not double-wrap. yap never inserts bracketed-paste bytes into
// the buffer payload itself; doing so would cause double-wrapping
// (tmux would wrap the already-wrapped payload again) and corrupt
// the shell input.
type tmuxStrategy struct {
	deps Deps
}

// newTmuxStrategy constructs a tmux strategy bound to deps.
func newTmuxStrategy(deps Deps) *tmuxStrategy {
	return &tmuxStrategy{deps: deps}
}

// Name returns the strategy identifier used in audit logs and
// app_overrides lookups.
func (s *tmuxStrategy) Name() string { return "tmux" }

// Supports returns true when the focused application is a terminal
// running inside a tmux session.
func (s *tmuxStrategy) Supports(target yinject.Target) bool {
	return target.AppType == yinject.AppTerminal && target.Tmux
}

// Deliver pipes text into `tmux load-buffer -` then runs `tmux
// paste-buffer -p`. The `-p` flag tells tmux to wrap the paste in
// bracketed-paste markers when the destination pane has them
// enabled — yap does not inject the markers itself. Both commands
// fail with a non-nil error when tmux is not running or when the
// current pane is invalid.
func (s *tmuxStrategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	load := s.deps.ExecCommandContext(ctx, "tmux", "load-buffer", "-")
	load.Stdin = strings.NewReader(text)
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux: load-buffer: %w", err)
	}

	paste := s.deps.ExecCommandContext(ctx, "tmux", "paste-buffer", "-p")
	if err := paste.Run(); err != nil {
		return fmt.Errorf("tmux: paste-buffer: %w", err)
	}
	return nil
}
