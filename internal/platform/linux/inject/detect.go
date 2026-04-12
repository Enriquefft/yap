package inject

import (
	"context"
	"errors"
	"fmt"
	"strings"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// ErrNoDisplay is returned by Detect when neither WAYLAND_DISPLAY nor
// DISPLAY are set in the environment. Callers fall back to a generic
// Target without active-window information.
var ErrNoDisplay = errors.New("inject: no display server (WAYLAND_DISPLAY / DISPLAY unset)")

// Detect resolves the focused window into a classified Target. The
// dispatcher tries compositor-specific detectors in order:
//
//  1. Sway via swaymsg, when SWAYSOCK is set.
//  2. Hyprland via hyprctl, when HYPRLAND_INSTANCE_SIGNATURE is set.
//  3. Generic wlroots via the wlr-foreign-toplevel-management
//     unstable-v1 protocol, applied to any Wayland compositor that
//     advertises zwlr_foreign_toplevel_manager_v1 (river, niri,
//     Wayfire, Cosmic, Sway forks, …).
//  4. X11 via xdotool + xprop, when DISPLAY is set.
//
// On every successful detection, annotate() is called to layer the
// additive Tmux / SSHRemote bits onto the Target. When no Wayland
// detector recognizes the focused window — for example, the
// compositor does not expose the wlr foreign-toplevel manager, or
// nothing is currently focused — Detect returns a generic-GUI Target
// and the orchestrator falls through to the wtype strategy.
func Detect(ctx context.Context, deps Deps) (yinject.Target, error) {
	server := detectDisplayServer(deps)
	switch server {
	case "wayland":
		if deps.EnvGet("SWAYSOCK") != "" {
			t, err := detectSway(ctx, deps)
			if err == nil {
				return annotate(t, deps), nil
			}
		}
		if deps.EnvGet("HYPRLAND_INSTANCE_SIGNATURE") != "" {
			t, err := detectHyprland(ctx, deps)
			if err == nil {
				return annotate(t, deps), nil
			}
		}
		t, err := detectWlroots(ctx, deps)
		if err == nil {
			return annotate(t, deps), nil
		}
		// Compositor does not expose wlr-foreign-toplevel-management,
		// no toplevel is focused, or the wayland connection failed
		// outright. Fall through to a generic-GUI Target without
		// AppClass; the orchestrator dispatches to the wtype strategy.
		return annotate(yinject.Target{
			DisplayServer: "wayland",
			AppType:       yinject.AppGeneric,
		}, deps), nil
	case "x11":
		t, err := detectX11(ctx, deps)
		if err != nil {
			return yinject.Target{}, fmt.Errorf("x11 detect: %w", err)
		}
		return annotate(t, deps), nil
	default:
		return yinject.Target{}, ErrNoDisplay
	}
}

// detectDisplayServer returns the active display-server kind based on
// the standard environment variables. Wayland wins when both are set
// because every modern Wayland session also exports DISPLAY for
// XWayland clients.
func detectDisplayServer(deps Deps) string {
	if deps.EnvGet("WAYLAND_DISPLAY") != "" {
		return "wayland"
	}
	if deps.EnvGet("DISPLAY") != "" {
		return "x11"
	}
	return ""
}

// annotate applies the additive Tmux / SSHRemote bits to a Target.
// These environment variables are read once per Inject call (not
// cached) because the user may exec into a different shell between
// recording sessions.
func annotate(t yinject.Target, deps Deps) yinject.Target {
	if t.AppType == yinject.AppTerminal && deps.EnvGet("TMUX") != "" {
		t.Tmux = true
	}
	if deps.EnvGet("SSH_TTY") != "" || deps.EnvGet("SSH_CONNECTION") != "" {
		t.SSHRemote = true
	}
	return t
}

// classifyAndBuildTarget is a small helper used by every detector to
// turn a parsed (appClass, windowID, displayServer) tuple into a
// classified Target. It exists so the per-compositor parsers stay
// focused on their wire format.
func classifyAndBuildTarget(displayServer, appClass, windowID string) yinject.Target {
	return yinject.Target{
		DisplayServer: displayServer,
		AppClass:      strings.ToLower(strings.TrimSpace(appClass)),
		WindowID:      windowID,
		AppType:       classify(appClass),
	}
}
