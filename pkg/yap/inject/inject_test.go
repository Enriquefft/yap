package inject_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// fakeStrategy is a trivial Strategy impl used only to prove the
// Strategy interface is satisfiable from outside the package.
type fakeStrategy struct {
	name    string
	support bool
	err     error
	called  int
}

func (f *fakeStrategy) Name() string                            { return f.name }
func (f *fakeStrategy) Supports(t inject.Target) bool           { return f.support }
func (f *fakeStrategy) Deliver(ctx context.Context, s string) error {
	f.called++
	return f.err
}

// fakeInjector is a trivial Injector impl used to prove the Injector
// interface is satisfiable from outside the package.
type fakeInjector struct {
	last     string
	streamed []transcribe.TranscriptChunk
	err      error
}

func (f *fakeInjector) Inject(ctx context.Context, text string) error {
	if f.err != nil {
		return f.err
	}
	f.last = text
	return nil
}

func (f *fakeInjector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	for c := range in {
		f.streamed = append(f.streamed, c)
	}
	return f.err
}

func TestStrategyInterfaceSatisfied(t *testing.T) {
	var _ inject.Strategy = (*fakeStrategy)(nil)
	s := &fakeStrategy{name: "fake", support: true}
	if !s.Supports(inject.Target{DisplayServer: "wayland"}) {
		t.Error("Supports should return true")
	}
	if err := s.Deliver(context.Background(), "hi"); err != nil {
		t.Errorf("Deliver: %v", err)
	}
	if s.called != 1 {
		t.Errorf("called = %d, want 1", s.called)
	}
}

func TestInjectorInterfaceSatisfied(t *testing.T) {
	var _ inject.Injector = (*fakeInjector)(nil)
	f := &fakeInjector{}
	if err := f.Inject(context.Background(), "hello"); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if f.last != "hello" {
		t.Errorf("last = %q, want hello", f.last)
	}

	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: "x", IsFinal: true}
	close(in)
	if err := f.InjectStream(context.Background(), in); err != nil {
		t.Fatalf("InjectStream: %v", err)
	}
	if len(f.streamed) != 1 {
		t.Errorf("streamed = %d, want 1", len(f.streamed))
	}
}

func TestInjectorErrorPropagates(t *testing.T) {
	f := &fakeInjector{err: errors.New("boom")}
	if err := f.Inject(context.Background(), "x"); err == nil {
		t.Error("expected error")
	}
}

func TestTargetZeroValue(t *testing.T) {
	// Zero value of Target should be harmless to construct. The
	// Supports selection logic in Phase 4 is responsible for
	// rejecting incomplete targets.
	var tgt inject.Target
	if tgt.AppType != inject.AppGeneric {
		t.Errorf("zero AppType = %d, want AppGeneric(%d)", tgt.AppType, inject.AppGeneric)
	}
}

func TestAppTypeConstantsDistinct(t *testing.T) {
	seen := map[inject.AppType]string{}
	for name, at := range map[string]inject.AppType{
		"AppGeneric":   inject.AppGeneric,
		"AppTerminal":  inject.AppTerminal,
		"AppElectron":  inject.AppElectron,
		"AppBrowser":   inject.AppBrowser,
		"AppTmux":      inject.AppTmux,
		"AppSSHRemote": inject.AppSSHRemote,
	} {
		if prev, ok := seen[at]; ok {
			t.Errorf("%s shares value with %s", name, prev)
		}
		seen[at] = name
	}
}
