package hint_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// fakeProvider is a minimal Provider for registry round-trip tests.
type fakeProvider struct{ name string }

func (f fakeProvider) Name() string                                                  { return f.name }
func (f fakeProvider) Supports(_ inject.Target) bool                                 { return true }
func (f fakeProvider) Fetch(_ context.Context, _ inject.Target) (hint.Bundle, error) { return hint.Bundle{}, nil }

func fakeFactory(cfg hint.Config) (hint.Provider, error) {
	return fakeProvider{name: "fake"}, nil
}

func uniqueName(t *testing.T, suffix string) string {
	t.Helper()
	return "hint_test_" + t.Name() + "_" + suffix
}

func TestRegisterAndGet(t *testing.T) {
	name := uniqueName(t, "ok")
	hint.Register(name, fakeFactory)

	got, err := hint.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	if got == nil {
		t.Fatal("Get returned nil factory")
	}
	p, err := got(hint.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if p == nil {
		t.Fatal("factory returned nil provider")
	}
}

func TestGetUnknownProvider(t *testing.T) {
	_, err := hint.Get("does-not-exist-" + t.Name())
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !errors.Is(err, hint.ErrUnknownProvider) {
		t.Errorf("error %v does not wrap ErrUnknownProvider", err)
	}
	if !strings.Contains(err.Error(), "registered:") {
		t.Errorf("error %q should list registered providers", err.Error())
	}
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	hint.Register("", fakeFactory)
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	hint.Register(uniqueName(t, "nilf"), nil)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := uniqueName(t, "dup")
	hint.Register(name, fakeFactory)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	hint.Register(name, fakeFactory)
}

func TestProvidersSorted(t *testing.T) {
	names := []string{
		uniqueName(t, "c"),
		uniqueName(t, "a"),
		uniqueName(t, "b"),
	}
	for _, n := range names {
		hint.Register(n, fakeFactory)
	}
	got := hint.Providers()
	if !sort.StringsAreSorted(got) {
		t.Errorf("Providers() not sorted: %v", got)
	}
	have := map[string]bool{}
	for _, n := range got {
		have[n] = true
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("Providers() missing %q", n)
		}
	}
}
