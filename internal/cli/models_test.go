package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/internal/cli"
	linux "github.com/Enriquefft/yap/internal/platform/linux"
	"github.com/Enriquefft/yap/pkg/yap/transcribe/whisperlocal/models"
)

// withTempCache redirects XDG_CACHE_HOME and any unrelated env vars
// the CLI may consult so the models commands run in a scratch
// environment.
func withTempCache(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	// Make sure the wizard is never invoked by other CLI tests
	// running in parallel by stubbing the YAP_CONFIG to a fresh path.
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	return cache
}

// runCLIWithModelMgr runs the cobra command tree with a fixture
// models.Manager injected. Tests use this to exercise the
// `yap models` subtree against an httptest fixture instead of the
// real Hugging Face URLs.
func runCLIWithModelMgr(t *testing.T, mgr *models.Manager, argv ...string) (string, string, error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	err := cli.ExecuteForTestWithDeps(linux.NewPlatform(), mgr, argv, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), err
}

// discardWriter is the no-op writer used by tests that do not care
// about the captured stdout/stderr — handy when only the error is
// being asserted.
var _ io.Writer = (*bytes.Buffer)(nil)

func TestModelsList_Empty(t *testing.T) {
	withTempCache(t)
	stdout, _, err := runCLI(t, "models", "list")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	if !strings.Contains(stdout, "base.en") {
		t.Errorf("expected base.en in output, got:\n%s", stdout)
	}
	// The status column must read "missing" for every model in a
	// fresh cache. "missing" is the post-L6 wording that replaces
	// the old INSTALLED=no column and distinguishes absent files
	// from corrupt ones in the same table.
	if !strings.Contains(stdout, "missing") {
		t.Errorf("expected status=missing in fresh cache, got:\n%s", stdout)
	}
	// A fresh cache must never produce a corrupt footer.
	if strings.Contains(stdout, "corrupt cache files detected") {
		t.Errorf("fresh cache flagged as corrupt:\n%s", stdout)
	}
}

func TestModelsList_Installed(t *testing.T) {
	cache := withTempCache(t)
	// Seed the cache with a fake base.en file so the listing flips
	// to "installed". The seed must start with the ggml magic prefix
	// "lmgg" (little-endian GGML_FILE_MAGIC) because models.Installed
	// validates the magic bytes as a guard against garbage files
	// reporting as ready — exactly the bug 3 fix in the models package.
	dir := filepath.Join(cache, "yap", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ggml-base.en.bin"), []byte("lmgg seeded fixture"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, _, err := runCLI(t, "models", "list")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	if !strings.Contains(stdout, "installed") {
		t.Errorf("expected status=installed after seeding, got:\n%s", stdout)
	}
}

// TestModelsList_CorruptFlagged is the L6 regression: a cached file
// that exists but fails ggml magic-byte verification must appear in
// `yap models list` with STATUS=corrupt AND the footer must list an
// `rm <path>` hint so the user can clear the bad file manually. The
// old behavior silently reported "not installed" and left the user
// chasing a mysterious download failure when the cache was a
// read-only Nix store link.
func TestModelsList_CorruptFlagged(t *testing.T) {
	cache := withTempCache(t)
	dir := filepath.Join(cache, "yap", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Exactly 5 bytes, no ggml magic — the failure mode from the
	// original bug report. VerifyGGMLMagic rejects it, so the CLI
	// must surface it as corrupt rather than missing.
	corruptPath := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(corruptPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	stdout, _, err := runCLI(t, "models", "list")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	if !strings.Contains(stdout, "corrupt") {
		t.Errorf("expected status=corrupt in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "corrupt cache files detected") {
		t.Errorf("expected corrupt footer in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "rm "+corruptPath) {
		t.Errorf("expected actionable rm hint for %s, got:\n%s", corruptPath, stdout)
	}
	// base.en must NOT be shown as installed. The user's recovery
	// path is `rm`, not `yap whisper run`.
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "base.en") && strings.Contains(line, "installed") {
			t.Errorf("corrupt file rendered as installed: %q", line)
		}
	}
}

func TestModelsPath_CacheDir(t *testing.T) {
	cache := withTempCache(t)
	stdout, _, err := runCLI(t, "models", "path")
	if err != nil {
		t.Fatalf("models path: %v", err)
	}
	want := filepath.Join(cache, "yap", "models")
	if strings.TrimSpace(stdout) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(stdout), want)
	}
}

func TestModelsPath_NamedModel(t *testing.T) {
	cache := withTempCache(t)
	stdout, _, err := runCLI(t, "models", "path", "base.en")
	if err != nil {
		t.Fatalf("models path base.en: %v", err)
	}
	want := filepath.Join(cache, "yap", "models", "ggml-base.en.bin")
	if strings.TrimSpace(stdout) != want {
		t.Errorf("got %q, want %q", strings.TrimSpace(stdout), want)
	}
}

func TestModelsPath_UnknownModel(t *testing.T) {
	withTempCache(t)
	_, _, err := runCLI(t, "models", "path", "potato.en")
	if err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestModelsDownload_Success(t *testing.T) {
	withTempCache(t)

	// Stand up an httptest server with a known payload + hash. The
	// fixture Manager is wired to that server's URL via WithManifest
	// + WithHTTPClient — no package-level mutation, no globals.
	payload := []byte("hello cli download")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	mgr := models.NewManager(
		models.WithHTTPClient(srv.Client()),
		models.WithManifest([]models.Manifest{
			{
				Name:   "test.en",
				URL:    srv.URL + "/ggml-test.en.bin",
				SHA256: hash,
				SizeMB: 1,
			},
		}),
	)

	stdout, _, err := runCLIWithModelMgr(t, mgr, "models", "download", "test.en")
	if err != nil {
		t.Fatalf("models download: %v", err)
	}
	if !strings.Contains(stdout, "installed test.en") {
		t.Errorf("expected install confirmation, got:\n%s", stdout)
	}

	p, err := mgr.Path("test.en")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("model file missing after download: %v", err)
	}
}

func TestModelsDownload_RejectsBadHash(t *testing.T) {
	withTempCache(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("the wrong bytes"))
	}))
	defer srv.Close()

	mgr := models.NewManager(
		models.WithHTTPClient(srv.Client()),
		models.WithManifest([]models.Manifest{
			{
				Name:   "test.en",
				URL:    srv.URL + "/x",
				SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
				SizeMB: 1,
			},
		}),
	)

	_, _, err := runCLIWithModelMgr(t, mgr, "models", "download", "test.en")
	if err == nil {
		t.Fatal("expected sha mismatch error")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected SHA256 mismatch in error, got %q", err.Error())
	}
}
