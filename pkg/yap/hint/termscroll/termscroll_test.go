package termscroll

import (
	"context"
	"errors"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeStrategy is a test double for Strategy.
type fakeStrategy struct {
	name     string
	supports bool
	text     string
	err      error
}

func (f *fakeStrategy) Name() string                          { return f.name }
func (f *fakeStrategy) Supports(_ inject.Target) bool         { return f.supports }
func (f *fakeStrategy) Read(_ context.Context) (string, error) { return f.text, f.err }

func TestProviderName(t *testing.T) {
	p := newProvider(nil)
	if p.Name() != "termscroll" {
		t.Errorf("Name() = %q, want %q", p.Name(), "termscroll")
	}
}

func TestProviderSupports(t *testing.T) {
	p := newProvider(nil)
	tests := []struct {
		name   string
		target inject.Target
		want   bool
	}{
		{"terminal", inject.Target{AppType: inject.AppTerminal}, true},
		{"browser", inject.Target{AppType: inject.AppBrowser}, false},
		{"electron", inject.Target{AppType: inject.AppElectron}, false},
		{"generic", inject.Target{AppType: inject.AppGeneric}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Supports(tt.target); got != tt.want {
				t.Errorf("Supports() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchFirstMatchWins(t *testing.T) {
	p := newProvider([]Strategy{
		&fakeStrategy{name: "first", supports: true, text: "first output"},
		&fakeStrategy{name: "second", supports: true, text: "second output"},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "first output" {
		t.Errorf("Conversation = %q, want %q", bundle.Conversation, "first output")
	}
	if bundle.Source != "termscroll" {
		t.Errorf("Source = %q, want %q", bundle.Source, "termscroll")
	}
}

func TestFetchSkipsUnsupported(t *testing.T) {
	p := newProvider([]Strategy{
		&fakeStrategy{name: "nope", supports: false, text: "should not appear"},
		&fakeStrategy{name: "yes", supports: true, text: "correct output"},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "correct output" {
		t.Errorf("Conversation = %q, want %q", bundle.Conversation, "correct output")
	}
}

func TestFetchSkipsErrors(t *testing.T) {
	p := newProvider([]Strategy{
		&fakeStrategy{name: "err", supports: true, err: errors.New("fail")},
		&fakeStrategy{name: "ok", supports: true, text: "fallback"},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "fallback" {
		t.Errorf("Conversation = %q, want %q", bundle.Conversation, "fallback")
	}
}

func TestFetchSkipsEmpty(t *testing.T) {
	p := newProvider([]Strategy{
		&fakeStrategy{name: "empty", supports: true, text: ""},
		&fakeStrategy{name: "notempty", supports: true, text: "got it"},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "got it" {
		t.Errorf("Conversation = %q, want %q", bundle.Conversation, "got it")
	}
}

func TestFetchAllFail(t *testing.T) {
	p := newProvider([]Strategy{
		&fakeStrategy{name: "err", supports: true, err: errors.New("fail")},
		&fakeStrategy{name: "empty", supports: true, text: ""},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "" {
		t.Errorf("expected empty conversation, got %q", bundle.Conversation)
	}
}

func TestFetchNoStrategies(t *testing.T) {
	p := newProvider(nil)
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "" {
		t.Errorf("expected empty conversation, got %q", bundle.Conversation)
	}
}

func TestFetchStripsANSI(t *testing.T) {
	// Strategy returns text with ANSI codes.
	ansiText := "hello \x1b[31mred\x1b[0m world"
	p := newProvider([]Strategy{
		&fakeStrategy{name: "ansi", supports: true, text: ansiText},
	})
	target := inject.Target{AppType: inject.AppTerminal}

	bundle, err := p.Fetch(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "hello red world" {
		t.Errorf("Conversation = %q, want %q", bundle.Conversation, "hello red world")
	}
}

func TestNewFactory(t *testing.T) {
	p, err := NewFactory(hint.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "termscroll" {
		t.Errorf("Name() = %q, want %q", p.Name(), "termscroll")
	}
}
