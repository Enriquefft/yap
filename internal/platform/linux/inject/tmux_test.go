package inject

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	yinject "github.com/hybridz/yap/pkg/yap/inject"
)

// fakeTmuxExec snapshots the call sequence and the stdin payload for
// load-buffer. The load-buffer command is wired to `sh -c "cat"` so
// that the strategy's Stdin assignment flows through to the captured
// buffer; paste-buffer is wired to `true`.
type fakeTmuxExec struct {
	calls    []fakeCall
	loadBuf  *bytes.Buffer
	loadErr  bool
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

func (f *fakeTmuxExec) commandContext(_ context.Context, name string, args ...string) *exec.Cmd {
	return f.command(name, args...)
}

func TestTmuxDeliverRunsLoadAndPasteBuffer(t *testing.T) {
	fe := &fakeTmuxExec{}
	deps := Deps{ExecCommandContext: fe.commandContext}
	s := newTmuxStrategy(deps)

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
	if !equalStrings(fe.calls[0].args, []string{"load-buffer", "-"}) {
		t.Errorf("load-buffer args = %v, want [load-buffer -]", fe.calls[0].args)
	}
	if !equalStrings(fe.calls[1].args, []string{"paste-buffer", "-p"}) {
		t.Errorf("paste-buffer args = %v, want [paste-buffer -p]", fe.calls[1].args)
	}
}

// TestTmuxStrategy_Deliver_MultiLine guards F2: a multi-line payload
// must NOT be wrapped in bracketed-paste markers by yap. The tmux
// `paste-buffer -p` flag tells tmux to wrap the paste itself; doing
// it client-side would double-wrap and corrupt the shell input.
func TestTmuxStrategy_Deliver_MultiLine(t *testing.T) {
	fe := &fakeTmuxExec{}
	deps := Deps{ExecCommandContext: fe.commandContext}
	s := newTmuxStrategy(deps)

	tgt := yinject.Target{AppType: yinject.AppTerminal, Tmux: true}
	if err := s.Deliver(context.Background(), tgt, "line1\nline2\nline3"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	stdin := fe.loadBuf.String()
	if stdin != "line1\nline2\nline3" {
		t.Errorf("load-buffer stdin = %q, want raw multi-line text", stdin)
	}
	if strings.Contains(stdin, "\x1b[200~") {
		t.Errorf("load-buffer stdin contains bracketed-paste start marker — yap must not wrap, tmux paste-buffer -p does it natively: %q", stdin)
	}
	if strings.Contains(stdin, "\x1b[201~") {
		t.Errorf("load-buffer stdin contains bracketed-paste end marker — yap must not wrap, tmux paste-buffer -p does it natively: %q", stdin)
	}
	// Confirm the paste call carries the -p flag.
	var pasteArgs []string
	for _, c := range fe.calls {
		if len(c.args) > 0 && c.args[0] == "paste-buffer" {
			pasteArgs = c.args
		}
	}
	if !equalStrings(pasteArgs, []string{"paste-buffer", "-p"}) {
		t.Errorf("paste-buffer args = %v, want [paste-buffer -p]", pasteArgs)
	}
}

func TestTmuxDeliverPropagatesLoadBufferFailure(t *testing.T) {
	fe := &fakeTmuxExec{loadErr: true}
	deps := Deps{ExecCommandContext: fe.commandContext}
	s := newTmuxStrategy(deps)
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
	deps := Deps{ExecCommandContext: fe.commandContext}
	s := newTmuxStrategy(deps)
	err := s.Deliver(context.Background(), yinject.Target{AppType: yinject.AppTerminal, Tmux: true}, "x")
	if err == nil || !strings.Contains(err.Error(), "paste-buffer") {
		t.Errorf("err = %v, want paste-buffer error", err)
	}
}

func TestTmuxSupportsRequiresTerminalAndTmuxBit(t *testing.T) {
	s := newTmuxStrategy(Deps{})
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
