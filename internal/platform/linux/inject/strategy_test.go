package inject

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Enriquefft/yap/internal/platform"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
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

// names delegates to strategyNames so the test assertions share one
// implementation with the production Resolve path.
func names(ss []Strategy) []string {
	return strategyNames(ss)
}

// selectFor is a thin helper for tests that don't care about ctx,
// logger capture, or the reason token. Tests that do care call
// buildStrategyOrder directly.
func selectFor(strategies []Strategy, opts platform.InjectionOptions, target yinject.Target) []Strategy {
	return buildStrategyOrder(context.Background(), nil, strategies, opts, target).strategies
}

func TestSelect_TerminalNoTmux(t *testing.T) {
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
	})
	want := []string{"osc52", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_TerminalWithTmux(t *testing.T) {
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
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
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppElectron,
	})
	want := []string{"electron", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_Browser(t *testing.T) {
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "x11",
		AppType:       yinject.AppBrowser,
	})
	want := []string{"electron", "x11"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_GenericWayland(t *testing.T) {
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppGeneric,
	})
	want := []string{"wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v", names(got), want)
	}
}

func TestSelect_GenericX11(t *testing.T) {
	got := selectFor(makeStrategies(nil), platform.InjectionOptions{}, yinject.Target{
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
	got := selectFor(makeStrategies(nil), opts, yinject.Target{
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

// TestBuildStrategyOrder_AppOverrideAppendEnterPropagates guards the
// AppendEnter plumbing that powers per-app auto-submit. The flag lives
// on the config.AppOverride, is copied into platform.AppOverride by
// daemon.InjectionOptionsFromConfig, and must surface on
// strategyOrder.appendEnter so the injector can re-append a trailing
// "\n" after trimming whisper's artifact newline. Every branch of
// buildStrategyOrder is asserted so a future refactor cannot
// accidentally leak appendEnter into natural-order / default-strategy
// paths (which must never auto-submit — only explicit per-app
// overrides opt in).
func TestBuildStrategyOrder_AppOverrideAppendEnterPropagates(t *testing.T) {
	target := yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "kitty",
		AppType:       yinject.AppTerminal,
	}
	strategies := makeStrategies(nil)

	cases := []struct {
		name string
		opts platform.InjectionOptions
		want bool
	}{
		{
			name: "matched override with AppendEnter=true propagates",
			opts: platform.InjectionOptions{
				AppOverrides: []platform.AppOverride{
					{Match: "kitty", Strategy: "wayland", AppendEnter: true},
				},
			},
			want: true,
		},
		{
			name: "matched override with AppendEnter=false does not propagate",
			opts: platform.InjectionOptions{
				AppOverrides: []platform.AppOverride{
					{Match: "kitty", Strategy: "wayland"},
				},
			},
			want: false,
		},
		{
			name: "unmatched override never propagates even when AppendEnter=true",
			opts: platform.InjectionOptions{
				AppOverrides: []platform.AppOverride{
					{Match: "firefox", Strategy: "wayland", AppendEnter: true},
				},
			},
			want: false,
		},
		{
			name: "unsupported override falls through without propagating",
			opts: platform.InjectionOptions{
				AppOverrides: []platform.AppOverride{
					// x11 on a wayland target is unsupported, the branch
					// skips the override entirely and falls to natural
					// order — AppendEnter must NOT leak.
					{Match: "kitty", Strategy: "x11", AppendEnter: true},
				},
			},
			want: false,
		},
		{
			name: "default_strategy never sets appendEnter",
			opts: platform.InjectionOptions{
				DefaultStrategy: "wayland",
			},
			want: false,
		},
		{
			name: "natural order never sets appendEnter",
			opts: platform.InjectionOptions{},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildStrategyOrder(context.Background(), nil, strategies, tc.opts, target)
			if got.appendEnter != tc.want {
				t.Errorf("appendEnter = %v, want %v (reason=%q)", got.appendEnter, tc.want, got.reason)
			}
		})
	}
}

func TestSelect_OverrideAgainstUnknownStrategyIgnored(t *testing.T) {
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "nonexistent"},
		},
	}
	got := selectFor(makeStrategies(nil), opts, yinject.Target{
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
	got := selectFor(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "rofi",
		AppType:       yinject.AppGeneric,
	})
	if !equalStrings(names(got), []string{"wayland"}) {
		t.Errorf("got %v, want [wayland]", names(got))
	}
}

// TestSelectStrategies_UnsupportedOverrideFallsThrough guards F3:
// when a user override names a strategy whose Supports() returns
// false on the current target (e.g. an x11 strategy on a wayland
// session), the override must be ignored, the natural order must be
// returned, and the audit trail must record a DEBUG-level "override
// ignored" entry — never WARN.
func TestSelectStrategies_UnsupportedOverrideFallsThrough(t *testing.T) {
	logger, buf := newCaptureHandler()
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			// x11 strategy applied to a wayland session — unsupported.
			{Match: "kitty", Strategy: "x11"},
		},
	}
	got := buildStrategyOrder(context.Background(), logger, makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "kitty",
		AppType:       yinject.AppTerminal,
	}).strategies
	want := []string{"osc52", "wayland"}
	if !equalStrings(names(got), want) {
		t.Errorf("got %v, want %v (unsupported override must fall through)", names(got), want)
	}
	lines := parseRawLogLines(t, buf)
	sawDebug := false
	for _, l := range lines {
		if l["level"] == "WARN" || l["level"] == "ERROR" {
			t.Errorf("unsupported override surfaced as %s, want DEBUG only: %v", l["level"], l)
		}
		if l["msg"] == "inject override ignored" && l["level"] == "DEBUG" {
			sawDebug = true
			if l["override_strategy"] != "x11" {
				t.Errorf("override_strategy = %v, want x11", l["override_strategy"])
			}
			if l["reason"] != "strategy does not apply to target" {
				t.Errorf("reason = %v, want apply-to-target message", l["reason"])
			}
		}
	}
	if !sawDebug {
		t.Errorf("expected DEBUG override-ignored line, got %v", lines)
	}
}

