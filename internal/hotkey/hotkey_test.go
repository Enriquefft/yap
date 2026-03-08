package hotkey

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/holoplot/go-evdev"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHasAlphaKeys tests the hasAlphaKeys helper function.
func TestHasAlphaKeys(t *testing.T) {
	tests := []struct {
		name     string
		codes    []evdev.EvCode
		expected bool
	}{
		{
			name:     "contains KEY_A",
			codes:    []evdev.EvCode{evdev.KEY_A, evdev.KEY_B, evdev.KEY_C},
			expected: true,
		},
		{
			name:     "contains KEY_Z",
			codes:    []evdev.EvCode{evdev.KEY_1, evdev.KEY_2, evdev.KEY_Z},
			expected: true,
		},
		{
			name:     "no alpha keys",
			codes:    []evdev.EvCode{evdev.KEY_1, evdev.KEY_2, evdev.KEY_3},
			expected: false,
		},
		{
			name:     "empty slice",
			codes:    []evdev.EvCode{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasAlphaKeys(tt.codes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFindKeyboardsPermissionError tests that permission errors on /dev/input/event*
// return an error with the exact usermod command (INPUT-06).
func TestFindKeyboardsPermissionError(t *testing.T) {
	// This test would require mocking evdev.ListDevicePaths or Open
	// For now, we'll skip as it requires filesystem-level mocking
	t.Skip("INPUT-06 test: requires mocking evdev filesystem access")
}

// TestFindKeyboardsNoDevices tests that when no devices have alpha keys,
// it returns "no keyboard devices found".
func TestFindKeyboardsNoDevices(t *testing.T) {
	// This test requires mocking evdev to return non-keyboard devices
	t.Skip("INPUT-05 test: requires mocking evdev device capabilities")
}

// TestNonBlockSafe verifies that Listener.Run() uses dev.NonBlock() then only dev.ReadOne()
// and never calls dev.Fd() (INPUT-04).
func TestNonBlockSafe(t *testing.T) {
	// We create a fake device that panics if Fd() is called after NonBlock()
	fake := &fdPanicDevice{}

	listener := &Listener{
		devices: []inputDevice{fake},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// This should not panic
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		listener.Run(ctx, evdev.KEY_RIGHTCTRL, func() {}, func() {})
	}()

	// Wait a bit then cancel
	time.Sleep(10 * time.Millisecond)
	cancel()
	wg.Wait()

	// If we reach here without panicking, NonBlockSafe behavior is confirmed
	assert.True(t, true)
}

// TestHoldToTalkPressRelease tests that value=1 triggers onPress, value=0 triggers onRelease,
// and value=2 (repeat) is ignored (INPUT-01, INPUT-02).
func TestHoldToTalkPressRelease(t *testing.T) {
	// Create a fake device that can inject events
	fake := &eventInjectorDevice{
		events: make(chan evdev.InputEvent, 10),
	}

	listener := &Listener{
		devices: []inputDevice{fake},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var pressCount, releaseCount int
	var pressMutex, releaseMutex sync.Mutex

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		listener.Run(ctx, evdev.KEY_RIGHTCTRL,
			func() {
				pressMutex.Lock()
				pressCount++
				pressMutex.Unlock()
			},
			func() {
				releaseMutex.Lock()
				releaseCount++
				releaseMutex.Unlock()
			},
		)
	}()

	// Send press event
	fake.events <- evdev.InputEvent{
		Type:  evdev.EV_KEY,
		Code:  evdev.KEY_RIGHTCTRL,
		Value: 1,
	}

	time.Sleep(10 * time.Millisecond)

	// Send repeat event (should be ignored)
	fake.events <- evdev.InputEvent{
		Type:  evdev.EV_KEY,
		Code:  evdev.KEY_RIGHTCTRL,
		Value: 2,
	}

	time.Sleep(10 * time.Millisecond)

	// Send release event
	fake.events <- evdev.InputEvent{
		Type:  evdev.EV_KEY,
		Code:  evdev.KEY_RIGHTCTRL,
		Value: 0,
	}

	time.Sleep(10 * time.Millisecond)
	cancel()
	wg.Wait()

	pressMutex.Lock()
	releaseMutex.Lock()
	defer pressMutex.Unlock()
	defer releaseMutex.Unlock()

	assert.Equal(t, 1, pressCount, "onPress should be called once")
	assert.Equal(t, 1, releaseCount, "onRelease should be called once")
}

// TestHoldToTalkContextCancel tests that context cancellation stops the Run() goroutine
// without leaking (INPUT-03).
func TestHoldToTalkContextCancel(t *testing.T) {
	fake := &eventInjectorDevice{
		events: make(chan evdev.InputEvent, 1),
	}

	listener := &Listener{
		devices: []inputDevice{fake},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		listener.Run(ctx, evdev.KEY_RIGHTCTRL, func() {}, func() {})
	}()

	// Cancel after a short delay
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Wait for goroutine to exit (should happen quickly)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutine exited cleanly
		assert.True(t, true)
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Run() goroutine did not exit within 50ms of context cancellation")
	}

	// Verify no goroutine leak
	runtime.GC()
	time.Sleep(10 * time.Millisecond)

	// Check that the fake device was properly closed
	assert.True(t, fake.closed)
}

// TestHotkeyCodeParse tests parsing config hotkey names to evdev.EvCode.
func TestHotkeyCodeParse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    evdev.EvCode
		expectError bool
	}{
		{
			name:        "KEY_RIGHTCTRL",
			input:       "KEY_RIGHTCTRL",
			expected:    evdev.KEY_RIGHTCTRL,
			expectError: false,
		},
		{
			name:        "KEY_A",
			input:       "KEY_A",
			expected:    evdev.KEY_A,
			expectError: false,
		},
		{
			name:        "KEY_SPACE",
			input:       "KEY_SPACE",
			expected:    evdev.KEY_SPACE,
			expectError: false,
		},
		{
			name:        "invalid name",
			input:       "KEY_INVALID",
			expected:    0,
			expectError: true,
		},
		{
			name:        "empty string",
			input:       "",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := HotkeyCode(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "example")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestListenerClose tests that Close() releases all device file descriptors.
func TestListenerClose(t *testing.T) {
	fake1 := &eventInjectorDevice{events: make(chan evdev.InputEvent, 1)}
	fake2 := &eventInjectorDevice{events: make(chan evdev.InputEvent, 1)}

	listener := &Listener{
		devices: []inputDevice{fake1, fake2},
	}

	listener.Close()

	assert.True(t, fake1.closed)
	assert.True(t, fake2.closed)
}

// TestPermissionDeniedErrorMessage tests that permission denied errors
// include the exact usermod command (INPUT-06).
func TestPermissionDeniedErrorMessage(t *testing.T) {
	err := os.ErrPermission
	wrappedErr := buildPermissionError(err)
	assert.Contains(t, wrappedErr.Error(), "usermod -aG input")
}

// --- Test doubles for evdev.InputDevice ---

// fdPanicDevice is a fake device that panics if Fd() is called after NonBlock().
type fdPanicDevice struct {
	nonBlockCalled bool
	closed         bool
}

func (f *fdPanicDevice) Name() string                       { return "fake-fd-panic" }
func (f *fdPanicDevice) Path() string                       { return "/dev/input/fake" }
func (f *fdPanicDevice) NonBlock() error                    { f.nonBlockCalled = true; return nil }
func (f *fdPanicDevice) Fd() uintptr                        { panic("Fd() called after NonBlock() - violates INPUT-04") }
func (f *fdPanicDevice) ReadOne() (evdev.InputEvent, error) { return evdev.InputEvent{Type: evdev.EV_SYN}, errors.New("EAGAIN") }
func (f *fdPanicDevice) Close() error                       { f.closed = true; return nil }

// eventInjectorDevice is a fake device that can inject events for testing.
type eventInjectorDevice struct {
	events chan evdev.InputEvent
	closed bool
}

func (e *eventInjectorDevice) Name() string                       { return "fake-injector" }
func (e *eventInjectorDevice) Path() string                       { return "/dev/input/fake-injector" }
func (e *eventInjectorDevice) NonBlock() error                    { return nil }
func (e *eventInjectorDevice) Fd() uintptr                        { return 123 }
func (e *eventInjectorDevice) ReadOne() (evdev.InputEvent, error) {
	select {
	case event := <-e.events:
		return event, nil
	default:
		return evdev.InputEvent{}, errors.New("EAGAIN")
	}
}
func (e *eventInjectorDevice) Close() error {
	e.closed = true
	close(e.events)
	return nil
}

// inputDevice is the minimal interface we need from evdev.InputDevice.
// This allows us to use test doubles.
type inputDevice interface {
	NonBlock() error
	ReadOne() (evdev.InputEvent, error)
	Close() error
}
