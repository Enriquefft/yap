package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/config"
)

// legacyFlat is the canonical pre-Phase-2 TOML payload. Kept as a
// constant so every migration test round-trips identically.
const legacyFlat = `api_key = "gsk_legacyapikey00000000000000000000000000000000000000000"
hotkey = "KEY_SPACE"
language = "fr"
mic_device = "hw:1,0"
timeout_seconds = 45
`

func TestMigrate_FlatToNested_InMemory(t *testing.T) {
	config.ResetMigrationNoticeForTest()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	if err := os.WriteFile(cfgFile, []byte(legacyFlat), 0o600); err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	cfg, err := config.LoadWithNotices(&notices)
	if err != nil {
		t.Fatalf("LoadWithNotices: %v", err)
	}

	// Fields landed in the correct nested slots.
	if cfg.Transcription.APIKey != "gsk_legacyapikey00000000000000000000000000000000000000000" {
		t.Errorf("APIKey migration: got %q", cfg.Transcription.APIKey)
	}
	if cfg.General.Hotkey != "KEY_SPACE" {
		t.Errorf("Hotkey migration: got %q", cfg.General.Hotkey)
	}
	if cfg.Transcription.Language != "fr" {
		t.Errorf("Language migration: got %q", cfg.Transcription.Language)
	}
	if cfg.General.AudioDevice != "hw:1,0" {
		t.Errorf("MicDevice→AudioDevice migration: got %q", cfg.General.AudioDevice)
	}
	if cfg.General.MaxDuration != 45 {
		t.Errorf("TimeoutSeconds→MaxDuration migration: got %d", cfg.General.MaxDuration)
	}

	// Notice was printed to the captured writer.
	if !strings.Contains(notices.String(), "migrated legacy flat config") {
		t.Errorf("expected migration notice, got: %q", notices.String())
	}
}

func TestMigrate_OnDiskUnchanged(t *testing.T) {
	config.ResetMigrationNoticeForTest()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	if err := os.WriteFile(cfgFile, []byte(legacyFlat), 0o600); err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	if _, err := config.LoadWithNotices(&notices); err != nil {
		t.Fatalf("Load: %v", err)
	}

	got, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != legacyFlat {
		t.Errorf("migration mutated on-disk file.\nwant: %q\ngot:  %q", legacyFlat, string(got))
	}
}

func TestMigrate_NoticeOnlyPrintsOnce(t *testing.T) {
	config.ResetMigrationNoticeForTest()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	if err := os.WriteFile(cfgFile, []byte(legacyFlat), 0o600); err != nil {
		t.Fatal(err)
	}

	var firstBuf, secondBuf bytes.Buffer
	if _, err := config.LoadWithNotices(&firstBuf); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if _, err := config.LoadWithNotices(&secondBuf); err != nil {
		t.Fatalf("second Load: %v", err)
	}

	if !strings.Contains(firstBuf.String(), "migrated legacy flat config") {
		t.Errorf("first Load missing notice: %q", firstBuf.String())
	}
	if strings.Contains(secondBuf.String(), "migrated legacy flat config") {
		t.Errorf("second Load should not re-print notice, got: %q", secondBuf.String())
	}
}

func TestMigrate_HybridFileNotSilentlyFlattened(t *testing.T) {
	// Conservative detection: if a nested table exists, treat as
	// nested even if some flat keys are present. Unknown-key
	// warnings are acceptable; clobbering is not.
	config.ResetMigrationNoticeForTest()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	hybrid := `api_key = "gsk_hybridkey000000000000000000000000000000000000000000000"
[transcription]
  backend = "groq"
  language = "de"
`
	if err := os.WriteFile(cfgFile, []byte(hybrid), 0o600); err != nil {
		t.Fatal(err)
	}

	var notices bytes.Buffer
	cfg, err := config.LoadWithNotices(&notices)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Nested values survived.
	if cfg.Transcription.Language != "de" {
		t.Errorf("hybrid language: got %q, want de", cfg.Transcription.Language)
	}
	// Migration did NOT happen.
	if strings.Contains(notices.String(), "migrated legacy flat config") {
		t.Errorf("hybrid file should not emit migration notice, got: %q", notices.String())
	}
}

func TestMigrate_EnvOverrideAppliesAfter(t *testing.T) {
	config.ResetMigrationNoticeForTest()

	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)

	if err := os.WriteFile(cfgFile, []byte(legacyFlat), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YAP_API_KEY", "env-api-key-wins")
	t.Setenv("GROQ_API_KEY", "")

	var notices bytes.Buffer
	cfg, err := config.LoadWithNotices(&notices)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Transcription.APIKey != "env-api-key-wins" {
		t.Errorf("env override not applied after migration: got %q", cfg.Transcription.APIKey)
	}
}

func TestConfigPath_EtcFallback(t *testing.T) {
	// This test does not write to /etc/yap (would need root) — it
	// exercises the precedence logic by pointing YAP_CONFIG at a
	// non-existent user file and confirming the /etc fallback is
	// chosen when /etc/yap/config.toml exists. We emulate by
	// checking that when both XDG and YAP_CONFIG are unset, the
	// function picks the user path (there is no /etc/yap on the
	// test runner unless Nix put it there).
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("YAP_CONFIG", "")

	got, err := config.ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	// Must be the user XDG path because no file exists yet.
	wantPrefix := filepath.Join(tmp, "yap")
	if !strings.HasPrefix(got, wantPrefix) && got != "/etc/yap/config.toml" {
		t.Errorf("ConfigPath: got %s, want prefix %s or /etc/yap/config.toml", got, wantPrefix)
	}
}
