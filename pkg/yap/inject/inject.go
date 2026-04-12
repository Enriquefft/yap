// Package inject is the public, library-facing surface of yap's
// app-aware text injection (Pillar 2 in ARCHITECTURE.md). It declares
// the Injector contract, the Target description used by selection,
// the AppType classification enum, and the per-strategy interface.
//
// Concrete implementations live under internal/platform/<os>/inject/.
// Library consumers depend only on these types; the production
// daemon wires the OS-specific Injector at startup.
package inject

import (
	"context"
	"errors"
	"fmt"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// Injector delivers text to the currently focused application. It is
// the public face of yap's Pillar 2 (app-aware text injection).
type Injector interface {
	// Inject delivers text to the active target. Implementations
	// detect the target, classify the app, select a strategy, and
	// deliver. Returns nil on successful delivery; returns an error
	// only when every applicable strategy has failed.
	Inject(ctx context.Context, text string) error

	// InjectStream delivers text as it arrives on in. Partial-safe
	// targets (GUI textboxes, clipboard-backed strategies) receive
	// partial chunks. Unsafe targets (terminals, shells) batch until
	// the final chunk.
	InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error
}

// StrategyResolver is an optional interface that Injector
// implementations may satisfy. It exposes the target detection and
// strategy selection logic as a pure query — no Deliver call is made.
// Debug tooling (`yap paste --dry-run`, `yap record --resolve`) uses
// this to report which strategy would fire without mutating any
// external state.
//
// The interface is optional because platforms under active
// development may not ship it on the first cut: the CLI type-asserts
// at call time and surfaces a clean error when the current Injector
// does not implement it, rather than forcing every platform backend
// to supply a stub. This is the same pattern io.Closer uses to extend
// io.Writer without breaking non-closeable writers.
type StrategyResolver interface {
	// Resolve runs target detection + classification + strategy
	// selection and returns the StrategyDecision that the next Inject
	// would act on. It MUST NOT call Deliver on any strategy, mutate
	// the clipboard, synthesize keystrokes, or otherwise touch
	// external state — it is a pure query.
	//
	// Returns an error only when target detection or classification
	// fails outright (no display server, for example). Selection
	// failure — "no strategy applies to this target" — is reported
	// via an empty Strategy in the decision, never as an error, so
	// callers can display the classified Target alongside the "no
	// applicable strategies" verdict in one render.
	Resolve(ctx context.Context) (StrategyDecision, error)
}

// StrategyDecision is the structured output of StrategyResolver.Resolve.
// Every field is populated even when Strategy is empty so that debug
// tooling can render a meaningful report regardless of whether any
// strategy matched.
type StrategyDecision struct {
	// Target is the classified active-window description produced by
	// detection. It carries the DisplayServer, AppClass, AppType,
	// and the additive Tmux / SSHRemote modifiers. Always populated,
	// even when no strategy matches — the user needs the classification
	// to understand why nothing fired.
	Target Target

	// Strategy is the Name() of the strategy that would fire first on
	// the next Inject call. Empty when no strategy applies to Target
	// (for example, a wayland generic target on a build with no
	// wayland strategy registered). Never a failure signal on its own
	// — callers render the empty string as "none" or similar.
	Strategy string

	// Tool is the underlying executable the winning strategy would
	// invoke (wtype, xdotool, tmux, osc52, …). Derived from the
	// strategy's identity, not from a live subprocess probe: Resolve
	// is pure and must not shell out. Empty when Strategy is empty or
	// when the strategy does not map to a single external tool.
	Tool string

	// Fallbacks is the full ordered list of strategy Name() values
	// that Inject would walk if the first one fell through via
	// ErrStrategyUnsupported or a transient failure. The first entry
	// is always equal to Strategy when Strategy is non-empty.
	Fallbacks []string

	// Reason is a human-readable explanation of why this particular
	// ordering was selected. Stable tokens ("app_override matched
	// kitty → osc52", "default_strategy wayland", "natural order",
	// "no applicable strategies") let the user map the decision back
	// to their config without diffing code.
	Reason string
}

// Target is the classified description of the focused application.
// It is the output of active-window detection and the input to
// Strategy.Supports. The zero value is deliberately invalid — callers
// must populate DisplayServer at a minimum.
type Target struct {
	// DisplayServer identifies the compositor / OS graphical server.
	// Valid values: "wayland", "x11", "macos", "windows".
	DisplayServer string
	// WindowID is an opaque, platform-specific identifier for the
	// focused window. On Linux this is typically the focused
	// application's process id (as a decimal string) so OSC52 can
	// resolve the rendering tty via /proc/<pid>/fd/0.
	WindowID string
	// AppClass is the WM_CLASS / bundle id / process name of the
	// focused application. Used as the key for app_overrides lookup.
	AppClass string
	// AppType is the classification result: terminal, electron,
	// browser, or generic.
	AppType AppType
	// Tmux is an additive modifier set when $TMUX is detected at
	// inject time and the underlying app is a terminal.
	Tmux bool
	// SSHRemote is an additive modifier set when $SSH_TTY or
	// $SSH_CONNECTION is detected at inject time.
	SSHRemote bool
}

// AppType is the classified application category. Values are mutually
// exclusive — additive modifiers (tmux, ssh) live on Target as bools.
type AppType int

const (
	// AppGeneric is the fallback classification when no other
	// allowlist matches.
	AppGeneric AppType = iota
	// AppTerminal covers foot, kitty, alacritty, wezterm, ghostty,
	// xterm, urxvt, konsole, gnome-terminal, xfce4-terminal.
	AppTerminal
	// AppElectron covers code, code-oss, vscodium, cursor, claude,
	// discord, slack, obsidian, notion, element, zed.
	AppElectron
	// AppBrowser covers firefox, chromium, chrome, brave, librewolf,
	// zen.
	AppBrowser
)

// String returns a stable lowercase identifier for the AppType used in
// audit logs, app-override matching, and debugging output. Unknown
// values render as AppType(N) so a missing enum addition surfaces in
// audit logs instead of silently aliasing to "generic".
func (a AppType) String() string {
	switch a {
	case AppGeneric:
		return "generic"
	case AppTerminal:
		return "terminal"
	case AppElectron:
		return "electron"
	case AppBrowser:
		return "browser"
	default:
		return fmt.Sprintf("AppType(%d)", int(a))
	}
}

// Strategy is a single text-delivery implementation. Implementations
// live under internal/platform/<os>/inject/ — this interface is the
// public contract that lets external Go programs assemble or test
// custom strategies through the inject package.
type Strategy interface {
	// Name returns the human-readable strategy name used in logs
	// and audit output.
	Name() string
	// Supports returns true when this strategy is applicable for
	// the given Target.
	Supports(Target) bool
	// Deliver writes text via this strategy. Returns
	// ErrStrategyUnsupported when the strategy cannot serve the
	// concrete target (so the orchestrator falls through cleanly);
	// returns any other non-nil error to mark a real delivery
	// failure that should still be logged but should also cause
	// fall-through.
	Deliver(ctx context.Context, text string) error
}

// ErrStrategyUnsupported is returned by a Strategy.Deliver
// implementation to signal "I cannot serve this target" — distinct
// from a transient delivery failure. The orchestrator uses this
// sentinel to fall through to the next applicable strategy without
// surfacing the unsupported attempt as a real error.
var ErrStrategyUnsupported = errors.New("inject: strategy does not support this target")
