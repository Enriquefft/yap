package hotkey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/holoplot/go-evdev"
)

// Listener holds open keyboard devices and runs the event loop.
type Listener struct {
	devices []*evdev.InputDevice
	mu      sync.Mutex
}

// FindKeyboards scans /dev/input/event* for keyboard-capable devices.
// Returns error with exact usermod command on permission denied (INPUT-06).
// Keyboard = device with evdev.EV_KEY and at least one code KEY_A..KEY_Z.
func FindKeyboards() (*Listener, error) {
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		return nil, fmt.Errorf("failed to list input devices: %w", err)
	}

	var keyboards []*evdev.InputDevice
	var permErr error

	for _, devicePath := range paths {
		dev, err := evdev.Open(devicePath.Path)
		if err != nil {
			if os.IsPermission(err) {
				// Save permission error to provide helpful message
				permErr = err
				continue
			}
			continue // Skip other errors (device not available, etc.)
		}

		// Check if device has EV_KEY capability
		// CapableEvents returns []EvCode, not a bool
		keyCodes := dev.CapableEvents(evdev.EV_KEY)
		if len(keyCodes) == 0 {
			dev.Close()
			continue
		}

		// Check if device has alpha keys (KEY_A through KEY_Z)
		// This identifies full keyboards vs keypads/special devices
		if hasAlphaKeys(keyCodes) {
			keyboards = append(keyboards, dev)
		} else {
			dev.Close()
		}
	}

	if len(keyboards) == 0 {
		if permErr != nil {
			return nil, buildPermissionError(permErr)
		}
		return nil, errors.New("no keyboard devices found")
	}

	return &Listener{devices: keyboards}, nil
}

// hasAlphaKeys checks if the list of key codes contains any KEY_A through KEY_Z.
func hasAlphaKeys(codes []evdev.EvCode) bool {
	for _, code := range codes {
		if code >= evdev.KEY_A && code <= evdev.KEY_Z {
			return true
		}
	}
	return false
}

// Run starts the hold-to-talk event loop. Blocks until ctx is cancelled.
// Calls onPress when hotkeyCode key is pressed (value=1).
// Calls onRelease when hotkeyCode key is released (value=0).
// Repeat events (value=2) are ignored.
//
// Critical: dev.NonBlock() is called before loop; dev.Fd() is NEVER called after (INPUT-04).
// Critical: dev.Grab() is NEVER called (INPUT-03).
func (l *Listener) Run(ctx context.Context, hotkeyCode evdev.EvCode, onPress func(), onRelease func()) {
	l.mu.Lock()
	if len(l.devices) == 0 {
		l.mu.Unlock()
		return
	}
	// Copy devices to avoid holding lock during goroutines
	devices := make([]*evdev.InputDevice, len(l.devices))
	copy(devices, l.devices)
	l.mu.Unlock()

	var wg sync.WaitGroup
	for _, dev := range devices {
		wg.Add(1)
		go func(d *evdev.InputDevice) {
			defer wg.Done()

			// Set non-blocking mode first (INPUT-04)
			if err := d.NonBlock(); err != nil {
				return
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// ReadOne() is the only read method we use after NonBlock()
					// Returns *InputEvent, not InputEvent
					event, err := d.ReadOne()
					if err != nil {
						// EAGAIN means no data available, retry
						if strings.Contains(err.Error(), "EAGAIN") ||
							strings.Contains(err.Error(), "resource temporarily unavailable") {
							time.Sleep(10 * time.Millisecond)
							continue
						}
						// Other errors: exit this goroutine
						return
					}

					// Check for KEY events matching our hotkey
					if event.Type == evdev.EV_KEY && event.Code == hotkeyCode {
						switch event.Value {
						case 1: // Press
							if onPress != nil {
								onPress()
							}
						case 0: // Release
							if onRelease != nil {
								onRelease()
							}
						case 2: // Repeat - ignore
						}
					}
				}
			}
		}(dev)
	}

	wg.Wait()
}

// Close releases all open device file descriptors.
func (l *Listener) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, dev := range l.devices {
		if dev != nil {
			_ = dev.Close()
		}
	}
	l.devices = nil
}

// HotkeyCode converts a config hotkey name like "KEY_RIGHTCTRL" to evdev.EvCode.
func HotkeyCode(name string) (evdev.EvCode, error) {
	// Use evdev package's KEYFromString map
	code, ok := evdev.KEYFromString[name]
	if !ok {
		// Invalid name - provide helpful error with examples
		return 0, fmt.Errorf("invalid hotkey name '%s'. Example valid names: KEY_RIGHTCTRL, KEY_SPACE, KEY_A", name)
	}
	return code, nil
}

// buildPermissionError creates a helpful error message with the exact usermod command.
func buildPermissionError(err error) error {
	return fmt.Errorf("permission denied on /dev/input/event* — fix with: usermod -aG input $USER\nOriginal error: %w", err)
}
