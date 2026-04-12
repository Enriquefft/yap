package inject

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Enriquefft/yap/internal/platform"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
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

// strategyOrder is the structured output of buildStrategyOrder. It
// carries the ordered strategy list AND the human-readable reason the
// order was chosen, so both Inject (which just needs the list) and the
// Resolve debug surface (which needs the reason) consume the same
// helper instead of duplicating the branching logic.
type strategyOrder struct {
	// strategies is the final per-Target call order. Inject walks
	// this slice in sequence; the Resolve surface derives both
	// Strategy (first entry) and Fallbacks (all entries) from it.
	strategies []Strategy
	// reason is a stable, human-readable token explaining which
	// branch of the selection logic produced strategies. See the
	// reasonXxx constants below for the canonical values.
	reason string
	// appendEnter is true when the selection was driven by an
	// app_override entry whose AppendEnter flag is set. The injector
	// appends a trailing newline to the (already-trimmed) text before
	// dispatching to the chosen strategy so keystroke strategies type
	// Enter at the end — the explicit opt-in for terminals and form
	// fields that want auto-submit. Only app_overrides can turn this
	// on; the natural-order and default_strategy branches never do.
	appendEnter bool
}

// Stable reason tokens emitted by buildStrategyOrder. Keeping them as
// named constants prevents drift between the selection logic and the
// Resolve debug surface that renders them to users.
const (
	reasonAppOverride     = "app_override"
	reasonDefaultStrategy = "default_strategy"
	reasonNaturalOrder    = "natural order"
	reasonNoneApplicable  = "no applicable strategies"
)

// buildStrategyOrder returns the per-Target call order along with the
// reason it was chosen. It applies app_overrides first (an override
// forces a named strategy to the front of the list, with the natural
// order kept as fall-through). When no app_overrides matches and
// opts.DefaultStrategy is non-empty, the named default strategy is
// treated as a wildcard override and prepended to the front (same
// Supports gate). The natural-order walk follows.
//
// Override resolution ignores a named strategy that does not exist in
// the registry (typo in config) and also ignores one whose Supports()
// returns false on the current target (e.g. an x11 override applied to
// a wayland session). Both ignore-cases emit a DEBUG-level audit line
// so users grepping the trail can see why their config did not take
// effect — no WARN, because a user-authored config choice that simply
// doesn't apply is not an operational failure.
//
// The function takes a logger so every selection decision is
// attributable. Tests that do not care pass a nil logger.
//
// buildStrategyOrder is the single source of truth for selection
// ordering. Both the injector's Inject path and its Resolve debug
// surface call it — if a future refactor introduces another caller,
// it MUST go through this helper, not a duplicated copy of the
// branching logic.
func buildStrategyOrder(ctx context.Context, logger *slog.Logger, strategies []Strategy, opts platform.InjectionOptions, target yinject.Target) strategyOrder {
	natural := naturalOrder(strategies, target)

	for _, ov := range opts.AppOverrides {
		if !matchesOverride(target.AppClass, ov.Match) {
			continue
		}
		forced := strategyByName(strategies, ov.Strategy)
		if forced == nil {
			logOverrideIgnored(ctx, logger, target, ov.Strategy, "unknown strategy name")
			continue
		}
		if !forced.Supports(target) {
			logOverrideIgnored(ctx, logger, target, ov.Strategy, "strategy does not apply to target")
			continue
		}
		return strategyOrder{
			strategies:  prependUnique(forced, natural),
			reason:      fmt.Sprintf("%s matched (%s -> %s)", reasonAppOverride, ov.Match, forced.Name()),
			appendEnter: ov.AppendEnter,
		}
	}

	if opts.DefaultStrategy != "" {
		forced := strategyByName(strategies, opts.DefaultStrategy)
		if forced == nil {
			logOverrideIgnored(ctx, logger, target, opts.DefaultStrategy, "unknown default strategy name")
		} else if !forced.Supports(target) {
			logOverrideIgnored(ctx, logger, target, opts.DefaultStrategy, "default strategy does not apply to target")
		} else {
			return strategyOrder{
				strategies: prependUnique(forced, natural),
				reason:     fmt.Sprintf("%s %s", reasonDefaultStrategy, forced.Name()),
			}
		}
	}

	if len(natural) == 0 {
		return strategyOrder{
			strategies: nil,
			reason:     reasonNoneApplicable,
		}
	}
	return strategyOrder{
		strategies: natural,
		reason:     reasonNaturalOrder,
	}
}

// logOverrideIgnored emits a DEBUG-level structured log line when an
// override or a default_strategy entry cannot be honoured. The line
// is deliberately DEBUG — an inapplicable user config choice is not
// an operational warning, just an explanation for the audit trail.
func logOverrideIgnored(ctx context.Context, logger *slog.Logger, target yinject.Target, name, reason string) {
	if logger == nil {
		return
	}
	logger.LogAttrs(ctx, slog.LevelDebug, "inject override ignored",
		slog.String("override_strategy", name),
		slog.String("target.app_class", target.AppClass),
		slog.String("target.display_server", target.DisplayServer),
		slog.String("target.app_type", target.AppType.String()),
		slog.String("reason", reason))
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

// strategyNames returns a slice of Name() values for the supplied
// strategy list. Used by Resolve to populate StrategyDecision.Fallbacks
// and by tests to assert the selected order, so both paths share one
// implementation.
func strategyNames(strategies []Strategy) []string {
	out := make([]string, len(strategies))
	for i, s := range strategies {
		out[i] = s.Name()
	}
	return out
}

// toolForStrategy maps a strategy name to the canonical user-facing
// tool label surfaced by StrategyResolver.Resolve. The mapping is
// static (not a live probe) because Resolve is a pure query and must
// not shell out to LookPath or otherwise touch external state. For
// strategies whose underlying tool depends on the display server
// (electron uses wtype on Wayland, xdotool on X11), the target's
// DisplayServer selects the appropriate label.
//
// Returns the empty string when name does not correspond to a known
// strategy — the Resolve caller then renders an empty Tool field
// rather than guessing.
func toolForStrategy(name string, target yinject.Target) string {
	switch name {
	case "tmux":
		return "tmux"
	case "osc52":
		return "osc52"
	case "electron":
		switch target.DisplayServer {
		case "wayland":
			return "wtype"
		case "x11":
			return "xdotool"
		default:
			return ""
		}
	case "wayland":
		return "wtype"
	case "x11":
		return "xdotool"
	default:
		return ""
	}
}
