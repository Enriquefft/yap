package config_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	// Set XDG_CONFIG_HOME to a temp dir to isolate from real user config.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	path, err := ConfigPathHelper(t)
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("ConfigPath() returned relative path: %s", path)
	}
	// Must be under our temp XDG_CONFIG_HOME
	rel, err := filepath.Rel(tmp, path)
	if err != nil || rel == ".." || len(rel) == 0 {
		t.Errorf("ConfigPath() %s not under XDG_CONFIG_HOME %s", path, tmp)
	}
	// Must end with yap/config.toml
	if filepath.Base(path) != "config.toml" {
		t.Errorf("ConfigPath() base: got %s, want config.toml", filepath.Base(path))
	}
}

func TestMissingConfigUsesDefaults(t *testing.T) {
	// Point XDG_CONFIG_HOME to empty temp dir so no config file exists.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg, err := LoadHelper(t)
	if err != nil {
		t.Fatalf("Load() returned error for missing config: %v", err)
	}
	if cfg.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("default Hotkey: got %q, want KEY_RIGHTCTRL", cfg.Hotkey)
	}
	if cfg.Language != "en" {
		t.Errorf("default Language: got %q, want en", cfg.Language)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("default TimeoutSeconds: got %d, want 60", cfg.TimeoutSeconds)
	}
}

func TestConfigLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Write a known config file to the XDG config path.
	cfgDir := filepath.Join(tmp, "yap")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "config.toml")
	content := `
api_key = "sk-test-123"
hotkey = "KEY_F2"
language = "fr"
mic_device = "default"
timeout_seconds = 30
`
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadHelper(t)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.APIKey != "sk-test-123" {
		t.Errorf("APIKey: got %q, want sk-test-123", cfg.APIKey)
	}
	if cfg.Hotkey != "KEY_F2" {
		t.Errorf("Hotkey: got %q, want KEY_F2", cfg.Hotkey)
	}
	if cfg.Language != "fr" {
		t.Errorf("Language: got %q, want fr", cfg.Language)
	}
	if cfg.MicDevice != "default" {
		t.Errorf("MicDevice: got %q, want default", cfg.MicDevice)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds: got %d, want 30", cfg.TimeoutSeconds)
	}
}

func TestConfigKeys(t *testing.T) {
	// Verify all 5 config keys are accessible via struct fields (compile-time check).
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg, err := LoadHelper(t)
	if err != nil {
		t.Fatal(err)
	}
	// All 5 keys must exist as addressable fields (CONFIG-03).
	_ = cfg.APIKey
	_ = cfg.Hotkey
	_ = cfg.Language
	_ = cfg.MicDevice
	_ = cfg.TimeoutSeconds
}

func TestEnvOverrides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Write config with different values.
	cfgDir := filepath.Join(tmp, "yap")
	if err := os.MkdirAll(cfgDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "config.toml")
	content := "api_key = \"from-toml\"\nhotkey = \"KEY_RIGHTCTRL\"\n"
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GROQ_API_KEY", "env-override-key")
	t.Setenv("YAP_HOTKEY", "KEY_F1")

	cfg, err := LoadHelper(t)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.APIKey != "env-override-key" {
		t.Errorf("GROQ_API_KEY override: got %q, want env-override-key", cfg.APIKey)
	}
	if cfg.Hotkey != "KEY_F1" {
		t.Errorf("YAP_HOTKEY override: got %q, want KEY_F1", cfg.Hotkey)
	}
}

func TestNonGlobal(t *testing.T) {
	// Two Load() calls must return independent structs with no shared state (CONFIG-05).
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg1, err := LoadHelper(t)
	if err != nil {
		t.Fatal(err)
	}
	cfg1.APIKey = "mutated"

	cfg2, err := LoadHelper(t)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.APIKey == "mutated" {
		t.Error("Load() shares state between calls — must return independent structs")
	}
}
