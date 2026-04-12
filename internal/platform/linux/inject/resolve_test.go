package inject

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/internal/platform"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// TestInjectorSatisfiesStrategyResolver is a compile-time guard that
// catches any future refactor that accidentally breaks the optional
// StrategyResolver interface. If this fails to compile, the Resolve
// method signature has drifted from pkg/yap/inject.StrategyResolver
// and every debug-tooling consumer will break.
func TestInjectorSatisfiesStrategyResolver(t *testing.T) {
	var _ yinject.StrategyResolver = (*Injector)(nil)
}

// newResolveInjector builds a Linux Injector with a fully populated
// Deps bag suitable for Resolve tests. The caller supplies the deps
// fake; the strategy list is wired exactly like production so the
// test exercises the real selection logic.
func newResolveInjector(t *testing.T, opts platform.InjectionOptions, deps Deps) *Injector {
	t.Helper()
	inj, err := New(opts, deps, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return inj
}

// TestResolve_TmuxBranch covers the tmux strategy branch: an
// AppTerminal target inside a tmux session must report strategy=tmux
// with the natural-order fall-throughs osc52 (when PreferOSC52) and
// wayland following.
func TestResolve_TmuxBranch(t *testing.T) {
	const tree = `{"focused":true,"app_id":"kitty","pid":4242}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
			"TMUX":            "/tmp/tmux-1000/default,1234,0",
		}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{PreferOSC52: true}, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "tmux" {
		t.Errorf("Strategy = %q, want tmux", decision.Strategy)
	}
	if decision.Tool != "tmux" {
		t.Errorf("Tool = %q, want tmux", decision.Tool)
	}
	if !decision.Target.Tmux {
		t.Error("Target.Tmux must be true on a tmux-annotated target")
	}
	if decision.Target.AppType != yinject.AppTerminal {
		t.Errorf("Target.AppType = %v, want AppTerminal", decision.Target.AppType)
	}
	if got, want := decision.Fallbacks, []string{"tmux", "osc52", "wayland"}; !equalStrings(got, want) {
		t.Errorf("Fallbacks = %v, want %v", got, want)
	}
	if !strings.Contains(decision.Reason, "natural order") {
		t.Errorf("Reason = %q, want natural order token", decision.Reason)
	}
}

// TestResolve_OSC52Branch covers the osc52 strategy branch: an
// AppTerminal target with PreferOSC52 (and no tmux) must report
// strategy=osc52 with wayland as fall-through.
func TestResolve_OSC52Branch(t *testing.T) {
	const tree = `{"focused":true,"app_id":"foot","pid":100}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
		}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{PreferOSC52: true}, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "osc52" {
		t.Errorf("Strategy = %q, want osc52", decision.Strategy)
	}
	if decision.Tool != "osc52" {
		t.Errorf("Tool = %q, want osc52", decision.Tool)
	}
	if decision.Target.Tmux {
		t.Error("Target.Tmux must be false outside tmux")
	}
	if decision.Target.AppType != yinject.AppTerminal {
		t.Errorf("Target.AppType = %v, want AppTerminal", decision.Target.AppType)
	}
	if got, want := decision.Fallbacks, []string{"osc52", "wayland"}; !equalStrings(got, want) {
		t.Errorf("Fallbacks = %v, want %v", got, want)
	}
}

