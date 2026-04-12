package inject

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// detectX11 resolves the focused X11 window into a classified Target.
// Implementation:
//
//  1. `xdotool getactivewindow` returns the focused window id.
//  2. `xprop -id <id> WM_CLASS _NET_WM_PID` yields the WM_CLASS pair
//     and the owning process id.
//  3. The WM_CLASS pair is `"instance", "class"` — we use the second
//     entry (class) as the canonical AppClass for allowlist matching.
func detectX11(ctx context.Context, deps Deps) (yinject.Target, error) {
	winID, err := x11ActiveWindow(ctx, deps)
	if err != nil {
		return yinject.Target{}, err
	}
	class, pid, err := x11WindowProps(ctx, deps, winID)
	if err != nil {
		return yinject.Target{}, err
	}
	windowID := strconv.Itoa(pid)
	if pid <= 0 {
		// Fall back to the X11 window id when no PID is exposed.
		windowID = winID
	}
	return classifyAndBuildTarget("x11", class, windowID), nil
}

// x11ActiveWindow runs `xdotool getactivewindow` and returns the
// trimmed window id string.
func x11ActiveWindow(ctx context.Context, deps Deps) (string, error) {
	out, err := deps.ExecCommandContext(ctx, "xdotool", "getactivewindow").Output()
	if err != nil {
		return "", fmt.Errorf("xdotool getactivewindow: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("xdotool getactivewindow: empty output")
	}
	return id, nil
}

// x11WindowProps runs `xprop -id <id> WM_CLASS _NET_WM_PID` and
// extracts the WM_CLASS class entry plus the owning PID. The xprop
// output looks like:
//
//	WM_CLASS(STRING) = "kitty", "kitty"
//	_NET_WM_PID(CARDINAL) = 12345
//
// Missing pids return 0; missing classes return the empty string.
func x11WindowProps(ctx context.Context, deps Deps, winID string) (class string, pid int, err error) {
	out, err := deps.ExecCommandContext(ctx, "xprop", "-id", winID, "WM_CLASS", "_NET_WM_PID").Output()
	if err != nil {
		return "", 0, fmt.Errorf("xprop -id %s: %w", winID, err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "WM_CLASS"):
			class = parseXpropWMClass(line)
		case strings.HasPrefix(line, "_NET_WM_PID"):
			pid = parseXpropPID(line)
		}
	}
	return class, pid, nil
}

// parseXpropWMClass extracts the second quoted string from a line of
// the form `WM_CLASS(STRING) = "instance", "class"`. Returns the empty
// string when the input is malformed or the property is unset.
func parseXpropWMClass(line string) string {
	eq := strings.Index(line, "=")
	if eq < 0 {
		return ""
	}
	rest := line[eq+1:]
	parts := strings.Split(rest, ",")
	if len(parts) == 0 {
		return ""
	}
	pick := parts[len(parts)-1]
	pick = strings.TrimSpace(pick)
	pick = strings.Trim(pick, "\"")
	return pick
}

// parseXpropPID extracts the integer from a line of the form
// `_NET_WM_PID(CARDINAL) = 12345`. Returns 0 when the line is missing
// the property or unparseable.
func parseXpropPID(line string) int {
	eq := strings.Index(line, "=")
	if eq < 0 {
		return 0
	}
	rest := strings.TrimSpace(line[eq+1:])
	if rest == "" {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0
	}
	return n
}
