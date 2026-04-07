package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hybridz/yap/internal/config"
)

func TestConfigPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("YAP_CONFIG", "")

	path, err := config.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath() error: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("ConfigPath() returned relative path: %s", path)
	}
	if filepath.Base(path) != "config.toml" {
		t.Errorf("ConfigPath() base: got %s, want config.toml", filepath.Base(path))
	}
}

func TestConfigPath_YAPConfigOverride(t *testing.T) {
	tmp := t.TempDir()
	override := filepath.Join(tmp, "custom.toml")
	t.Setenv("YAP_CONFIG", override)

	got, err := config.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath error: %v", err)
	}
	if got != override {
		t.Errorf("YAP_CONFIG override: got %s, want %s", got, override)
	}
}

func TestMissingConfigUsesDefaults(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("YAP_CONFIG", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned error for missing config: %v", err)
	}
	if cfg.General.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("default Hotkey: got %q, want KEY_RIGHTCTRL", cfg.General.Hotkey)
	}
	if cfg.Transcription.Language != "en" {
		t.Errorf("default Language: got %q, want en", cfg.Transcription.Language)
	}
	if cfg.General.MaxDuration != 60 {
		t.Errorf("default MaxDuration: got %d, want 60", cfg.General.MaxDuration)
	}
}

func TestConfigLoad_Nested(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	content := `
[general]
  hotkey = "KEY_F2"
  audio_device = "default"
  max_duration = 30

[transcription]
  backend = "groq"
  language = "fr"
  api_key = "sk-test-123"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Transcription.APIKey != "sk-test-123" {
		t.Errorf("APIKey: got %q, want sk-test-123", cfg.Transcription.APIKey)
	}
	if cfg.General.Hotkey != "KEY_F2" {
		t.Errorf("Hotkey: got %q, want KEY_F2", cfg.General.Hotkey)
	}
	if cfg.Transcription.Language != "fr" {
		t.Errorf("Language: got %q, want fr", cfg.Transcription.Language)
	}
	if cfg.General.AudioDevice != "default" {
		t.Errorf("AudioDevice: got %q, want default", cfg.General.AudioDevice)
	}
	if cfg.General.MaxDuration != 30 {
		t.Errorf("MaxDuration: got %d, want 30", cfg.General.MaxDuration)
	}
}

func TestNonGlobal(t *testing.T) {
	// Two Load() calls return independent structs.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("YAP_CONFIG", "")

	cfg1, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg1.Transcription.APIKey = "mutated"

	cfg2, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Transcription.APIKey == "mutated" {
		t.Error("Load() shares state between calls — must return independent structs")
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.General.Hotkey = "KEY_SPACE"
	cfg.Transform.Enabled = true
	cfg.Transform.Backend = "local"
	cfg.Transform.Model = "llama3.2:3b"

	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.General.Hotkey != "KEY_SPACE" {
		t.Errorf("Hotkey after round-trip: got %q, want KEY_SPACE", reloaded.General.Hotkey)
	}
	if !reloaded.Transform.Enabled {
		t.Error("Transform.Enabled not persisted")
	}
	if reloaded.Transform.Model != "llama3.2:3b" {
		t.Errorf("Transform.Model not persisted: got %q", reloaded.Transform.Model)
	}
}