// TestResolve_ElectronBranch covers the electron strategy branch: an
// AppElectron target must report strategy=electron with wayland as
// fall-through. Tool resolves to wtype because the display server is
// wayland (electron's per-server synthesize helper uses wtype on
// wayland and xdotool on x11).
func TestResolve_ElectronBranch(t *testing.T) {
	const tree = `{"focused":true,"app_id":"code","pid":77}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
		}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{}, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "electron" {
		t.Errorf("Strategy = %q, want electron", decision.Strategy)
	}
	if decision.Tool != "wtype" {
		t.Errorf("Tool = %q, want wtype (electron on wayland uses wtype for the paste keystroke)", decision.Tool)
	}
	if decision.Target.AppType != yinject.AppElectron {
		t.Errorf("Target.AppType = %v, want AppElectron", decision.Target.AppType)
	}
	if got, want := decision.Fallbacks, []string{"electron", "wayland"}; !equalStrings(got, want) {
		t.Errorf("Fallbacks = %v, want %v", got, want)
	}
}

// TestResolve_WaylandBranch covers the wayland strategy branch: a
// generic-GUI target on Wayland must report strategy=wayland with
// tool=wtype and no fall-throughs beyond it.
func TestResolve_WaylandBranch(t *testing.T) {
	// Force the sway/hyprland detectors to miss and the wlroots
	// detector to fail, so the generic Wayland fall-through path
	// produces an AppGeneric target with empty AppClass.
	deps := Deps{
		ExecCommandContext: (&fakeExec{}).commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
		}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{}, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "wayland" {
		t.Errorf("Strategy = %q, want wayland", decision.Strategy)
	}
	if decision.Tool != "wtype" {
		t.Errorf("Tool = %q, want wtype", decision.Tool)
	}
	if decision.Target.AppType != yinject.AppGeneric {
		t.Errorf("Target.AppType = %v, want AppGeneric", decision.Target.AppType)
	}
	if got, want := decision.Fallbacks, []string{"wayland"}; !equalStrings(got, want) {
		t.Errorf("Fallbacks = %v, want %v", got, want)
	}
}

// TestResolve_X11Branch covers the x11 strategy branch: a generic-GUI
// target on X11 must report strategy=x11 with tool=xdotool.
func TestResolve_X11Branch(t *testing.T) {
	stdout := map[string]string{
		"xdotool": "12345\n",
		"xprop":   "WM_CLASS(STRING) = \"rofi\", \"rofi\"\n_NET_WM_PID(CARDINAL) = 8888\n",
	}
	fe := &fakeExec{stdout: stdout}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet:             envFunc(map[string]string{"DISPLAY": ":0"}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{}, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "x11" {
		t.Errorf("Strategy = %q, want x11", decision.Strategy)
	}
	if decision.Tool != "xdotool" {
		t.Errorf("Tool = %q, want xdotool", decision.Tool)
	}
	if decision.Target.DisplayServer != "x11" {
		t.Errorf("DisplayServer = %q, want x11", decision.Target.DisplayServer)
	}
	if decision.Target.AppClass != "rofi" {
		t.Errorf("AppClass = %q, want rofi", decision.Target.AppClass)
	}
	if got, want := decision.Fallbacks, []string{"x11"}; !equalStrings(got, want) {
		t.Errorf("Fallbacks = %v, want %v", got, want)
	}
}

// TestResolve_AppOverrideReason guards that the Reason field carries
// an app_override token when an AppOverrides entry matches. The
// selection logic already hits this branch in strategy_test.go — this
// test adds the Resolve-specific assertion that the reason propagates
// into StrategyDecision.Reason.
func TestResolve_AppOverrideReason(t *testing.T) {
	const tree = `{"focused":true,"app_id":"kitty","pid":4242}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
		}),
	}
	opts := platform.InjectionOptions{
		PreferOSC52: true,
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "wayland"},
		},
	}
	inj := newResolveInjector(t, opts, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "wayland" {
		t.Errorf("Strategy = %q, want wayland (override forces it)", decision.Strategy)
	}
	if !strings.Contains(decision.Reason, "app_override") {
		t.Errorf("Reason = %q, want app_override token", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "kitty") {
		t.Errorf("Reason = %q, want 'kitty' substring", decision.Reason)
	}
}

// TestResolve_DefaultStrategyReason guards that the Reason field
// carries a default_strategy token when the default_strategy branch
// wins.
func TestResolve_DefaultStrategyReason(t *testing.T) {
	// Empty AppClass + no AppOverrides means the branch the default
	// strategy was introduced for (C7: generic-wlroots target where no
	// substring can ever match an AppOverrides entry).
	deps := Deps{
		ExecCommandContext: (&fakeExec{}).commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
		}),
	}
	opts := platform.InjectionOptions{
		DefaultStrategy: "wayland",
	}
	inj := newResolveInjector(t, opts, deps)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if decision.Strategy != "wayland" {
		t.Errorf("Strategy = %q, want wayland", decision.Strategy)
	}
	if !strings.Contains(decision.Reason, "default_strategy") {
		t.Errorf("Reason = %q, want default_strategy token", decision.Reason)
	}
}

// TestResolve_NoDisplaySurfacesError guards the detection-failure
// path: when no display server is available, Resolve must surface an
// error instead of returning a zero-value decision. Debug tooling
// uses the error to tell the user "we could not even detect which
// compositor is active" instead of a confusing empty decision.
func TestResolve_NoDisplaySurfacesError(t *testing.T) {
	deps := Deps{
		EnvGet: envFunc(map[string]string{}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{}, deps)

	_, err := inj.Resolve(context.Background())
	if err == nil {
		t.Fatal("expected error when no display server is detected")
	}
	if !errors.Is(err, ErrNoDisplay) {
		t.Errorf("err = %v, want wrapped ErrNoDisplay", err)
	}
}

// TestResolve_DoesNotCallDeliver guards the "pure query" contract:
// Resolve must not call Deliver on any strategy, even indirectly. We
// wire in a strategy list that records every Deliver call and assert
// it stays empty after a Resolve.
func TestResolve_DoesNotCallDeliver(t *testing.T) {
	const tree = `{"focused":true,"app_id":"kitty","pid":4242}`
	fe := &fakeExec{stdout: map[string]string{"swaymsg": tree}}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		EnvGet: envFunc(map[string]string{
			"WAYLAND_DISPLAY": "wayland-1",
			"SWAYSOCK":        "/run/sway",
		}),
	}
	inj := newResolveInjector(t, platform.InjectionOptions{PreferOSC52: true}, deps)
	// Replace the production strategy list with recording stubs so
	// any accidental Deliver call flips the guard bit.
	deliveredAt := []string{}
	inj.strategies = makeStrategies(&deliveredAt)

	decision, err := inj.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(deliveredAt) != 0 {
		t.Errorf("Resolve must not call Deliver, but recorded %v", deliveredAt)
	}
	// Selection should still produce a non-empty decision because
	// the classified target survives past the strategy swap.
	if decision.Strategy == "" {
		t.Error("Strategy should be non-empty even with recording stubs")
	}
}

