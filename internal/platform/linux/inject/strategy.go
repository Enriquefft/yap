package inject

import (
	"context"
	"strings"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// Strategy is the Linux-local strategy interface. It's narrower than
// pkg/yap/inject.Strategy because Deliver takes the pre-classified
// Target — the orchestrator has already detected the active window
// and we want to avoid re-detection per attempt.
type Strategy interface {
	// Name returns the stable identifier used in audit logs and in
	// app_overrides.strategy lookups.
	Name() string
	// Supports returns true when the strategy is theoretically
	// applicable for the given Target. A "true" response is not a
	// guarantee Deliver will succeed; it gates whether the strategy
	// is included in the natural-order walk.
	Supports(target yinject.Target) bool
	// Deliver writes text to the focused application via this
	// strategy. Returns nil on success. Returns
	// pkg/yap/inject.ErrStrategyUnsupported when the strategy
	// dynamically discovers it cannot serve this concrete target
	// (e.g. /proc unreadable in OSC52). Returns any other non-nil
	// error to mark a real delivery failure.
	Deliver(ctx context.Context, target yinject.Target, text string) error
}

// selectStrategies returns the per-Target call order. It applies
// app_overrides first (an override forces a named strategy to the
// front of the list, with the natural order kept as fall-through),
// then collects every strategy whose Supports returns true.
//
// The function is exported package-level for testability — Injector's
// method form delegates to it.
func selectStrategies(strategies []Strategy, opts platform.InjectionOptions, target yinject.Target) []Strategy {
	natural := naturalOrder(strategies, target)
	for _, ov := range opts.AppOverrides {
		if matchesOverride(target.AppClass, ov.Match) {
			if forced := strategyByName(strategies, ov.Strategy); forced != nil {
				return prependUnique(forced, natural)
			}
		}
	}
	return natural
}

// naturalOrder filters the strategy list to those that Supports the
// target, preserving the fixed registration order. The order is
// defined in injector.New: tmux → osc52 → electron → wayland → x11.
func naturalOrder(strategies []Strategy, target yinject.Target) []Strategy {
	out := make([]Strategy, 0, len(strategies))
	for _, s := range strategies {
		if s.Supports(target) {
			out = append(out, s)
		}
	}
	return out
}

// strategyByName returns the strategy with the given Name() or nil
// when no match is found.
func strategyByName(strategies []Strategy, name string) Strategy {
	for _, s := range strategies {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// matchesOverride checks whether an app_overrides entry's Match
// substring is contained in appClass. Empty Match never matches.
// Comparison is case-insensitive on appClass (which is already
// lowercased by classifyAndBuildTarget) and on the Match string.
func matchesOverride(appClass, match string) bool {
	if appClass == "" || match == "" {
		return false
	}
	return strings.Contains(appClass, strings.ToLower(match))
}

// prependUnique places forced at the front of natural, removing any
// later occurrence so the same strategy never appears twice in the
// resulting walk.
func prependUnique(forced Strategy, natural []Strategy) []Strategy {
	out := make([]Strategy, 0, len(natural)+1)
	out = append(out, forced)
	for _, s := range natural {
		if s == forced || s.Name() == forced.Name() {
			continue
		}
		out = append(out, s)
	}
	return out
}
