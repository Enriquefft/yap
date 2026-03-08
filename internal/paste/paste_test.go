package paste

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/atotto/clipboard"
)

func restoreMocks() {
	execCommand = exec.Command
	clipboardRead = clipboard.ReadAll
	clipboardWrite = clipboard.WriteAll
	lookPath = exec.LookPath
	osStat = os.Stat
	sleep = time.Sleep
}

func TestDisplayDetection_Wayland(t *testing.T) {
	defer restoreMocks()

	// Mock env vars
	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "/usr/bin/wtype", nil
		}
		return "", errors.New("not found")
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		// Return a command that will succeed (echo command)
		return exec.Command("echo", "test")
	}

	err := Paste("test text")
	if err != nil {
		t.Errorf("Paste() error = %v, want nil", err)
	}
}

func TestDisplayDetection_X11(t *testing.T) {
	defer restoreMocks()

	// Mock env vars
	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	sleep = func(d time.Duration) {}

	err := Paste("test text")
	if err != nil {
		t.Errorf("Paste() error = %v, want nil", err)
	}
}

func TestDisplayDetection_None(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	err := Paste("test text")
	if err == nil {
		t.Error("Paste() error = nil, want error about no display server")
	}
}

func TestWaylandChain_wtype(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	wtypeCalled := false
	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			wtypeCalled = true
			return "/usr/bin/wtype", nil
		}
		return "", errors.New("not found")
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !wtypeCalled {
		t.Error("wtype was not called, expected it to be called")
	}
}

func TestWaylandChain_wtypeFallsBackToClipboard(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		return "", errors.New("not found")
	}

	clipboardWriteCalled := false
	clipboardWrite = func(text string) error {
		clipboardWriteCalled = true
		return nil
	}

	Paste("test text")

	if !clipboardWriteCalled {
		t.Error("clipboard write was not called, expected it to be called")
	}
}

func TestWaylandChain_ydotool(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	wtypeCalled := false
	ydotoolCalled := false
	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			wtypeCalled = true
			return "", errors.New("not found")
		}
		if name == "ydotool" {
			ydotoolCalled = true
			return "/usr/bin/ydotool", nil
		}
		return "", errors.New("not found")
	}

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !wtypeCalled {
		t.Error("wtype was not attempted")
	}

	if !ydotoolCalled {
		t.Error("ydotool was not called after wtype failed")
	}
}

func TestYdotoolSocketCheck_SocketExists(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "", errors.New("not found")
		}
		if name == "ydotool" {
			return "/usr/bin/ydotool", nil
		}
		return "", errors.New("not found")
	}

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}

	ydotoolCalled := false
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "ydotool" {
			ydotoolCalled = true
		}
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !ydotoolCalled {
		t.Error("ydotool was not called, socket exists")
	}
}

func TestYdotoolSocketCheck_SocketMissing(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "", errors.New("not found")
		}
		if name == "ydotool" {
			return "/usr/bin/ydotool", nil
		}
		return "", errors.New("not found")
	}

	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	clipboardWriteCalled := false
	clipboardWrite = func(text string) error {
		clipboardWriteCalled = true
		return nil
	}

	Paste("test text")

	if !clipboardWriteCalled {
		t.Error("clipboard write was not called, ydotool socket missing")
	}
}

func TestYdotoolSocketEnvOverride(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	oldSocket := os.Getenv("YDOTOOL_SOCKET")
	os.Setenv("YDOTOOL_SOCKET", "/custom/socket")
	defer os.Setenv("YDOTOOL_SOCKET", oldSocket)

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "", errors.New("not found")
		}
		if name == "ydotool" {
			return "/usr/bin/ydotool", nil
		}
		return "", errors.New("not found")
	}

	statCalledPath := ""
	osStat = func(name string) (os.FileInfo, error) {
		statCalledPath = name
		return nil, nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if statCalledPath != "/custom/socket" {
		t.Errorf("stat called with %s, want /custom/socket", statCalledPath)
	}
}

func TestX11Paste(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	sleepCalled := false
	sleep = func(d time.Duration) {
		if d == 150*time.Millisecond {
			sleepCalled = true
		}
	}

	calledArgs := ""
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "xdotool" {
			calledArgs = args[0]
			cmd := exec.Command("echo", "test")
			return cmd
		}
		return &exec.Cmd{}
	}

	Paste("test text")

	if !sleepCalled {
		t.Error("150ms sleep was not called before xdotool")
	}

	if calledArgs != "type" {
		t.Errorf("xdotool args = %v, want type", calledArgs)
	}
}

func TestX11Paste_WithClearmodifiers(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	sleep = func(d time.Duration) {}

	hasClearmodifiers := false
	execCommand = func(name string, args ...string) *exec.Cmd {
		for _, arg := range args {
			if arg == "--clearmodifiers" {
				hasClearmodifiers = true
			}
		}
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !hasClearmodifiers {
		t.Error("xdotool called without --clearmodifiers")
	}
}

func TestX11ExitCodeChecked(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	sleep = func(d time.Duration) {}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), "false")
		return cmd
	}

	err := Paste("test text")
	if err == nil {
		t.Error("Paste() error = nil, want error on xdotool failure")
	}
}

func TestClipboardSave(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	readCalled := false
	clipboardRead = func() (string, error) {
		readCalled = true
		return "", nil
	}

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !readCalled {
		t.Error("clipboard read was not called before paste")
	}
}

