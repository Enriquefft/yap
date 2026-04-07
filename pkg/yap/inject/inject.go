package inject

import (
	"context"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// Injector delivers text to the currently focused application. It is
// the public face of yap's Pillar 2 (app-aware text injection). Phase
// 4 will supply the concrete implementations; Phase 3 only declares
// the contract so downstream packages can depend on it.
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

// Target is the classified description of the focused application.
// It is the output of active-window detection and the input to
// Strategy.Supports. The zero value is deliberately invalid — callers
// must populate DisplayServer at a minimum.
type Target struct {
	// DisplayServer identifies the compositor / OS graphical server.
	// Valid values: "wayland", "x11", "macos", "windows".
	DisplayServer string
	// WindowID is an opaque, platform-specific identifier for the
	// focused window.
	WindowID string
	// AppClass is the WM_CLASS / bundle id / process name of the
	// focused application. Used as the key for app_overrides lookup.
	AppClass string
	// AppType is the classification result: terminal, electron,
	// browser, or generic. tmux / ssh additives combine via bitwise
	// OR on the enum (see AppTmux, AppSSHRemote).
	AppType AppType
}

// AppType is the classified application category. The base values
// (AppGeneric..AppBrowser) are mutually exclusive. AppTmux and
// AppSSHRemote are additive modifiers set by environment detection;
// they are ORed onto the base value at classification time.
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
	// AppTmux is an additive modifier set when $TMUX is detected.
	AppTmux
	// AppSSHRemote is an additive modifier set when $SSH_TTY or
	// $SSH_CONNECTION is detected.
	AppSSHRemote
)

// Strategy is a single text-delivery implementation. The registry
// of strategies, selection policy, and orchestration live under
// internal/platform/<os>/inject/ and are wired into an Injector at
// startup.
type Strategy interface {
	// Name returns the human-readable strategy name used in logs
	// and audit output.
	Name() string
	// Supports returns true when this strategy is applicable for
	// the given Target.
	Supports(Target) bool
	// Deliver writes text via this strategy. Returns an error if
	// delivery failed so the caller can try the next strategy.
	Deliver(ctx context.Context, text string) error
}
