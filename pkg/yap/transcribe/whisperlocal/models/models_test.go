package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// withTempCache redirects XDG_CACHE_HOME to a fresh temp directory and
// returns its path. Models tests must always run in a scratch cache so
// they cannot pollute the developer's real model cache (a fully-
// populated cache is ~2.1 GB across all four pinned English-only
// models).
func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	return dir
}

// newFixtureManager constructs a Manager wired to an httptest server
// and a one-entry fixture manifest. The server serves payload, and
// the manifest's SHA256 matches the payload so a Download succeeds.
// Tests pass a different hash to exercise the rejection path.
func newFixtureManager(t *testing.T, payload []byte, hash string) (*Manager, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	mgr := NewManager(
		WithHTTPClient(server.Client()),
		WithManifest([]Manifest{
			{
				Name:   "test.en",
				URL:    server.URL + "/ggml-test.en.bin",
				SHA256: hash,
				SizeMB: 1,
			},
		}),
	)
	return mgr, server.URL
}

// hashOf returns the lowercase hex SHA256 of b.
func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestCacheDir_CreatesDirectory(t *testing.T) {
	withTempCache(t)
	dir, err := CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
	// Idempotent: a second call must not fail.
	if _, err := CacheDir(); err != nil {
		t.Fatalf("second CacheDir: %v", err)
	}
}

func TestPath_UnknownModel(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	if _, err := mgr.Path("nope.en"); err == nil {
		t.Fatal("Path(\"nope.en\") returned nil error")
	}
}

func TestErrUnknownModel_ListsPinnedModels(t *testing.T) {
	err := ErrUnknownModel("large-v3")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown model") {
		t.Errorf("expected message to start with 'unknown model', got %q", msg)
	}
	// Every pinned model name must appear in the error so the user can
	// see exactly what they can pick from without consulting docs.
	for _, name := range []string{"tiny.en", "base.en", "small.en", "medium.en"} {
		if !strings.Contains(msg, name) {
			t.Errorf("expected message to mention %q, got %q", name, msg)
		}
	}
	// The hint about transcription.model_path is the documented
	// escape hatch for models outside the manifest.
	if !strings.Contains(msg, "transcription.model_path") {
		t.Errorf("expected message to point at transcription.model_path, got %q", msg)
	}
}

func TestInstalled_MissingFile(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	got, err := mgr.Installed("base.en")
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if got {
		t.Fatal("Installed reported true for missing file")
	}
}

func TestInstalled_PresentFile(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	p, err := mgr.Path("base.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(p, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	got, err := mgr.Installed("base.en")
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if !got {
		t.Fatal("Installed reported false for present file")
	}
}

func TestList_PinnedEnglishModels(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	got, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// The manifest pins exactly the four English-only whisper.cpp
	// models. If you add or remove a model from the manifest, update
	// this expected list — it is the canonical assertion of which
	// models a fresh `yap models list` will surface.
	wantNames := []string{"tiny.en", "base.en", "small.en", "medium.en"}
	if len(got) != len(wantNames) {
		t.Fatalf("expected %d pinned models, got %d: %+v",
			len(wantNames), len(got), got)
	}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Errorf("model[%d]: want name %q, got %q", i, want, got[i].Name)
		}
		if got[i].Installed {
			t.Errorf("model[%d] (%s): should not be installed in fresh cache",
				i, got[i].Name)
		}
		if got[i].SHA256 == "" {
			t.Errorf("model[%d] (%s): SHA256 is empty", i, got[i].Name)
		}
		if got[i].SizeMB <= 0 {
			t.Errorf("model[%d] (%s): SizeMB must be positive, got %d",
				i, got[i].Name, got[i].SizeMB)
		}
	}
}

func TestKnown_ReturnsCopy(t *testing.T) {
	a := Known()
	b := Known()
	if len(a) != len(b) {
		t.Fatalf("Known length mismatch: %d vs %d", len(a), len(b))
	}
	if len(a) > 0 {
		// Mutating the returned slice must not affect the package
		// state — Known returns a fresh copy.
		a[0].SHA256 = "tampered"
		c := Known()
		if c[0].SHA256 == "tampered" {
			t.Fatal("Known returned a shared slice; callers can corrupt package state")
		}
	}
}

