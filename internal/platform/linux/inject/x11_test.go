package inject

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	yinject "github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeX11Exec records every xdotool call and lets the test script the
// sequence of getactivewindow outputs and the type-command result.
type fakeX11Exec struct {
	calls       []fakeCall
	winSequence []string
	winIdx      int
	typeFail    bool
}

func (f *fakeX11Exec) command(name string, args ...string) *exec.Cmd {
	f.calls = append(f.calls, fakeCall{name: name, args: append([]string(nil), args...)})
	if name != "xdotool" {
		return exec.Command("false")
	}
	if len(args) > 0 && args[0] == "getactivewindow" {
		var out string
		if f.winIdx < len(f.winSequence) {
			out = f.winSequence[f.winIdx]
			f.winIdx++
		} else if len(f.winSequence) > 0 {
			out = f.winSequence[len(f.winSequence)-1]
		}
		return exec.Command("printf", "%s", out)
	}
	if len(args) > 0 && args[0] == "type" {
		if f.typeFail {
			return exec.Command("false")
		}
		return exec.Command("true")
	}
	return exec.Command("false")
}

func (f *fakeX11Exec) commandContext(_ context.Context, name string, args ...string) *exec.Cmd {
	return f.command(name, args...)
}

func TestX11DeliverIssuesXdotoolType(t *testing.T) {
	fe := &fakeX11Exec{winSequence: []string{"123", "123"}}
	sleeps := 0
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		SleepCtx:           func(context.Context, time.Duration) error { sleeps++; return nil },
	}
	s := newX11Strategy(deps)
	tgt := yinject.Target{DisplayServer: "x11", AppType: yinject.AppGeneric}
	if err := s.Deliver(context.Background(), tgt, "hello"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	// Find the type call.
	var typed *fakeCall
	for i := range fe.calls {
		if len(fe.calls[i].args) > 0 && fe.calls[i].args[0] == "type" {
			typed = &fe.calls[i]
			break
		}
	}
	if typed == nil {
		t.Fatalf("expected xdotool type call, got %+v", fe.calls)
	}
	wantArgs := []string{"type", "--clearmodifiers", "--", "hello"}
	if !equalStrings(typed.args, wantArgs) {
		t.Errorf("type args = %v, want %v", typed.args, wantArgs)
	}
}

func TestX11FocusPollStopsOnStableImmediately(t *testing.T) {
	// Two consecutive identical samples → loop exits after one
	// Sleep call.
	fe := &fakeX11Exec{winSequence: []string{"42", "42"}}
	sleeps := 0
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		SleepCtx:           func(context.Context, time.Duration) error { sleeps++; return nil },
	}
	s := newX11Strategy(deps)
	if err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "x11"}, "x"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if sleeps != 1 {
		t.Errorf("sleeps = %d, want 1", sleeps)
	}
}

func TestX11FocusPollGivesUpAfterMaxAttempts(t *testing.T) {
	// Always-different sequence forces the loop to run to its cap.
	winSequence := []string{}
	for i := 0; i <= focusPollMaxAttempts+1; i++ {
		winSequence = append(winSequence, string(rune('a'+i)))
	}
	fe := &fakeX11Exec{winSequence: winSequence}
	sleeps := 0
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		SleepCtx:           func(context.Context, time.Duration) error { sleeps++; return nil },
	}
	s := newX11Strategy(deps)
	if err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "x11"}, "x"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if sleeps != focusPollMaxAttempts {
		t.Errorf("sleeps = %d, want %d", sleeps, focusPollMaxAttempts)
	}
}

func TestX11FocusPollSkipsWhenInitialQueryFails(t *testing.T) {
	// xdotool getactivewindow returning empty → skip polling, proceed.
	fe := &fakeX11Exec{winSequence: []string{""}}
	sleeps := 0
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		SleepCtx:           func(context.Context, time.Duration) error { sleeps++; return nil },
	}
	s := newX11Strategy(deps)
	if err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "x11"}, "x"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if sleeps != 0 {
		t.Errorf("sleeps = %d, want 0 when initial focus query fails", sleeps)
	}
}

func TestX11PropagatesXdotoolTypeFailure(t *testing.T) {
	fe := &fakeX11Exec{winSequence: []string{"1", "1"}, typeFail: true}
	deps := Deps{
		ExecCommandContext: fe.commandContext,
		SleepCtx:           func(context.Context, time.Duration) error { return nil },
	}
	s := newX11Strategy(deps)
	err := s.Deliver(context.Background(), yinject.Target{DisplayServer: "x11"}, "x")
	if err == nil {
		t.Fatal("expected xdotool failure to surface")
	}
	if errors.Is(err, yinject.ErrStrategyUnsupported) {
		t.Error("real failure must not surface as ErrStrategyUnsupported")
	}
}

func TestX11SupportsOnlyX11(t *testing.T) {
	s := newX11Strategy(Deps{})
	if !s.Supports(yinject.Target{DisplayServer: "x11"}) {
		t.Error("Supports must be true on X11")
	}
	if s.Supports(yinject.Target{DisplayServer: "wayland"}) {
		t.Error("Supports must be false on Wayland")
	}
}
