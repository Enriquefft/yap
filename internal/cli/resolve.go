package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/hybridz/yap/pkg/yap/inject"
)

// writeStrategyDecision renders a StrategyDecision to w in the
// stable text format shared between `yap paste --dry-run` and
// `yap record --resolve`. Keeping the format in one helper means
// both commands emit the same layout, and a future `--json` or
// machine-readable mode only has to grow in one place.
//
// The rendering deliberately hides no fields — the user is looking
// at this because something routed unexpectedly, so the full shape
// of the decision (target, strategy, tool, fallbacks, reason) is
// always present. Empty values are rendered as "<none>" so the
// output is unambiguous when, for example, no strategy applied or
// the Tool field is not populated.
//
// Output format (verbatim):
//
//	target:
//	  display_server: wayland
//	  window_id:      0x1234
//	  app_class:      kitty
//	  app_type:       terminal
//	  tmux:           false
//	  ssh_remote:     false
//	strategy:  osc52
//	tool:      osc52
//	fallbacks: osc52, wayland, x11
//	reason:    app_override matched (kitty -> osc52)
func writeStrategyDecision(w io.Writer, decision inject.StrategyDecision) {
	t := decision.Target
	fmt.Fprintln(w, "target:")
	fmt.Fprintf(w, "  display_server: %s\n", orNone(t.DisplayServer))
	fmt.Fprintf(w, "  window_id:      %s\n", orNone(t.WindowID))
	fmt.Fprintf(w, "  app_class:      %s\n", orNone(t.AppClass))
	fmt.Fprintf(w, "  app_type:       %s\n", t.AppType.String())
	fmt.Fprintf(w, "  tmux:           %t\n", t.Tmux)
	fmt.Fprintf(w, "  ssh_remote:     %t\n", t.SSHRemote)
	fmt.Fprintf(w, "strategy:  %s\n", orNone(decision.Strategy))
	fmt.Fprintf(w, "tool:      %s\n", orNone(decision.Tool))
	fmt.Fprintf(w, "fallbacks: %s\n", formatFallbacks(decision.Fallbacks))
	fmt.Fprintf(w, "reason:    %s\n", orNone(decision.Reason))
}

// orNone renders the empty string as "<none>" and passes every other
// value through unchanged. Used by writeStrategyDecision so empty
// fields (no classified app class, no winning strategy, no reason)
// are unambiguous in the rendered output.
func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

// formatFallbacks joins the fallback strategy names with ", " and
// renders an empty list as "<none>". Kept separate from orNone
// because the join step is specific to fallbacks.
func formatFallbacks(fallbacks []string) string {
	if len(fallbacks) == 0 {
		return "<none>"
	}
	return strings.Join(fallbacks, ", ")
}
