// Package termscroll is a hint provider that reads terminal scrollback
// via a strategy pattern. Each terminal emulator backend (kitty,
// wezterm, ghostty, tmux) is a Strategy; the provider walks them in
// priority order and returns the first successful result.
//
// Phase 12 ships the kitty strategy only. tmux, wezterm, and ghostty
// are deferred to Phase 12.5.
package termscroll