func TestClipboardRestoreOnSuccess(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	readCalled := false
	writeCalled := false
	writtenContent := ""

	clipboardRead = func() (string, error) {
		readCalled = true
		return "saved content", nil
	}

	clipboardWrite = func(text string) error {
		writeCalled = true
		writtenContent = text
		return nil
	}

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	sleeps := []time.Duration{}
	sleep = func(d time.Duration) {
		sleeps = append(sleeps, d)
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	Paste("test text")

	if !readCalled {
		t.Error("clipboard read was not called")
	}

	if !writeCalled {
		t.Error("clipboard write was not called after paste success")
	}

	if writtenContent != "saved content" {
		t.Errorf("clipboard restored with %s, want saved content", writtenContent)
	}

	has100msSleep := false
	for _, s := range sleeps {
		if s == 100*time.Millisecond {
			has100msSleep = true
		}
	}

	if !has100msSleep {
		t.Error("100ms sleep was not called after paste success")
	}
}

func TestClipboardNotRestoredOnFailure(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	readCalled := false
	writeCalled := false

	clipboardRead = func() (string, error) {
		readCalled = true
		return "saved content", nil
	}

	clipboardWrite = func(text string) error {
		writeCalled = true
		return nil
	}

	sleep = func(d time.Duration) {}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(context.Background(), "false")
		return cmd
	}

	Paste("test text")

	if !readCalled {
		t.Error("clipboard read was not called")
	}

	if writeCalled {
		t.Error("clipboard write was called after paste failure, should not restore")
	}
}

func TestClipboardSaveError_AllowsPaste(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	clipboardRead = func() (string, error) {
		return "", errors.New("clipboard read error")
	}

	clipboardWrite = func(text string) error {
		return nil
	}

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	sleep = func(d time.Duration) {}

	err := Paste("test text")
	if err != nil {
		t.Errorf("Paste() error = %v, want nil (should still paste)", err)
	}
}

func TestClipboardNotRestoredWhenSaveFails(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	clipboardRead = func() (string, error) {
		return "", errors.New("clipboard read error")
	}

	writeCalled := false
	clipboardWrite = func(text string) error {
		writeCalled = true
		return nil
	}

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	sleep = func(d time.Duration) {}

	Paste("test text")

	if writeCalled {
		t.Error("clipboard write was called after save error, should not restore")
	}
}

func TestX11WithInterruptedSyscall(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	sleep = func(d time.Duration) {
		time.Sleep(d)
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		// Simulate interrupted syscall
		cmd := exec.Command("sh", "-c", "kill -INT $$")
		return cmd
	}

	err := Paste("test text")
	if err == nil {
		t.Error("Paste() error = nil, want error on interrupted syscall")
	}
}

func TestWtypeInterrupted(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("sh", "-c", "kill -INT $$")
		return cmd
	}

	err := Paste("test text")
	if err == nil {
		t.Error("Paste() error = nil, want error on interrupted wtype")
	}
}

func TestYdotoolInterrupted(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "", errors.New("not found")
		}
		if name == "ydotool" {
			return "/usr/bin/ydotool", nil
		}
		return "", errors.New("not found")
	}

	osStat = func(name string) (os.FileInfo, error) {
		return nil, nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "ydotool" {
			cmd := exec.Command("sh", "-c", "kill -INT $$")
			return cmd
		}
		return &exec.Cmd{}
	}

	err := Paste("test text")
	if err == nil {
		t.Error("Paste() error = nil, want error on interrupted ydotool")
	}
}

func TestExecCommandSignalExit(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "false")
	err := cmd.Run()
	if err == nil {
		t.Error("expected non-nil error from false command")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.ExitCode())
	}
}

func TestPasteEmptyString(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", "test")
		return cmd
	}

	err := Paste("")
	if err != nil {
		t.Errorf("Paste() with empty string error = %v, want nil", err)
	}
}

func TestPasteWithSpecialCharacters(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	testText := `Hello "world"! This has 'quotes' and $symbols.`

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", testText)
		return cmd
	}

	err := Paste(testText)
	if err != nil {
		t.Errorf("Paste() error = %v, want nil", err)
	}
}

func TestPasteMultilineText(t *testing.T) {
	defer restoreMocks()

	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", "")
	defer os.Setenv("DISPLAY", oldDisplay)

	testText := "Line 1\nLine 2\nLine 3"

	lookPath = func(name string) (string, error) {
		return "/usr/bin/wtype", nil
	}

	execCommand = func(name string, args ...string) *exec.Cmd {
		cmd := exec.Command("echo", testText)
		return cmd
	}

	err := Paste(testText)
	if err != nil {
		t.Errorf("Paste() error = %v, want nil", err)
	}
}

func TestWaylandWithBothDisplays(t *testing.T) {
	defer restoreMocks()

	// When both WAYLAND_DISPLAY and DISPLAY are set, Wayland should be used
	oldWayland := os.Getenv("WAYLAND_DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")
	defer os.Setenv("WAYLAND_DISPLAY", oldWayland)

	oldDisplay := os.Getenv("DISPLAY")
	os.Setenv("DISPLAY", ":0")
	defer os.Setenv("DISPLAY", oldDisplay)

	xdotoolCalled := false
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "xdotool" {
			xdotoolCalled = true
		}
		cmd := exec.Command("echo", "test")
		return cmd
	}

	lookPath = func(name string) (string, error) {
		if name == "wtype" {
			return "/usr/bin/wtype", nil
		}
		return "", errors.New("not found")
	}

	Paste("test text")

	if xdotoolCalled {
		t.Error("xdotool was called when WAYLAND_DISPLAY is set, should use Wayland path")
	}
}
