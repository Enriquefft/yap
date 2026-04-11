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

	"github.com/adrg/xdg"
)

// withTempCache redirects XDG_CACHE_HOME to a fresh temp directory and
// returns its path. Models tests must always run in a scratch cache so
// they cannot pollute the developer's real model cache (a fully-
// populated cache is ~2.1 GB across all four pinned English-only
// models).
//
// xdg.Reload is called immediately after t.Setenv because the
// adrg/xdg library caches resolved paths at init time: writing to
// XDG_CACHE_HOME has no effect until Reload re-reads the environment.
// Without the Reload any helper that bypasses CacheDir (which happens
// to Reload on every call) would resolve against the developer's
// real ~/.cache/yap/models and leak test fixtures into it — the
// exact failure mode that let a historical 5-byte ggml-base.en.bin
// sit in one user's cache and get reported as "installed".
//
// A t.Cleanup re-runs xdg.Reload so subsequent tests in the same
// process see the restored outer environment rather than the temp
// dir that t.Setenv rolled back.
//
// The Cleanup is registered BEFORE t.Setenv so that Go's LIFO cleanup
// ordering runs Setenv's env-restore first (reverting XDG_CACHE_HOME
// to the outer value) and our xdg.Reload second (re-reading the now-
// correct outer env). Registering them in the opposite order would
// run xdg.Reload while XDG_CACHE_HOME still pointed at the temp dir,
// caching the stale temp path in xdg's internal state for every
// subsequent test in the same binary — leaking fixtures across the
// process boundary and resolving to a deleted directory.
func withTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_CACHE_HOME", dir)
	xdg.Reload()
	return dir
}

