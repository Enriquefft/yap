package transform_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
)

type fakeTransformer struct{}

func (fakeTransformer) Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk, _ transform.Options) (<-chan transcribe.TranscriptChunk, error) {
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

// TestRegistry_NonCheckerBackendWorks asserts the C19 fix: a backend
// that does NOT implement Checker is still usable end-to-end via
// the registry. The fakeTransformer in this file deliberately omits
// HealthCheck; the test confirms callers that type-assert on
// Checker get ok=false (so they can skip the probe) and the
// returned Transformer still emits chunks.
func TestRegistry_NonCheckerBackendWorks(t *testing.T) {
	name := uniqueName(t, "nochecker")
	transform.Register(name, fakeFactory)

	factory, err := transform.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	tr, err := factory(transform.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// The Checker assertion must fail cleanly — fakeTransformer
	// does not implement HealthCheck.
	if _, ok := tr.(transform.Checker); ok {
		t.Fatal("fakeTransformer must NOT implement transform.Checker; this test depends on that")
	}

	// The Transformer must still satisfy the basic Transform
	// contract: pass an empty input channel through to a closed
	// output channel.
	in := make(chan transcribe.TranscriptChunk)
	close(in)
	out, err := tr.Transform(context.Background(), in, transform.Options{})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	for range out {
	}
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
