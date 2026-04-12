package inject

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Enriquefft/yap/internal/platform"
	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeClipboard records read and write events.
type fakeClipboard struct {
	current string
	reads   int
	writes  []string
	readErr error
}

func (f *fakeClipboard) read() (string, error) {
	f.reads++
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.current, nil
}

func (f *fakeClipboard) write(s string) error {
	f.writes = append(f.writes, s)
	f.current = s
	return nil
}

func TestElectronDeliverSavesWritesPastesRestores(t *testing.T) {
	cb := &fakeClipboard{current: "original"}
	calls := 0
	deps := Deps{
		ClipboardRead:  cb.read,
		ClipboardWrite: cb.write,
		LookPath: func(name string) (string, error) {
			if name == "wtype" {
				return "/usr/bin/wtype", nil
			}
			return "", os.ErrNotExist
		},
		ExecCommandContext: func(_ context.Context, name string, args ...string) *exec.Cmd {
			calls++
			return exec.Command("true")
		},
		SleepCtx: func(context.Context, time.Duration) error { return nil },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	tgt := yinject.Target{DisplayServer: "wayland", AppType: yinject.AppElectron}
	if err := s.Deliver(context.Background(), tgt, "new text"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	wantWrites := []string{"new text", "original"}
	if !equalStrings(cb.writes, wantWrites) {
		t.Errorf("clipboard writes = %v, want %v", cb.writes, wantWrites)
	}
	if calls != 1 {
		t.Errorf("exec calls = %d, want 1 (wtype paste)", calls)
	}
}

func TestElectronDeliverX11(t *testing.T) {
	cb := &fakeClipboard{current: "old"}
	var ranName string
	deps := Deps{
		ClipboardRead:  cb.read,
		ClipboardWrite: cb.write,
		LookPath: func(name string) (string, error) {
			if name == "xdotool" {
				return "/usr/bin/xdotool", nil
			}
			return "", os.ErrNotExist
		},
		ExecCommandContext: func(_ context.Context, name string, args ...string) *exec.Cmd {
			ranName = name
			return exec.Command("true")
		},
		SleepCtx: func(context.Context, time.Duration) error { return nil },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	tgt := yinject.Target{DisplayServer: "x11", AppType: yinject.AppBrowser}
	if err := s.Deliver(context.Background(), tgt, "x"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if ranName != "xdotool" {
		t.Errorf("ran %q, want xdotool", ranName)
	}
}

func TestElectronDeliverRestoresOnPasteFailure(t *testing.T) {
	cb := &fakeClipboard{current: "old"}
	deps := Deps{
		ClipboardRead:  cb.read,
		ClipboardWrite: cb.write,
		LookPath: func(string) (string, error) {
			return "/usr/bin/wtype", nil
		},
		ExecCommandContext: func(context.Context, string, ...string) *exec.Cmd { return exec.Command("false") },
		SleepCtx:           func(context.Context, time.Duration) error { return nil },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	tgt := yinject.Target{DisplayServer: "wayland", AppType: yinject.AppElectron}
	err := s.Deliver(context.Background(), tgt, "new")
	if err == nil {
		t.Fatal("expected synthesis failure to surface")
	}
	// First write was the new text; the failure path should also have
	// restored the saved value.
	if len(cb.writes) != 2 || cb.writes[1] != "old" {
		t.Errorf("clipboard writes = %v, want [new old]", cb.writes)
	}
}

func TestElectronDeliverPropagatesClipboardWriteError(t *testing.T) {
	cb := &fakeClipboard{current: "x"}
	cb.readErr = nil
	deps := Deps{
		ClipboardRead:  cb.read,
		ClipboardWrite: func(string) error { return errors.New("xclip dead") },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "wayland", AppType: yinject.AppElectron}, "x")
	if err == nil {
		t.Fatal("expected clipboard write failure")
	}
}

func TestElectronDeliverDoesNotRestoreWhenSaveFailed(t *testing.T) {
	writes := []string{}
	deps := Deps{
		ClipboardRead:      func() (string, error) { return "", errors.New("blocked") },
		ClipboardWrite:     func(s string) error { writes = append(writes, s); return nil },
		LookPath:           func(string) (string, error) { return "/usr/bin/wtype", nil },
		ExecCommandContext: func(context.Context, string, ...string) *exec.Cmd { return exec.Command("true") },
		SleepCtx:           func(context.Context, time.Duration) error { return nil },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	if err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "wayland", AppType: yinject.AppElectron}, "new"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(writes) != 1 || writes[0] != "new" {
		t.Errorf("writes = %v, want [new] (no restore when save failed)", writes)
	}
}

func TestElectronSupports(t *testing.T) {
	s := newElectronStrategy(Deps{}, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	if !s.Supports(yinject.Target{AppType: yinject.AppElectron}) {
		t.Error("Supports must include Electron")
	}
	if !s.Supports(yinject.Target{AppType: yinject.AppBrowser}) {
		t.Error("Supports must include Browser")
	}
	if s.Supports(yinject.Target{AppType: yinject.AppTerminal}) {
		t.Error("Supports must reject terminal targets")
	}
	if s.Supports(yinject.Target{AppType: yinject.AppGeneric}) {
		t.Error("Supports must reject generic targets")
	}
}

func TestElectronSupportsRespectsKeystrokeMode(t *testing.T) {
	s := newElectronStrategy(Deps{}, platform.InjectionOptions{ElectronStrategy: "keystroke"})
	if s.Supports(yinject.Target{AppType: yinject.AppElectron}) {
		t.Error("electron strategy must opt out when ElectronStrategy=keystroke")
	}
}

func TestElectronUnsupportedDisplayServer(t *testing.T) {
	deps := Deps{
		ClipboardRead:  func() (string, error) { return "", nil },
		ClipboardWrite: func(string) error { return nil },
		SleepCtx:       func(context.Context, time.Duration) error { return nil },
	}
	s := newElectronStrategy(deps, platform.InjectionOptions{ElectronStrategy: "clipboard"})
	err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "macos", AppType: yinject.AppElectron}, "x")
	if err == nil {
		t.Fatal("expected error on unsupported display server")
	}
}
