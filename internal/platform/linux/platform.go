// Package linux provides Linux-specific implementations of all
// platform interfaces defined in internal/platform. It uses evdev for
// hotkeys, malgo (or PortAudio in the legacy build) for audio,
// linux/inject for app-aware text injection, and beeep/libnotify for
// notifications.
package linux

import (
	"github.com/hybridz/yap/internal/platform"
	"github.com/hybridz/yap/internal/platform/linux/inject"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// NewPlatform returns a Platform with all Linux implementations wired
// together. This is the composition root for all OS-specific behavior
// on Linux. The factory takes no arguments because it is called at
// startup before the daemon has any per-session config; per-session
// values (audio device name, injection options) are passed to the
// per-component factories at session-start time.
func NewPlatform() platform.Platform {
	return platform.Platform{
		NewRecorder:  NewRecorder,
		Chime:        NewChimePlayer(),
		NewHotkey:    NewHotkey,
		HotkeyCfg:    NewHotkeyConfig(),
		NewInjector:  newLinuxInjector,
		Notifier:     NewNotifier(),
		DeviceLister: NewDeviceLister(),
	}
}

// newLinuxInjector is the platform.NewInjectorFunc that constructs a
// Linux Injector from the bridged InjectionOptions and the production
// dependency bag. Production wiring uses a discard logger; callers
// that want audit output (the daemon, the CLI's `yap paste` debug
// command in Phase 7) supply their own slog.Logger via the unexported
// constructor below.
func newLinuxInjector(opts platform.InjectionOptions) (yinject.Injector, error) {
	return inject.New(opts, inject.NewDeps(), nil)
}
