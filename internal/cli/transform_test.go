package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func withPassthroughTransform(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	writeConfigFile(t, cfgFile, `
[transform]
  enabled = true
  backend = "passthrough"
`)
}

func TestTransform_PositionalArg(t *testing.T) {
	withPassthroughTransform(t)
	stdout, _, err := runCLI(t, "transform", "hello world")
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected echoed text, got %q", stdout)
	}
}

func TestTransform_DisabledBackendStillRuns(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	// Transform.Enabled defaults to false; the command must still
	// echo the text via the passthrough fallback.
	writeConfigFile(t, cfgFile, `[transform]
  enabled = false
  backend = "passthrough"
`)
	stdout, _, err := runCLI(t, "transform", "untouched text")
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if !strings.Contains(stdout, "untouched text") {
		t.Errorf("expected echoed text, got %q", stdout)
	}
}

func TestTransform_BackendOverride(t *testing.T) {
	withPassthroughTransform(t)
	stdout, _, err := runCLI(t, "transform", "--backend", "passthrough", "abc")
	if err != nil {
		t.Fatalf("transform --backend: %v", err)
	}
	if !strings.Contains(stdout, "abc") {
		t.Errorf("expected echoed text, got %q", stdout)
	}
}

func TestTransform_UnknownBackend(t *testing.T) {
	withPassthroughTransform(t)
	_, _, err := runCLI(t, "transform", "--backend", "no-such-backend", "x")
	if err == nil {
		t.Fatal("expected unknown backend error")
	}
	if !strings.Contains(err.Error(), "no-such-backend") {
		t.Errorf("error did not name the unknown backend: %v", err)
	}
}
