package inject

import (
	"context"
	"fmt"
	"strings"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// tmuxStrategy delivers text via tmux's load-buffer + paste-buffer
// commands. This is the safest path inside a tmux session because
// the buffer mechanism is atomic — multi-line commands paste as a
// single block instead of executing line-by-line.
//
// The strategy is gated on AppTerminal + Tmux additive bit.
type tmuxStrategy struct {
	deps Deps
	opts platform.InjectionOptions
}

// newTmuxStrategy constructs a tmux strategy bound to deps and opts.
// The opts argument is used for BracketedPaste configuration.
func newTmuxStrategy(deps Deps, opts platform.InjectionOptions) *tmuxStrategy {
	return &tmuxStrategy{deps: deps, opts: opts}
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
// paste-buffer`. Both commands fail with a non-nil error when tmux is
// not running or when the current pane is invalid.
func (s *tmuxStrategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	payload := text
	if s.opts.BracketedPaste && strings.Contains(text, "\n") {
		payload = wrapBracketed(text)
	}

	load := s.deps.ExecCommand("tmux", "load-buffer", "-")
	load.Stdin = strings.NewReader(payload)
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux: load-buffer: %w", err)
	}

	paste := s.deps.ExecCommand("tmux", "paste-buffer")
	if err := paste.Run(); err != nil {
		return fmt.Errorf("tmux: paste-buffer: %w", err)
	}
	return nil
}
