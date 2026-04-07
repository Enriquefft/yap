package inject

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// hyprlandActiveWindow matches the JSON shape of `hyprctl
// activewindow -j`. We only need class + pid; the rest of the document
// is intentionally ignored.
type hyprlandActiveWindow struct {
	Class string `json:"class"`
	PID   int    `json:"pid"`
}

// detectHyprland runs `hyprctl activewindow -j` and parses the result.
func detectHyprland(ctx context.Context, deps Deps) (yinject.Target, error) {
	cmd := deps.ExecCommand("hyprctl", "activewindow", "-j")
	out, err := cmd.Output()
	if err != nil {
		return yinject.Target{}, fmt.Errorf("hyprctl activewindow: %w", err)
	}
	var w hyprlandActiveWindow
	if err := json.Unmarshal(out, &w); err != nil {
		return yinject.Target{}, fmt.Errorf("hyprctl parse: %w", err)
	}
	windowID := ""
	if w.PID > 0 {
		windowID = strconv.Itoa(w.PID)
	}
	return classifyAndBuildTarget("wayland", w.Class, windowID), nil
}
