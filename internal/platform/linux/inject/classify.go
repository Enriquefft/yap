package inject

import (
	"strings"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// terminalClasses is the allowlist of known terminal emulators.
// Match is by exact lowercased equality against the WM_CLASS first
// segment (or the process name on Wayland where WM_CLASS may be
// empty). Order is not significant — see classify().
//
// Every entry is a real WM_CLASS value. TERM environment variable
// values like "rxvt-unicode" or "st-256color" must never appear here:
// WM_CLASS for urxvt is "urxvt" or "URxvt", and WM_CLASS for st is
// simply "st". Confusing the two leads to dead entries that never
// classify anything.
var terminalClasses = []string{
	"foot",
	"foot-server",
	"kitty",
	"alacritty",
	"wezterm",
	"org.wezfurlong.wezterm",
	"ghostty",
	"com.mitchellh.ghostty",
	"xterm",
	"urxvt",
	"konsole",
	"gnome-terminal",
	"gnome-terminal-server",
	"xfce4-terminal",
	"tilix",
	"terminator",
	"st",
}

// electronClasses is the allowlist of known Electron / Chromium-based
// editor/chat applications that benefit from clipboard+synthesized
// paste delivery instead of synthetic typing.
var electronClasses = []string{
	"code",
	"code-oss",
	"vscodium",
	"cursor",
	"claude",
	"claude-desktop",
	"discord",
	"slack",
	"obsidian",
	"notion",
	"element",
	"element-desktop",
	"zed",
	"zed-preview",
}

// browserClasses is the allowlist of known web browsers. Browsers go
// through the clipboard+synthesized paste path same as Electron apps
// because contenteditable / Monaco fields behave the same way.
var browserClasses = []string{
	"firefox",
	"firefox-developer-edition",
	"mozilla firefox",
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
	"brave-browser",
	"brave",
	"librewolf",
	"zen",
	"zen-browser",
}

// classify maps a WM_CLASS / process-name string to an AppType. The
// match is case-insensitive on the lowercased input. Unknown classes
// return AppGeneric — the orchestrator falls back to wtype/xdotool.
func classify(class string) yinject.AppType {
	c := strings.ToLower(strings.TrimSpace(class))
	if c == "" {
		return yinject.AppGeneric
	}
	for _, name := range terminalClasses {
		if c == name {
			return yinject.AppTerminal
		}
	}
	for _, name := range electronClasses {
		if c == name {
			return yinject.AppElectron
		}
	}
	for _, name := range browserClasses {
		if c == name {
			return yinject.AppBrowser
		}
	}
	return yinject.AppGeneric
}
