// Package inject implements yap's app-aware text injection (Pillar 2)
// for Linux. It owns the deep module that detects the focused window,
// classifies the application, and selects a per-app delivery strategy.
//
// The package is constructed via New, which accepts InjectionOptions
// (bridged from pkg/yap/config.InjectionConfig at the daemon layer)
// and a Deps bag for dependency injection. Tests substitute fakes
// through Deps; production wires Deps via NewDeps().
//
// There is no fallback-everything chain. Every Inject call:
//
//  1. Detects the active target via the compositor-specific backend
//     (Sway, Hyprland, X11; generic wlroots falls through to the
//     generic-GUI strategy without WM_CLASS).
//  2. Classifies the AppClass against allowlists in classify.go.
//  3. Walks the strategy list in priority order, applies any matching
//     app_overrides, and delivers via the first strategy that
//     Supports the target and does not return ErrStrategyUnsupported
//     or a transient error.
//  4. Emits one structured slog audit line summarizing the outcome,
//     plus a WARN line per failed attempt.
//
// Quality bar: zero hard-coded stdlib sleep literals in this package.
// All polling waits route through Deps.Sleep so tests have a single
// hook for time control. The TestNoLiteralTimeSleep guard in
// noglobals_test.go fails the build if any production .go file
// contains the literal token sequence used by the stdlib's blocking
// sleep primitive.
package inject
