package linux

import (
	"github.com/gen2brain/beeep"
	"github.com/hybridz/yap/internal/platform"
)

// notifier implements platform.Notifier using beeep (libnotify on Linux).
// beeep.Notify has signature func(title, message string, icon any) error.
type notifier struct {
	notifyFn func(title, message string, icon any) error
}

// NewNotifier returns a Notifier backed by beeep/libnotify.
func NewNotifier() platform.Notifier {
	return &notifier{notifyFn: beeep.Notify}
}

// newNotifierWithFn creates a notifier with an injected notify function (for tests).
func newNotifierWithFn(fn func(title, message string, icon any) error) platform.Notifier {
	return &notifier{notifyFn: fn}
}

// Notify sends a desktop notification. Always prefixes title with "yap: ".
// Best-effort: errors are silently dropped.
func (n *notifier) Notify(title, message string) {
	_ = n.notifyFn("yap: "+title, message, "")
}
