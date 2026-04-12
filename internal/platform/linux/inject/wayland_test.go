package inject

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"testing"
	"time"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeWaylandExec is a Wayland-specific exec recorder. The test
// patches LookPath to gate which binary is "found".
type fakeWaylandExec struct {
	calls   []fakeCall
	wtypeBuf  *bytes.Buffer
	ydotoolBuf *bytes.Buffer
	wtypeFail bool
}

func (f *fakeWaylandExec) command(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	switch name {
	case "wtype":
		if f.wtypeFail {
			return exec.Command("false")
		}
		cmd := exec.Command("sh", "-c", "cat")
		f.wtypeBuf = &bytes.Buffer{}
		cmd.Stdout = f.wtypeBuf
		return cmd
	case "ydotool":
		cmd := exec.Command("sh", "-c", "cat")
		f.ydotoolBuf = &bytes.Buffer{}
		cmd.Stdout = f.ydotoolBuf
		return cmd
	}
	return exec.Command("false")
}

func (f *fakeWaylandExec) commandContext(_ context.Context, name string, args ...string) *exec.Cmd {
	return f.command(name, args...)
}

// fakeFileInfo is the minimal os.FileInfo needed by OSStat fakes.
type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

func TestWaylandPrefersWtype(t *testing.T) {
	fe := &fakeWaylandExec{}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		LookPath: func(name string) (string, error) {
			if name == "wtype" {
				return "/usr/bin/wtype", nil
			}
			return "", os.ErrNotExist
		},
		EnvGet: func(string) string { return "" },
		OSStat: func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}
	s := newWaylandStrategy(deps)
	tgt := yinject.Target{DisplayServer: "wayland", AppType: yinject.AppGeneric}
	if err := s.Deliver(context.Background(), tgt, "hello"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if fe.wtypeBuf == nil || fe.wtypeBuf.String() != "hello" {
		t.Errorf("wtype stdin = %q, want hello", fe.wtypeBuf)
	}
	if len(fe.calls) != 1 || fe.calls[0].name != "wtype" {
		t.Errorf("expected single wtype call, got %+v", fe.calls)
	}
}

func TestWaylandFallsBackToYdotoolWhenWtypeMissing(t *testing.T) {
	fe := &fakeWaylandExec{}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		LookPath: func(name string) (string, error) {
			if name == "ydotool" {
				return "/usr/bin/ydotool", nil
			}
			return "", os.ErrNotExist
		},
		EnvGet: func(string) string { return "" },
		OSStat: func(name string) (os.FileInfo, error) {
			if name == "/tmp/.ydotool_socket" {
				return fakeFileInfo{name: name}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	s := newWaylandStrategy(deps)
	tgt := yinject.Target{DisplayServer: "wayland", AppType: yinject.AppGeneric}
	if err := s.Deliver(context.Background(), tgt, "hi there"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if fe.ydotoolBuf == nil || fe.ydotoolBuf.String() != "hi there" {
		t.Errorf("ydotool stdin = %q, want \"hi there\"", fe.ydotoolBuf)
	}
}

func TestWaylandReturnsUnsupportedWhenNeitherToolPresent(t *testing.T) {
	deps := Deps{
		ExecCommandContext: func(context.Context, string, ...string) *exec.Cmd { return exec.Command("false") },
		LookPath:           func(string) (string, error) { return "", os.ErrNotExist },
		OSStat:             func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		EnvGet:             func(string) string { return "" },
	}
	s := newWaylandStrategy(deps)
	err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "wayland"}, "x")
	if !errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Errorf("err = %v, want ErrStrategyUnsupported", err)
	}
}

func TestWaylandUsesEnvSocketPath(t *testing.T) {
	deps := Deps{
		LookPath: func(name string) (string, error) {
			if name == "ydotool" {
				return "/usr/bin/ydotool", nil
			}
			return "", os.ErrNotExist
		},
		EnvGet: func(k string) string {
			if k == "YDOTOOL_SOCKET" {
				return "/run/user/1000/ydotool.sock"
			}
			return ""
		},
		OSStat: func(name string) (os.FileInfo, error) {
			if name == "/run/user/1000/ydotool.sock" {
				return fakeFileInfo{name: name}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	s := newWaylandStrategy(deps)
	if !s.canUseYdotool() {
		t.Error("canUseYdotool should respect YDOTOOL_SOCKET")
	}
}

func TestWaylandSupportsOnlyWayland(t *testing.T) {
	s := newWaylandStrategy(Deps{})
	if !s.Supports(yinject.Target{DisplayServer: "wayland"}) {
		t.Error("Supports must be true on Wayland")
	}
	if s.Supports(yinject.Target{DisplayServer: "x11"}) {
		t.Error("Supports must be false on X11")
	}
}

func TestWaylandPropagatesWtypeFailure(t *testing.T) {
	fe := &fakeWaylandExec{wtypeFail: true}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		LookPath: func(name string) (string, error) {
			if name == "wtype" {
				return "/usr/bin/wtype", nil
			}
			return "", os.ErrNotExist
		},
	}
	s := newWaylandStrategy(deps)
	err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "wayland"}, "x")
	if err == nil {
		t.Fatal("expected wtype failure to surface")
	}
	if errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Error("wtype failure should not surface as ErrStrategyUnsupported")
	}
}
