package inject

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// swayNode is the subset of the swaymsg -t get_tree JSON shape we care
// about. The full document is large; we only need focus + app_id /
// window_properties.class + pid.
type swayNode struct {
	Focused          bool             `json:"focused"`
	AppID            string           `json:"app_id"`
	PID              int              `json:"pid"`
	WindowProperties *swayWindowProps `json:"window_properties,omitempty"`
	Nodes            []*swayNode      `json:"nodes,omitempty"`
	FloatingNodes    []*swayNode      `json:"floating_nodes,omitempty"`
}

type swayWindowProps struct {
	Class string `json:"class"`
}

// detectSway runs `swaymsg -t get_tree` and walks the result for the
// focused container. Returns a classified Target on success.
func detectSway(ctx context.Context, deps Deps) (yinject.Target, error) {
	cmd := deps.ExecCommandContext(ctx, "swaymsg", "-t", "get_tree")
	out, err := cmd.Output()
	if err != nil {
		return yinject.Target{}, fmt.Errorf("swaymsg get_tree: %w", err)
	}
	var root swayNode
	if err := json.Unmarshal(out, &root); err != nil {
		return yinject.Target{}, fmt.Errorf("swaymsg parse: %w", err)
	}
	focused := findFocusedSwayNode(&root)
	if focused == nil {
		return yinject.Target{}, fmt.Errorf("swaymsg: no focused node")
	}
	class := focused.AppID
	if class == "" && focused.WindowProperties != nil {
		// XWayland clients expose WM_CLASS via window_properties.class
		// rather than app_id.
		class = focused.WindowProperties.Class
	}
	windowID := ""
	if focused.PID > 0 {
		windowID = strconv.Itoa(focused.PID)
	}
	return classifyAndBuildTarget("wayland", class, windowID), nil
}

// findFocusedSwayNode does a depth-first search for the first node
// with focused=true. Both .nodes and .floating_nodes are walked.
func findFocusedSwayNode(n *swayNode) *swayNode {
	if n == nil {
		return nil
	}
	if n.Focused {
		return n
	}
	for _, child := range n.Nodes {
		if found := findFocusedSwayNode(child); found != nil {
			return found
		}
	}
	for _, child := range n.FloatingNodes {
		if found := findFocusedSwayNode(child); found != nil {
			return found
		}
	}
	return nil
}
