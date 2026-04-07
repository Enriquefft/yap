package transform_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transform"
)

type fakeTransformer struct{}

func (fakeTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error) {
	out := make(chan transcribe.TranscriptChunk)
	close(out)
	return out, nil
}

func fakeFactory(cfg transform.Config) (transform.Transformer, error) {
	return fakeTransformer{}, nil
}

func uniqueName(t *testing.T, suffix string) string {
	t.Helper()
	return "transform_test_" + t.Name() + "_" + suffix
}

func TestRegisterAndGet(t *testing.T) {
	name := uniqueName(t, "ok")
	transform.Register(name, fakeFactory)

	got, err := transform.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	if got == nil {
		t.Fatal("Get returned nil factory")
	}

	tr, err := got(transform.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, ok := tr.(fakeTransformer); !ok {
		t.Errorf("factory returned %T, want fakeTransformer", tr)
	}
}

func TestGetUnknownBackend(t *testing.T) {
	_, err := transform.Get("does-not-exist-" + t.Name())
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
	if !errors.Is(err, transform.ErrUnknownBackend) {
		t.Errorf("error %v does not wrap ErrUnknownBackend", err)
	}
	if !strings.Contains(err.Error(), "registered:") {
		t.Errorf("error %q should list registered backends", err.Error())
	}
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	transform.Register("", fakeFactory)
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	transform.Register(uniqueName(t, "nilf"), nil)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := uniqueName(t, "dup")
	transform.Register(name, fakeFactory)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	transform.Register(name, fakeFactory)
}

func TestBackendsSorted(t *testing.T) {
	names := []string{
		uniqueName(t, "c"),
		uniqueName(t, "a"),
		uniqueName(t, "b"),
	}
	for _, n := range names {
		transform.Register(n, fakeFactory)
	}
	got := transform.Backends()
	if !sort.StringsAreSorted(got) {
		t.Errorf("Backends() not sorted: %v", got)
	}
	have := map[string]bool{}
	for _, n := range got {
		have[n] = true
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("Backends() missing %q", n)
		}
	}
}
