package termscroll

import (
	"context"

	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// Strategy is the interface each terminal scrollback backend
// implements. The provider walks strategies in priority order; the
// first that Supports the target AND returns non-empty text wins.
type Strategy interface {
	// Name returns a stable identifier for this strategy.
	Name() string
	// Supports returns true when this strategy can read the given
	// target's scrollback.
	Supports(target inject.Target) bool
	// Read fetches terminal scrollback text. Returns ("", nil) when
	// the strategy cannot obtain text (e.g. no socket, remote control
	// disabled). Errors are non-fatal to the provider walk.
	Read(ctx context.Context) (string, error)
}