func TestLookupByName_CaseInsensitive(t *testing.T) {
	// Mixed case must resolve to the canonical lowercase entry. The
	// case normalization is the single source of truth in
	// lookupManifestIn.
	for _, input := range []string{"base.en", "Base.EN", "BASE.EN", "Base.en"} {
		m, ok := LookupByName(input)
		if !ok {
			t.Errorf("LookupByName(%q) = !ok, want resolved", input)
			continue
		}
		if m.Name != "base.en" {
			t.Errorf("LookupByName(%q) resolved to %q, want %q", input, m.Name, "base.en")
		}
	}
}

func TestDownload_Success(t *testing.T) {
	withTempCache(t)
	payload := []byte("hello whisper test fixture")
	mgr, _ := newFixtureManager(t, payload, hashOf(payload))

	if err := mgr.Download(context.Background(), "test.en", nil); err != nil {
		t.Fatalf("Download: %v", err)
	}

	p, err := mgr.Path("test.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("file contents mismatch:\ngot:  %q\nwant: %q", got, payload)
	}
}

func TestDownload_WrongSHARejected(t *testing.T) {
	withTempCache(t)
	payload := []byte("body the server actually returns")
	// Pin a hash that does NOT match payload.
	mgr, _ := newFixtureManager(t, payload, hashOf([]byte("totally different")))

	err := mgr.Download(context.Background(), "test.en", nil)
	if err == nil {
		t.Fatal("Download succeeded with wrong hash")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected SHA256 mismatch error, got %q", err.Error())
	}

	// The cache must be unchanged: no file at the final path, no
	// leftover temp file in the cache directory.
	p, err := mgr.Path("test.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("model file exists after rejected download: %v", statErr)
	}
	dir := filepath.Dir(p)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ggml-test.en.bin.") {
			t.Errorf("temp file leaked into cache after rejection: %s", e.Name())
		}
	}
}

func TestDownload_HTTPErrorPropagates(t *testing.T) {
	withTempCache(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	mgr := NewManager(
		WithHTTPClient(server.Client()),
		WithManifest([]Manifest{{
			Name:   "test.en",
			URL:    server.URL + "/x",
			SHA256: "ignored",
			SizeMB: 1,
		}}),
	)

	err := mgr.Download(context.Background(), "test.en", nil)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected HTTP 500 in error, got %q", err.Error())
	}
}

func TestDownload_ContextCancelled(t *testing.T) {
	withTempCache(t)
	payload := []byte("doesn't matter")
	mgr, _ := newFixtureManager(t, payload, hashOf(payload))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call so the request never starts.

	err := mgr.Download(ctx, "test.en", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// No leftover model file.
	p, _ := mgr.Path("test.en")
	if _, statErr := os.Stat(p); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("model file exists after cancelled download: %v", statErr)
	}
}

// TestDownload_ConcurrentSerializedByFlock fires two Download calls
// against the same Manager with the same upstream URL and asserts the
// upstream server received exactly one request — the inter-process
// advisory lock serialised them, and the second call observed the
// finished file and skipped the network round-trip.
func TestDownload_ConcurrentSerializedByFlock(t *testing.T) {
	withTempCache(t)
	payload := []byte("payload that must download exactly once")
	hash := hashOf(payload)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(server.Close)

	mgr := NewManager(
		WithHTTPClient(server.Client()),
		WithManifest([]Manifest{{
			Name:   "test.en",
			URL:    server.URL + "/once",
			SHA256: hash,
			SizeMB: 1,
		}}),
	)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- mgr.Download(context.Background(), "test.en", nil)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Download: %v", err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("upstream hits = %d, want 1 (the lock should serialize and the second call should skip)", got)
	}
}

func TestProgressWriter_NilSinkIsSilent(t *testing.T) {
	pw := newProgressWriter(nil, "x", 100)
	if _, err := pw.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	pw.finish(5)
}

func TestProgressWriter_EmitsLines(t *testing.T) {
	var sink strings.Builder
	pw := newProgressWriter(&sink, "x", 100)
	if _, err := pw.Write(make([]byte, 50)); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write(make([]byte, 50)); err != nil {
		t.Fatal(err)
	}
	pw.finish(100)
	out := sink.String()
	if !strings.Contains(out, "50%") || !strings.Contains(out, "100%") {
		t.Fatalf("expected 50%% and 100%% lines, got:\n%s", out)
	}
}

// sanity-check the io.Writer interface so the test does not silently
// pass when the writer is replaced with a nil-safe stub.
var _ io.Writer = (*progressWriter)(nil)
