package inject

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// fakeTmuxExec snapshots the call sequence and the stdin payload for
// load-buffer. The load-buffer command is wired to `sh -c "cat"` so
// that the strategy's Stdin assignment flows through to the captured
// buffer; paste-buffer is wired to `true`.
type fakeTmuxExec struct {
	calls   []fakeCall
	loadBuf *bytes.Buffer
	loadErr bool
	pasteErr bool
}

func (f *fakeTmuxExec) command(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	if name != "tmux" {
		return exec.Command("false")
	}
	if len(args) > 0 && args[0] == "load-buffer" {
		if f.loadErr {
			return exec.Command("false")
		}
		cmd := exec.Command("sh", "-c", "cat")
		f.loadBuf = &bytes.Buffer{}
		cmd.Stdout = f.loadBuf
		return cmd
	}
	if len(args) > 0 && args[0] == "paste-buffer" {
		if f.pasteErr {
			return exec.Command("false")
		}
		return exec.Command("true")
	}
	return exec.Command("false")
}

func TestTmuxDeliverRunsLoadAndPaste(t *testing.T) {
	fe := &fakeTmuxExec{}
	deps := Deps{ExecCommand: fe.command}
	s := newTmuxStrategy(deps, platform.InjectionOptions{BracketedPaste: false})

	tgt := yinject.Target{AppType: yinject.AppTerminal, Tmux: true}
	if err := s.Deliver(context.Background(), tgt, "echo hi"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if fe.loadBuf == nil || fe.loadBuf.String() != "echo hi" {
		t.Errorf("captured stdin = %q, want %q", fe.loadBuf, "echo hi")
	}
	if len(fe.calls) != 2 {
		t.Fatalf("expected 2 tmux calls, got %d", len(fe.calls))
	}
	if fe.calls[0].args[0] != "load-buffer" || fe.calls[1].args[0] != "paste-buffer" {
		t.Errorf("call sequence = %+v", fe.calls)
	}
}

func TestTmuxDeliverWrapsBracketedForMultiline(t *testing.T) {
	fe := &fakeTmuxExec{}
	deps := Deps{ExecCommand: fe.command}
	s := newTmuxStrategy(deps, platform.InjectionOptions{BracketedPaste: true})

	tgt := yinject.Target{AppType: yinject.AppTerminal, Tmux: true}
	if err := s.Deliver(context.Background(), tgt, "line1\nline2"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	out := fe.loadBuf.String()
	if !strings.HasPrefix(out, "\x1b[200~") || !strings.HasSuffix(out, "\x1b[201~") {
		t.Errorf("multiline payload not bracketed: %q", out)
	}
}

func TestTmuxDeliverDoesNotWrapBracketedWhenDisabled(t *testing.T) {
	fe := &fakeTmuxExec{}
	deps := Deps{ExecCommand: fe.command}
	s := newTmuxStrategy(deps, platform.InjectionOptions{BracketedPaste: false})
	if err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, Tmux: true}, "a\nb"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if strings.Contains(fe.loadBuf.String(), "\x1b[200~") {
		t.Errorf("unexpected bracketed wrap: %q", fe.loadBuf.String())
	}
}

func TestTmuxDeliverPropagatesLoadBufferFailure(t *testing.T) {
	fe := &fakeTmuxExec{loadErr: true}
	deps := Deps{ExecCommand: fe.command}
	s := newTmuxStrategy(deps, platform.InjectionOptions{})
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, Tmux: true}, "x")
	if err == nil || !strings.Contains(err.Error(), "load-buffer") {
		t.Errorf("err = %v, want load-buffer error", err)
	}
	if errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Error("real failure must not surface as ErrStrategyUnsupported")
	}
}

func TestTmuxDeliverPropagatesPasteBufferFailure(t *testing.T) {
	fe := &fakeTmuxExec{pasteErr: true}
	deps := Deps{ExecCommand: fe.command}
	s := newTmuxStrategy(deps, platform.InjectionOptions{})
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, Tmux: true}, "x")
	if err == nil || !strings.Contains(err.Error(), "paste-buffer") {
		t.Errorf("err = %v, want paste-buffer error", err)
	}
}

func TestTmuxSupportsRequiresTerminalAndTmuxBit(t *testing.T) {
	s := newTmuxStrategy(Deps{}, platform.InjectionOptions{})
	if !s.Supports(yinject.Target{AppType: yinject.AppTerminal, Tmux: true}) {
		t.Error("Supports must be true for terminal+tmux")
	}
	if s.Supports(yinject.Target{AppType: yinject.AppTerminal}) {
		t.Error("Supports must be false without tmux bit")
	}
	if s.Supports(yinject.Target{AppType: yinject.AppGeneric, Tmux: true}) {
		t.Error("Supports must be false for non-terminal targets")
	}
}
