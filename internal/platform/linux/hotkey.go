package linux

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/holoplot/go-evdev"
	"github.com/Enriquefft/yap/internal/platform"
)

// hotkeyListener implements platform.Hotkey using Linux evdev.
type hotkeyListener struct {
	devices []*evdev.InputDevice
	mu      sync.Mutex
}

// NewHotkey scans /dev/input/event* for keyboard devices and returns a Hotkey listener.
// Returns an error with an exact usermod command on permission denied.
// Keyboard = device with EV_KEY and at least one alpha key (KEY_A..KEY_Z).
func NewHotkey() (platform.Hotkey, error) {
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
				permErr = err
				continue
			}
			continue
		}

		keyCodes := dev.CapableEvents(evdev.EV_KEY)
		if len(keyCodes) == 0 {
			dev.Close()
			continue
		}

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
		return nil, fmt.Errorf("no keyboard devices found")
	}

	return &hotkeyListener{devices: keyboards}, nil
}

// Listen starts the hold-to-talk event loop. Blocks until ctx is cancelled.
// Calls onPress when the key is pressed (value=1).
// Calls onRelease when the key is released (value=0).
// Repeat events (value=2) are ignored.
//
// Critical: NonBlock() is called before the read loop; Fd() is never called after.
// Critical: Grab() is never called — input is shared with other applications.
func (l *hotkeyListener) Listen(ctx context.Context, key platform.KeyCode, onPress, onRelease func()) {
	hotkeyCode := evdev.EvCode(key)

	l.mu.Lock()
	if len(l.devices) == 0 {
		l.mu.Unlock()
		return
	}
	devices := make([]*evdev.InputDevice, len(l.devices))
	copy(devices, l.devices)
	l.mu.Unlock()

	var wg sync.WaitGroup
	for _, dev := range devices {
		wg.Add(1)
		go func(d *evdev.InputDevice) {
			defer wg.Done()

			if err := d.NonBlock(); err != nil {
				return
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
					event, err := d.ReadOne()
					if err != nil {
						if strings.Contains(err.Error(), "EAGAIN") ||
							strings.Contains(err.Error(), "resource temporarily unavailable") {
							time.Sleep(10 * time.Millisecond)
							continue
						}
						return
					}

					if event.Type == evdev.EV_KEY && event.Code == hotkeyCode {
						switch event.Value {
						case 1:
							if onPress != nil {
								onPress()
							}
						case 0:
							if onRelease != nil {
								onRelease()
							}
						}
					}
				}
			}
		}(dev)
	}

	wg.Wait()
}

// Close releases all open keyboard device file descriptors.
func (l *hotkeyListener) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, dev := range l.devices {
		if dev != nil {
			_ = dev.Close()
		}
	}
	l.devices = nil
}

// hasAlphaKeys reports whether the key code list contains KEY_A..KEY_Z.
func hasAlphaKeys(codes []evdev.EvCode) bool {
	for _, code := range codes {
		if code >= evdev.KEY_A && code <= evdev.KEY_Z {
			return true
		}
	}
	return false
}

// buildPermissionError creates a helpful error with the exact usermod command.
func buildPermissionError(err error) error {
	return fmt.Errorf("permission denied on /dev/input/event* — fix with: usermod -aG input $USER\nOriginal error: %w", err)
}

// hotkeyConfig implements platform.HotkeyConfig for Linux using evdev key names.
type hotkeyConfig struct{}

// NewHotkeyConfig returns a HotkeyConfig for Linux.
func NewHotkeyConfig() platform.HotkeyConfig {
	return &hotkeyConfig{}
}

// ValidKey returns true if name is a valid evdev key name.
func (h *hotkeyConfig) ValidKey(name string) bool {
	_, ok := evdev.KEYFromString[name]
	return ok
}

// ParseKey converts an evdev key name (e.g. "KEY_RIGHTCTRL") to a platform.KeyCode.
func (h *hotkeyConfig) ParseKey(name string) (platform.KeyCode, error) {
	code, ok := evdev.KEYFromString[name]
	if !ok {
		return 0, fmt.Errorf("invalid hotkey name %q — examples: KEY_RIGHTCTRL, KEY_SPACE, KEY_A", name)
	}
	return platform.KeyCode(code), nil
}

// DetectKey waits for a physical key press and returns its evdev name.
// Uses evdev for accurate detection of all keys (including modifiers).
// Falls back to terminal-based detection if evdev is unavailable.
func (h *hotkeyConfig) DetectKey(ctx context.Context) (string, error) {
	name, err := detectViaEvdev(ctx)
	if err == nil {
		return name, nil
	}
	// evdev failed — fall back to terminal detection
	timeout := 10 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	return detectViaTerminal(io.Discard, timeout)
}

// detectViaEvdev uses evdev devices to detect the first key press.
func detectViaEvdev(ctx context.Context) (string, error) {
	listener, err := NewHotkey()
	if err != nil {
		return "", err
	}
	l, ok := listener.(*hotkeyListener)
	if !ok {
		return "", fmt.Errorf("unexpected listener type %T", listener)
	}
	defer l.Close()

	l.mu.Lock()
	devices := make([]*evdev.InputDevice, len(l.devices))
	copy(devices, l.devices)
	l.mu.Unlock()

	type result struct {
		code evdev.EvCode
	}
	ch := make(chan result, 1)

	for _, dev := range devices {
		go func(d *evdev.InputDevice) {
			if err := d.NonBlock(); err != nil {
				return
			}
			for {
				select {
				case <-ctx.Done():
					return
				default:
					event, err := d.ReadOne()
					if err != nil {
						if strings.Contains(err.Error(), "EAGAIN") ||
							strings.Contains(err.Error(), "resource temporarily unavailable") {
							time.Sleep(10 * time.Millisecond)
							continue
						}
						return
					}
					if event.Type == evdev.EV_KEY && event.Value == 1 {
						select {
						case ch <- result{code: event.Code}:
						default:
						}
						return
					}
				}
			}
		}(dev)
	}

	select {
	case r := <-ch:
		name := evdev.CodeName(evdev.EV_KEY, r.code)
		if name == "" || name == "UNKNOWN" {
			return "", fmt.Errorf("unknown key code %d", r.code)
		}
		return name, nil
	case <-ctx.Done():
		return "", fmt.Errorf("timeout waiting for key press")
	}
}
