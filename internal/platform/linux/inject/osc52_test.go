package inject

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// fakeWriteCloser captures bytes written by a strategy through
// Deps.OSOpenFile.
type fakeWriteCloser struct {
	buf    bytes.Buffer
	closed bool
}

func (f *fakeWriteCloser) Write(p []byte) (int, error) {
	return f.buf.Write(p)
}

func (f *fakeWriteCloser) Close() error {
	f.closed = true
	return nil
}

// fakeDirEntry is the minimal os.DirEntry impl needed for /proc/<pid>/task
// fakes.
type fakeDirEntry struct {
	name string
	dir  bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("not implemented") }

// fakeProcDeps assembles a Deps with /proc and /dev/pts fakes set up
// to model a single terminal emulator pid 100 whose only descendant
// is shell pid 200, with stdin pointing at /dev/pts/7.
func fakeProcDeps(t *testing.T, opened *fakeWriteCloser, openTarget *string) Deps {
	t.Helper()
	readlinkOK := map[string]string{
		"/proc/200/fd/0": "/dev/pts/7",
	}
	readDir := func(name string) ([]os.DirEntry, error) {
		switch name {
		case "/proc/100/task":
			return []os.DirEntry{fakeDirEntry{name: "100", dir: true}}, nil
		case "/proc/200/task":
			return []os.DirEntry{fakeDirEntry{name: "200", dir: true}}, nil
		}
		return nil, errors.New("unknown task dir: " + name)
	}
	readFile := func(name string) ([]byte, error) {
		switch name {
		case "/proc/100/task/100/children":
			return []byte("200\n"), nil
		case "/proc/200/task/200/children":
			return []byte(""), nil
		}
		return nil, errors.New("unknown children file: " + name)
	}
	openFile := func(name string, flag int, perm os.FileMode) (io.WriteCloser, error) {
		if openTarget != nil {
			*openTarget = name
		}
		return opened, nil
	}
	return Deps{
		OSReadlink: func(p string) (string, error) {
			if v, ok := readlinkOK[p]; ok {
				return v, nil
			}
			return "", os.ErrNotExist
		},
		OSReadDir:  readDir,
		OSReadFile: readFile,
		OSOpenFile: openFile,
		EnvGet:     func(string) string { return "" },
		Sleep:      func(time.Duration) {},
		Now:        time.Now,
	}
}

func TestOSC52DeliverWritesEscapeSequence(t *testing.T) {
	wc := &fakeWriteCloser{}
	var openTarget string
	deps := fakeProcDeps(t, wc, &openTarget)
	opts := platform.InjectionOptions{PreferOSC52: true, BracketedPaste: true}
	s := newOSC52Strategy(deps, opts)

	target := yinject.Target{
		DisplayServer: "wayland",
		AppType:       yinject.AppTerminal,
		WindowID:      "100",
		AppClass:      "kitty",
	}
	if err := s.Deliver(context.Background(), target, "hello"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if openTarget != "/dev/pts/7" {
		t.Errorf("opened %q, want /dev/pts/7", openTarget)
	}
	if !wc.closed {
		t.Error("writer should be closed after Deliver")
	}
	written := wc.buf.String()
	if !strings.HasPrefix(written, "\x1b]52;c;") {
		t.Errorf("missing OSC52 prefix: %q", written)
	}
	if !strings.HasSuffix(written, "\x07") {
		t.Errorf("missing BEL terminator: %q", written)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(written, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("payload not base64: %v", err)
	}
	if string(decoded) != "hello" {
		t.Errorf("decoded = %q, want hello", string(decoded))
	}
}

func TestOSC52WrapsBracketedForMultilineWhenEnabled(t *testing.T) {
	wc := &fakeWriteCloser{}
	deps := fakeProcDeps(t, wc, nil)
	s := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: true, BracketedPaste: true})

	tgt := yinject.Target{AppType: yinject.AppTerminal, WindowID: "100"}
	if err := s.Deliver(context.Background(), tgt, "line1\nline2"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(wc.buf.String(), "\x1b]52;c;"), "\x07")
	decoded, _ := base64.StdEncoding.DecodeString(body)
	if !strings.HasPrefix(string(decoded), "\x1b[200~") || !strings.HasSuffix(string(decoded), "\x1b[201~") {
		t.Errorf("multiline payload missing bracketed wrap: %q", string(decoded))
	}
}

