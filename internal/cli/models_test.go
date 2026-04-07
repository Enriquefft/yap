package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal/models"
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

func TestModelsList_Empty(t *testing.T) {
	withTempCache(t)
	stdout, _, err := runCLI(t, "models", "list")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	if !strings.Contains(stdout, "base.en") {
		t.Errorf("expected base.en in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "no") {
		t.Errorf("expected installed=no in fresh cache, got:\n%s", stdout)
	}
}

func TestModelsList_Installed(t *testing.T) {
	cache := withTempCache(t)
	// Seed the cache with a fake base.en file so the listing flips
	// to "yes".
	dir := filepath.Join(cache, "yap", "models")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ggml-base.en.bin"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, _, err := runCLI(t, "models", "list")
	if err != nil {
		t.Fatalf("models list: %v", err)
	}
	if !strings.Contains(stdout, "yes") {
		t.Errorf("expected installed=yes after seeding, got:\n%s", stdout)
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

	// Stand up an httptest server with a known payload + hash. We
	// swap the package's manifest at runtime via the test helper so
	// the CLI command resolves to our fake URL.
	payload := []byte("hello cli download")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	// Substitute manifest + http client for the duration of the test.
	prevClient := models.SetDownloadClientForTest(srv.Client())
	t.Cleanup(func() { models.SetDownloadClientForTest(prevClient) })
	restoreManifest := models.OverrideManifestForTest([]models.Manifest{
		{
			Name:   "test.en",
			URL:    srv.URL + "/ggml-test.en.bin",
			SHA256: hash,
			SizeMB: 1,
		},
	})
	t.Cleanup(restoreManifest)

	stdout, _, err := runCLI(t, "models", "download", "test.en")
	if err != nil {
		t.Fatalf("models download: %v", err)
	}
	if !strings.Contains(stdout, "installed test.en") {
		t.Errorf("expected install confirmation, got:\n%s", stdout)
	}

	p, err := models.Path("test.en")
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

	prevClient := models.SetDownloadClientForTest(srv.Client())
	t.Cleanup(func() { models.SetDownloadClientForTest(prevClient) })
	restoreManifest := models.OverrideManifestForTest([]models.Manifest{
		{
			Name:   "test.en",
			URL:    srv.URL + "/x",
			SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
			SizeMB: 1,
		},
	})
	t.Cleanup(restoreManifest)

	_, _, err := runCLI(t, "models", "download", "test.en")
	if err == nil {
		t.Fatal("expected sha mismatch error")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Errorf("expected SHA256 mismatch in error, got %q", err.Error())
	}
}
