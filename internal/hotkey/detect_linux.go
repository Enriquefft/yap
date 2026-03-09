package hotkey

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/holoplot/go-evdev"
)

// DetectKeyPress listens for a physical key press using evdev and returns the evdev key name.
// On Linux, this directly reads /dev/input/event* devices for perfect detection of all keys
// including standalone modifiers (Ctrl, Shift, Alt, Super, CapsLock, etc.).
// Returns the evdev name (e.g. "KEY_RIGHTCTRL") and nil error on success.
// Falls back to terminal-based detection if evdev is unavailable (permissions).
func DetectKeyPress(output io.Writer, timeout time.Duration) (string, error) {
	name, err := detectViaEvdev(output, timeout)
	if err == nil {
		return name, nil
	}

	// evdev failed (permissions, no devices, etc.) — fall back to terminal
	fmt.Fprintf(output, "\n  (evdev unavailable: %v — falling back to terminal detection)\n", err)
	return detectViaTerminal(output, timeout)
}

// detectViaEvdev uses Linux evdev to detect any key press.
func detectViaEvdev(output io.Writer, timeout time.Duration) (string, error) {
	listener, err := FindKeyboards()
	if err != nil {
		return "", err
	}
	defer listener.Close()

	listener.mu.Lock()
	devices := make([]*evdev.InputDevice, len(listener.devices))
	copy(devices, listener.devices)
	listener.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
					// Detect key press (value=1), ignore release and repeat
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