func TestOSC52DoesNotWrapBracketedWhenDisabled(t *testing.T) {
	wc := &fakeWriteCloser{}
	deps := fakeProcDeps(t, wc, nil)
	s := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: true, BracketedPaste: false})

	tgt := yinject.Target{AppType: yinject.AppTerminal, WindowID: "100"}
	if err := s.Deliver(context.Background(), tgt, "line1\nline2"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(wc.buf.String(), "\x1b]52;c;"), "\x07")
	decoded, _ := base64.StdEncoding.DecodeString(body)
	if strings.Contains(string(decoded), "\x1b[200~") {
		t.Errorf("unexpected bracketed wrap when disabled: %q", string(decoded))
	}
}

func TestOSC52SupportsRespectsPreferOSC52(t *testing.T) {
	deps := Deps{}
	disabled := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: false})
	if disabled.Supports(yinject.Target{AppType: yinject.AppTerminal}) {
		t.Error("Supports must be false when PreferOSC52 is off")
	}
	enabled := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: true})
	if !enabled.Supports(yinject.Target{AppType: yinject.AppTerminal}) {
		t.Error("Supports must be true for AppTerminal when PreferOSC52 is on")
	}
	if enabled.Supports(yinject.Target{AppType: yinject.AppGeneric}) {
		t.Error("Supports must be false for non-terminal targets")
	}
}

func TestOSC52ReturnsUnsupportedWhenWindowIDMissing(t *testing.T) {
	s := newOSC52Strategy(Deps{}, platform.InjectionOptions{PreferOSC52: true})
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal}, "x")
	if !errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Errorf("err = %v, want ErrStrategyUnsupported", err)
	}
}

func TestOSC52ReturnsUnsupportedWhenWindowIDNotNumeric(t *testing.T) {
	s := newOSC52Strategy(Deps{}, platform.InjectionOptions{PreferOSC52: true})
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, WindowID: "abc"}, "x")
	if !errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Errorf("err = %v, want ErrStrategyUnsupported", err)
	}
}

func TestOSC52ReturnsUnsupportedWhenProcUnreadable(t *testing.T) {
	deps := Deps{
		OSReadlink: func(string) (string, error) { return "", os.ErrNotExist },
		OSReadDir:  func(string) ([]os.DirEntry, error) { return nil, os.ErrPermission },
	}
	s := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: true})
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, WindowID: "100"}, "x")
	if !errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Errorf("err = %v, want ErrStrategyUnsupported", err)
	}
}

func TestOSC52ResolvesViaShellChildEvenWhenTerminalHasNoPTS(t *testing.T) {
	// Models a terminal emulator (pid 100) with stdin pointing at
	// /dev/null and a single shell child (pid 300) whose stdin is
	// /dev/pts/9. The strategy must descend through the children to
	// find the pts.
	wc := &fakeWriteCloser{}
	var openTarget string
	deps := Deps{
		OSReadlink: func(p string) (string, error) {
			switch p {
			case "/proc/100/fd/0", "/proc/100/fd/1", "/proc/100/fd/2":
				return "/dev/null", nil
			case "/proc/300/fd/0":
				return "/dev/pts/9", nil
			}
			return "", os.ErrNotExist
		},
		OSReadDir: func(name string) ([]os.DirEntry, error) {
			switch name {
			case "/proc/100/task":
				return []os.DirEntry{fakeDirEntry{name: "100", dir: true}}, nil
			case "/proc/300/task":
				return []os.DirEntry{fakeDirEntry{name: "300", dir: true}}, nil
			}
			return nil, errors.New("unknown")
		},
		OSReadFile: func(name string) ([]byte, error) {
			switch name {
			case "/proc/100/task/100/children":
				return []byte("300\n"), nil
			case "/proc/300/task/300/children":
				return []byte(""), nil
			}
			return nil, errors.New("unknown")
		},
		OSOpenFile: func(name string, _ int, _ os.FileMode) (io.WriteCloser, error) {
			openTarget = name
			return wc, nil
		},
	}
	s := newOSC52Strategy(deps, platform.InjectionOptions{PreferOSC52: true})
	if err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, WindowID: "100"}, "ok"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if openTarget != "/dev/pts/9" {
		t.Errorf("opened %q, want /dev/pts/9", openTarget)
	}
}