// TestSelectStrategies_DefaultStrategyPrependedWhenSupported guards C7:
// when no app_overrides matches and DefaultStrategy is set, the named
// strategy is treated as a wildcard override.
func TestSelectStrategies_DefaultStrategyPrependedWhenSupported(t *testing.T) {
	opts := platform.InjectionOptions{
		DefaultStrategy: "wayland",
	}
	got := selectFor(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppGeneric,
	})
	if len(got) == 0 || got[0].Name() != "wayland" {
		t.Errorf("expected wayland prepended via default, got %v", names(got))
	}
}

// TestSelectStrategies_DefaultStrategyAppliedToEmptyAppClass guards
// the C7 motivation: a generic-wlroots target with empty AppClass
// can never match any substring app_overrides entry, but a
// DefaultStrategy fills the gap.
func TestSelectStrategies_DefaultStrategyAppliedToEmptyAppClass(t *testing.T) {
	opts := platform.InjectionOptions{
		AppOverrides: []platform.AppOverride{
			{Match: "kitty", Strategy: "x11"},
		},
		DefaultStrategy: "wayland",
	}
	got := selectFor(makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppClass:      "",
		AppType:       yinject.AppGeneric,
	})
	if len(got) == 0 || got[0].Name() != "wayland" {
		t.Errorf("expected wayland prepended via default for empty AppClass, got %v", names(got))
	}
}

// TestSelectStrategies_DefaultStrategyIgnoredWhenUnsupported guards
// the C7 fall-through path: a default strategy that does not Supports
// the target must not be prepended, and a DEBUG line must record the
// reason.
func TestSelectStrategies_DefaultStrategyIgnoredWhenUnsupported(t *testing.T) {
	logger, buf := newCaptureHandler()
	opts := platform.InjectionOptions{
		DefaultStrategy: "x11",
	}
	got := buildStrategyOrder(context.Background(), logger, makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppGeneric,
	}).strategies
	if !equalStrings(names(got), []string{"wayland"}) {
		t.Errorf("got %v, want [wayland] (default must fall through)", names(got))
	}
	lines := parseRawLogLines(t, buf)
	sawDebug := false
	for _, l := range lines {
		if l["level"] == "WARN" || l["level"] == "ERROR" {
			t.Errorf("unsupported default surfaced as %s, want DEBUG only: %v", l["level"], l)
		}
		if l["msg"] == "inject override ignored" && l["level"] == "DEBUG" {
			sawDebug = true
		}
	}
	if !sawDebug {
		t.Errorf("expected DEBUG override-ignored line, got %v", lines)
	}
}

// TestSelectStrategies_DefaultStrategyIgnoredWhenUnknown guards the
// C7 typo-tolerance path: an unknown default strategy name must not
// crash and must surface as a DEBUG line, not a WARN.
func TestSelectStrategies_DefaultStrategyIgnoredWhenUnknown(t *testing.T) {
	logger, buf := newCaptureHandler()
	opts := platform.InjectionOptions{
		DefaultStrategy: "banana",
	}
	got := buildStrategyOrder(context.Background(), logger, makeStrategies(nil), opts, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppGeneric,
	}).strategies
	if !equalStrings(names(got), []string{"wayland"}) {
		t.Errorf("got %v, want [wayland]", names(got))
	}
	lines := parseRawLogLines(t, buf)
	sawDebug := false
	for _, l := range lines {
		if l["msg"] == "inject override ignored" && l["level"] == "DEBUG" {
			sawDebug = true
		}
	}
	if !sawDebug {
		t.Errorf("expected DEBUG override-ignored line, got %v", lines)
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
	// buildStrategyOrder just builds the order; it does not call
	// Deliver. This test guards against any future refactor that
	// accidentally couples selection with delivery.
	deliveredAt := []string{}
	strategies := makeStrategies(&deliveredAt)
	for _, s := range strategies {
		s.(*recordingStrategy).deliverErr = errSentinel
	}
	_ = selectFor(strategies, platform.InjectionOptions{}, yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
	})
	if len(deliveredAt) != 0 {
		t.Errorf("buildStrategyOrder must not call Deliver, but recorded %v", deliveredAt)
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

// parseRawLogLines decodes a JSONHandler buffer into a slice of
// generic maps so individual tests can assert on whichever fields
// matter without sharing a typed shape with injector_test.go.
func parseRawLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("parse log line: %v: %s", err, string(line))
		}
		out = append(out, rec)
	}
	return out
}
