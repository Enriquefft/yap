package inject

import (
	"context"
	"fmt"
	"time"

	"github.com/Enriquefft/yap/internal/platform"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// electronStrategy delivers text to Electron and browser apps via the
// clipboard + synthesized Ctrl+V path. Most Monaco-style autocomplete
// editors swallow synthetic typing but accept clipboard pastes
// reliably, which is why this dedicated strategy exists.
//
// The flow is: save current clipboard → write text → synthesize
// Ctrl+V via wtype/xdotool → wait briefly → restore the previous
// clipboard. The restore wait routes through Deps.SleepCtx so tests
// have a single hook for time control and ctx cancellation unblocks
// the wait promptly. The wait is the only bounded delay permitted in
// the inject package, and it is documented in the Phase 4 plan §1.8.
type electronStrategy struct {
	deps Deps
	opts platform.InjectionOptions
}

// electronRestoreDelay is the bounded wait between writing the new
// clipboard contents and restoring the saved value. Picked at 50ms
// because synthesized Ctrl+V is processed by the focused app within
// a few milliseconds; 50ms gives even slow Electron apps time to
// observe the clipboard before we put the original back.
const electronRestoreDelay = 50 * time.Millisecond

// newElectronStrategy constructs an electron strategy bound to deps
// and opts. The opts argument carries the ElectronStrategy field —
// when it is not "clipboard", Supports returns false so the
// orchestrator falls through to the wayland/x11 strategies.
func newElectronStrategy(deps Deps, opts platform.InjectionOptions) *electronStrategy {
	return &electronStrategy{deps: deps, opts: opts}
}

// Name returns the strategy identifier used in audit logs and
// app_overrides lookups.
func (s *electronStrategy) Name() string { return "electron" }

// Supports returns true for Electron and browser targets when the
// configured ElectronStrategy is "clipboard". The "keystroke"
// alternative is served by the wayland/x11 generic strategies, so
// this strategy declines those targets explicitly.
func (s *electronStrategy) Supports(target yinject.Target) bool {
	if s.opts.ElectronStrategy != "" && s.opts.ElectronStrategy != "clipboard" {
		return false
	}
	return target.AppType == yinject.AppElectron || target.AppType == yinject.AppBrowser
}

// Deliver writes text to the clipboard, synthesizes a paste keystroke
// via the appropriate display-server tool, then restores the original
// clipboard contents after a bounded wait. Returns an error if the
// clipboard write or the paste synthesis fails.
func (s *electronStrategy) Deliver(ctx context.Context, target yinject.Target, text string) error {
	saved, saveErr := s.deps.ClipboardRead()
	if err := s.deps.ClipboardWrite(text); err != nil {
		return fmt.Errorf("electron: clipboard write: %w", err)
	}
	var pasteErr error
	switch target.DisplayServer {
	case "wayland":
		pasteErr = s.synthesizeCtrlVWayland(ctx)
	case "x11":
		pasteErr = s.synthesizeCtrlVX11(ctx)
	default:
		pasteErr = yinject.ErrStrategyUnsupported
	}
	if pasteErr != nil {
		// Restore the clipboard before bubbling so we don't leave
		// the user with stale paste content on a failed delivery.
		if saveErr == nil {
			_ = s.deps.ClipboardWrite(saved)
		}
		return fmt.Errorf("electron: synthesize paste: %w", pasteErr)
	}
	if saveErr == nil {
		// SleepCtx returns ctx.Err() on cancellation. We proceed with
		// the restore regardless so the clipboard is always returned
		// to its saved state — the alternative (skipping restore on
		// cancel) would leave the user with stale paste content.
		_ = s.deps.SleepCtx(ctx, electronRestoreDelay)
		_ = s.deps.ClipboardWrite(saved)
	}
	return nil
}

// synthesizeCtrlVWayland sends Ctrl+V via wtype. The strategy uses the
// pressed/released modifier syntax wtype expects.
func (s *electronStrategy) synthesizeCtrlVWayland(ctx context.Context) error {
	if _, err := s.deps.LookPath("wtype"); err != nil {
		return yinject.ErrStrategyUnsupported
	}
	cmd := s.deps.ExecCommandContext(ctx, "wtype", "-M", "ctrl", "v", "-m", "ctrl")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wtype ctrl+v: %w", err)
	}
	return nil
}

// synthesizeCtrlVX11 sends Ctrl+V via xdotool.
func (s *electronStrategy) synthesizeCtrlVX11(ctx context.Context) error {
	if _, err := s.deps.LookPath("xdotool"); err != nil {
		return yinject.ErrStrategyUnsupported
	}
	cmd := s.deps.ExecCommandContext(ctx, "xdotool", "key", "--clearmodifiers", "ctrl+v")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xdotool ctrl+v: %w", err)
	}
	return nil
}