// validGGMLBytes is the smallest payload that passes VerifyGGMLMagic:
// the four-byte ggml magic prefix followed by filler. Tests that
// want a file Installed() should accept use this helper so the
// magic sequence is never hardcoded at the call site.
func validGGMLBytes() []byte {
	return []byte("lmgg-test-fixture")
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
	for _, name := range []string{"tiny", "tiny.en", "base", "base.en", "small", "small.en", "medium", "medium.en"} {
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
	// A real model starts with the ggml magic prefix; Installed()
	// validates the magic bytes so a plain "dummy" payload (which
	// the pre-fix version accepted) no longer passes. validGGMLBytes
	// centralises the magic so the single source of truth lives in
	// the package that owns VerifyGGMLMagic.
	if err := os.WriteFile(p, validGGMLBytes(), 0o600); err != nil {
		t.Fatalf("write valid model fixture: %v", err)
	}
	got, err := mgr.Installed("base.en")
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if !got {
		t.Fatal("Installed reported false for present file")
	}
}

// TestInstalled_RejectsDummyFile is the regression for the
// reported bug: a 5-byte junk file named ggml-base.en.bin (the
// exact size of the historical "dummy" fixture that leaked into
// a developer's cache) must not be reported as installed.
// Installed() now calls VerifyGGMLMagic and surfaces a failing
// file as "not installed" so `yap models list` stays consistent
// with the resolveModel check that refuses to load it at record
// time.
func TestInstalled_RejectsDummyFile(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	p, err := mgr.Path("base.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// Exactly the 5-byte payload from the pre-fix test, which is
	// the same failure mode the user reported from their real
	// cache.
	if err := os.WriteFile(p, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	got, err := mgr.Installed("base.en")
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	if got {
		t.Fatal("Installed reported true for a 5-byte junk file; should fail magic-byte verification")
	}
}

// TestList_RejectsDummyFile asserts List() agrees with Installed()
// about what counts as a real model: a junk file in the cache
// must appear with Installed=false in `yap models list` so the
// CLI output never contradicts the record-time resolveModel
// check.
func TestList_RejectsDummyFile(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	p, err := mgr.Path("base.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(p, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	got, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range got {
		if m.Name == "base.en" && m.Installed {
			t.Fatal("List reported base.en installed for a 5-byte junk file; should fail magic-byte verification")
		}
	}
}

// TestList_CorruptFlagged is the L6 regression: a cache file that
// exists but fails VerifyGGMLMagic must surface on List() with
// Corrupt=true so the CLI renderer can tell the user their cache
// has a bad file that needs removing. Before L6, such a file was
// silently reported as "not installed", identical to a missing
// file, which misled users with read-only caches (e.g. a Nix
// store symlink) into thinking a redownload would fix the
// problem.
//
// A corrupt Model must also have Installed=false because the two
// flags are mutually exclusive: "ready to use" and "present-but-
// broken" are different user-facing states and collapsing them
// would defeat the whole point of L6.
func TestList_CorruptFlagged(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	p, err := mgr.Path("base.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.WriteFile(p, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write dummy: %v", err)
	}
	got, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var found *Model
	for i := range got {
		if got[i].Name == "base.en" {
			found = &got[i]
			break
		}
	}
	if found == nil {
		t.Fatal("base.en missing from List output")
	}
	if found.Installed {
		t.Error("corrupt file reported Installed=true; should be false")
	}
	if !found.Corrupt {
		t.Error("corrupt file reported Corrupt=false; should be true")
	}
	if found.Path != p {
		t.Errorf("Model.Path = %q, want %q", found.Path, p)
	}
	// Other pinned models (e.g. base, tiny) must remain in the
	// default missing state — the corrupt flag applies only to
	// the seeded file.
	for _, m := range got {
		if m.Name == "base.en" {
			continue
		}
		if m.Corrupt {
			t.Errorf("unrelated model %q flagged Corrupt=true", m.Name)
		}
	}
}

// TestList_MissingVsCorruptDistinct pins the semantic that the
// three dispositions (installed, corrupt, missing) are mutually
// exclusive. A fresh cache must report every model as
// !Installed && !Corrupt so the CLI renderer's "missing" fallback
// is the only thing the user sees for absent files.
func TestList_MissingVsCorruptDistinct(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	got, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, m := range got {
		if m.Installed {
			t.Errorf("fresh cache reported %q Installed=true", m.Name)
		}
		if m.Corrupt {
			t.Errorf("fresh cache reported %q Corrupt=true", m.Name)
		}
	}
}

// TestWithTempCache_LIFOCleanupOrder is the M1 regression. It
// verifies that withTempCache's cleanup sequence leaves xdg's
// resolved-path cache in sync with the real XDG_CACHE_HOME env
// var after the helper's Cleanup runs. The bug was that
// registering t.Cleanup(xdg.Reload) AFTER t.Setenv made LIFO
// run our Reload first (caching the still-present temp dir)
// before Setenv's env-restore reverted XDG_CACHE_HOME.
//
// The test exploits t.Run's subtests, which each get their own
// cleanup stack that is torn down before the next subtest runs.
// After the first subtest finishes, the parent's env var is
// restored to whatever it was before the helper installed the
// temp dir; xdg.CacheFile must resolve against that outer value
// — NOT the deleted temp directory — on the next call.
func TestWithTempCache_LIFOCleanupOrder(t *testing.T) {
	// Pin an outer XDG_CACHE_HOME so we have a deterministic
	// value to compare against after the inner subtest tears
	// down. t.Setenv restores this on test exit.
	outer := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", outer)
	xdg.Reload()

	var innerDir string
	t.Run("inner-using-withTempCache", func(t *testing.T) {
		innerDir = withTempCache(t)
		// Sanity: inside the subtest xdg must resolve to the
		// temp dir the helper installed.
		p, err := xdg.CacheFile("yap/models/.keep")
		if err != nil {
			t.Fatalf("xdg.CacheFile inside withTempCache: %v", err)
		}
		if !strings.HasPrefix(p, innerDir) {
			t.Errorf("inside withTempCache xdg resolved to %q, want prefix %q",
				p, innerDir)
		}
	})

	// After the subtest's cleanup stack has unwound, xdg must be
	// back in sync with the outer env. If the LIFO ordering is
	// wrong, xdg's internal cache will still point at innerDir
	// (now deleted) and CacheFile will resolve against a stale
	// directory — the exact regression this test guards against.
	p, err := xdg.CacheFile("yap/models/.keep")
	if err != nil {
		t.Fatalf("xdg.CacheFile after withTempCache cleanup: %v", err)
	}
	if !strings.HasPrefix(p, outer) {
		t.Errorf("after cleanup xdg resolved to %q, want prefix %q (outer); "+
			"withTempCache leaked the inner temp dir into xdg's cache",
			p, outer)
	}
	if strings.HasPrefix(p, innerDir) {
		t.Errorf("after cleanup xdg still points at deleted inner dir %q: %q",
			innerDir, p)
	}
}

func TestList_PinnedEnglishModels(t *testing.T) {
	withTempCache(t)
	mgr := NewManager()
	got, err := mgr.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// The manifest pins both English-only and multilingual models.
	// If you add or remove a model from the manifest, update this
	// expected list — it is the canonical assertion of which models
	// a fresh `yap models list` will surface.
	wantNames := []string{
		"tiny", "tiny.en",
		"base", "base.en",
		"small", "small.en",
		"medium", "medium.en",
	}
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
