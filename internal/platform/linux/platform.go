// Package linux provides Linux-specific implementations of all platform interfaces
// defined in internal/platform. It uses evdev for hotkeys, PortAudio for audio,
// wtype/ydotool/xdotool for text input, and beeep/libnotify for notifications.
package linux

import "github.com/hybridz/yap/internal/platform"

// NewPlatform returns a Platform with all Linux implementations wired together.
// This is the composition root for all OS-specific behavior on Linux.
func NewPlatform() platform.Platform {
	return platform.Platform{
		NewRecorder: NewRecorder,
		Chime:       NewChimePlayer(),
		NewHotkey:   NewHotkey,
		HotkeyCfg:   NewHotkeyConfig(),
		Paster:      NewPaster(),
		Notifier:    NewNotifier(),
	}
}
