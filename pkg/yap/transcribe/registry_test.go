package transcribe_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// fakeBackend is the simplest possible Transcriber used to exercise
// the registry. It does not touch the network and produces no output.
type fakeBackend struct{}

func (fakeBackend) Transcribe(ctx context.Context, audio io.Reader) (<-chan transcribe.TranscriptChunk, error) {
	ch := make(chan transcribe.TranscriptChunk)
	close(ch)
	return ch, nil
}

func fakeFactory(cfg transcribe.Config) (transcribe.Transcriber, error) {
	return fakeBackend{}, nil
}

// uniqueName returns a backend name that is extremely unlikely to
// collide with real backends. Each test allocates its own name so
// registrations never conflict across the package test binary.
func uniqueName(t *testing.T, suffix string) string {
	t.Helper()
	return "registry_test_" + t.Name() + "_" + suffix
}

func TestRegisterAndGet(t *testing.T) {
	name := uniqueName(t, "ok")
	transcribe.Register(name, fakeFactory)

	got, err := transcribe.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	if got == nil {
		t.Fatalf("Get(%q) returned nil factory", name)
	}

	tr, err := got(transcribe.Config{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, ok := tr.(fakeBackend); !ok {
		t.Errorf("factory returned %T, want fakeBackend", tr)
	}
}

func TestGetUnknownBackend(t *testing.T) {
	_, err := transcribe.Get("definitely-not-registered-" + t.Name())
	if err == nil {
		t.Fatal("Get should return an error for unknown backend")
	}
	if !errors.Is(err, transcribe.ErrUnknownBackend) {
		t.Errorf("error %v does not wrap ErrUnknownBackend", err)
	}
	// The error message should list currently registered backends so
	// users know what is available.
	if !strings.Contains(err.Error(), "registered:") {
		t.Errorf("error %q should list registered backends", err.Error())
	}
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with empty name should panic")
		}
	}()
	transcribe.Register("", fakeFactory)
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register with nil factory should panic")
		}
	}()
	transcribe.Register(uniqueName(t, "nilf"), nil)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	name := uniqueName(t, "dup")
	transcribe.Register(name, fakeFactory)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Register should panic on duplicate name")
		}
	}()
	transcribe.Register(name, fakeFactory)
}

func TestBackendsReturnsSorted(t *testing.T) {
	// Register a few entries and confirm Backends is sorted and
	// includes each of them. Other tests (and side-effect imports
	// from sub-packages) may also populate the registry, so we
	// assert containment + sortedness rather than exact equality.
	names := []string{
		uniqueName(t, "c"),
		uniqueName(t, "a"),
		uniqueName(t, "b"),
	}
	for _, n := range names {
		transcribe.Register(n, fakeFactory)
	}

	got := transcribe.Backends()
	if !sort.StringsAreSorted(got) {
		t.Errorf("Backends() not sorted: %v", got)
	}

	have := map[string]bool{}
	for _, n := range got {
		have[n] = true
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("Backends() missing %q: %v", n, got)
		}
	}
}

func TestBackendsSnapshotIsCopy(t *testing.T) {
	// Mutating the slice returned by Backends() must not affect
	// subsequent calls — the registry must return a fresh copy.
	name := uniqueName(t, "snap")
	transcribe.Register(name, fakeFactory)

	first := transcribe.Backends()
	if len(first) == 0 {
		t.Fatal("Backends() returned empty slice after Register")
	}
	orig := make([]string, len(first))
	copy(orig, first)

	for i := range first {
		first[i] = ""
	}
	second := transcribe.Backends()
	if !reflect.DeepEqual(orig, second) {
		t.Errorf("second Backends() call returned %v, want %v", second, orig)
	}
}
