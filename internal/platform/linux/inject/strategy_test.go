package inject

import (
	"context"
	"errors"
	"testing"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// recordingStrategy is a tiny Strategy used by selection tests. It
// records the order of Deliver calls and can be configured to fail.
type recordingStrategy struct {
	name        string
	supportsFn  func(yinject.Target) bool
	deliverErr  error
	deliveredAt *[]string
}

func (r *recordingStrategy) Name() string {
	return r.name
}

func (r *recordingStrategy) Supports(t yinject.Target) bool {
	if r.supportsFn == nil {
		return true
	}
	return r.supportsFn(t)
}

func (r *recordingStrategy) Deliver(ctx context.Context, t yinject.Target, text string) error {
	if r.deliveredAt != nil {
		*r.deliveredAt = append(*r.deliveredAt, r.name)
	}
	return r.deliverErr
}

// supportsType returns a Supports function that matches a single
// AppType. It is the most common gating shape used in the natural
// strategy ordering.
func supportsType(at yinject.AppType) func(yinject.Target) bool {
	return func(t yinject.Target) bool { return t.AppType == at }
}

// makeStrategies builds the standard ordered list used by selection
// tests. The strategies are in the order they would be registered by
// injector.New: tmux → osc52 → electron → wayland → x11.
func makeStrategies(deliveredAt *[]string) []Strategy {
	tmux := &recordingStrategy{name: "tmux", supportsFn: func(t yinject.Target) bool {
		return t.AppType == yinject.AppTerminal && t.Tmux
	}, deliveredAt: deliveredAt}
	osc52 := &recordingStrategy{name: "osc52", supportsFn: supportsType(yinject.AppTerminal), deliveredAt: deliveredAt}
	electron := &recordingStrategy{name: "electron", supportsFn: func(t yinject.Target) bool {
		return t.AppType == yinject.AppElectron || t.AppType == yinject.AppBrowser
	}, deliveredAt: deliveredAt}
	wayland := &recordingStrategy{name: "wayland", supportsFn: func(t yinject.Target) bool {
		return t.DisplayServer == "wayland"
	}, deliveredAt: deliveredAt}
	x11 := &recordingStrategy{name: "x11", supportsFn: func(t yinject.Target) bool {
		return t.DisplayServer == "x11"
	}, deliveredAt: deliveredAt}
	return []Strategy{tmux, osc52, electron, wayland, x11}
}

func names(ss []Strategy) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name()
	}
	return out
}

func TestSelect_TerminalNoTmux(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
	})
	want := []string{"osc52", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_TerminalWithTmux(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
		Tmux:          true,
	})
	want := []string{"tmux", "osc52", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_Electron(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppElectron,
	})
	want := []string{"electron", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_Browser(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "x11",
		AppType:       yinject.AppBrowser,
	})
	want := []string{"electron", "x11"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_GenericWayland(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppGeneric,
	})
	want := []string{"wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_GenericX11(t *testing.T) {
	got := selectStrategies(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "x11",
		AppType:       yinject.AppGeneric,
	})
	want := []string{"x11"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_AppOverrideForcesNamedStrategyFirst(t *testing.T) {
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "wayland"},
		},
	}
	got := selectStrategies(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "kitty",
		AppType:       yinject.AppTerminal,
	})
	if len(got) == 0 || got[0].Name() != "wayland" {
		t.Errorf("expected wayland forced to front, got %v", names(got))
	}
	// Natural order strategies (osc52) must still be in fall-through.
	if !contains(names(got), "osc52") {
		t.Errorf("osc52 should remain in fall-through; got %v", names(got))
	}
}

func TestSelect_OverrideAgainstUnknownStrategyIgnored(t *testing.T) {
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "nonexistent"},
		},
	}
	got := selectStrategies(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "kitty",
		AppType:       yinject.AppTerminal,
	})
	want := []string{"osc52", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v (override should be ignored)", names(got), want)
	}
}

func TestSelect_OverrideForcesUniqueAtFront(t *testing.T) {
	// Forcing wayland on a generic Wayland target should not produce a
	// list with two wayland entries.
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			{Match: "rofi", Strategy: "wayland"},
		},
	}
	got := selectStrategies(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "rofi",
		AppType:       yinject.AppGeneric,
	})
	if !equalStrings(names(got), []string{"wayland"}) {
		t.Errorf("got %v, want [wayland]", names(got))
	}
}

func TestMatchesOverride(t *testing.T) {
	cases := []struct {
		appClass string
		match    string
		want     bool
	}{
		{"kitty", "kitty", true},
		{"google-chrome", "chrome", true},
		{"firefox", "FIREFOX", true},
		{"firefox", "", false},
		{"", "kitty", false},
		{"wezterm", "kitty", false},
	}
	for _, tc := range cases {
		t.Run(tc.appClass+"|"+tc.match, func(t *testing.T) {
			if got := matchesOverride(tc.appClass, tc.match); got != tc.want {
				t.Errorf("matchesOverride(%q, %q) = %v, want %v", tc.appClass, tc.match, got, tc.want)
			}
		})
	}
}

// errSentinel is reused to verify Deliver error propagation in
// selection tests.
var errSentinel = errors.New("sentinel")

func TestSelect_DeliverErrorIsNotInterceptedByOrder(t *testing.T) {
	// selectStrategies just builds the order; it does not call
	// Deliver. This test guards against any future refactor that
	// accidentally couples selection with delivery.
	deliveredAt := []string{}
	strategies := makeStrategies(&deliveredAt)
	for _, s := range strategies {
		s.(*recordingStrategy).deliverErr = errSentinel
	}
	_ = selectStrategies(strategies, platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
	})
	if len(deliveredAt) != 0 {
		t.Errorf("selectStrategies must not call Deliver, but recorded %v", deliveredAt)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
