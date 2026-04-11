package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// seedDefaultConfig writes a fresh default config to cfgFile so Set
// runs against a known starting point.
func seedDefaultConfig(t *testing.T, cfgFile string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(pcfg.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
}

func readPersistedConfig(t *testing.T, cfgFile string) pcfg.Config {
	t.Helper()
	var c pcfg.Config
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.NewDecoder(strings.NewReader(string(data))).Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigSet_PersistsNestedValue(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "transform.enabled", "true")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}

	got := readPersistedConfig(t, cfgFile)
	if !got.Transform.Enabled {
		t.Error("transform.enabled was not persisted")
	}
}

func TestConfigSet_ValidatesBeforeSave(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// Set an invalid mode; validator must reject and not write.
	_, _, err := runCLI(t, "config", "set", "general.mode", "always-on")
	if err == nil {
		t.Fatal("expected validation error")
	}

	// File still contains the original default.
	got := readPersistedConfig(t, cfgFile)
	if got.General.Mode != "hold" {
		t.Errorf("mode clobbered: got %q, want hold", got.General.Mode)
	}
}

func TestConfigSet_RejectsBadBool(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "transform.enabled", "maybe")
	if err == nil {
		t.Fatal("expected bool parse error")
	}
}

func TestConfigSet_RejectsBadInt(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "general.max_duration", "forever")
	if err == nil {
		t.Fatal("expected integer parse error")
	}
}

func TestConfigSet_RejectsOutOfRangeInt(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// 0 and 301 are both out of validator range [1,300].
	for _, v := range []string{"0", "301"} {
		if _, _, err := runCLI(t, "config", "set", "general.max_duration", v); err == nil {
			t.Errorf("max_duration=%s should fail validation", v)
		}
	}
}

func TestConfigSet_RoundTripString(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// The default whisperlocal model is "base.en" which is
	// English-only; the validator correctly rejects setting
	// language=ja while the model stays .en. Switch to a
	// multilingual model first so the round-trip exercises the
	// set/get path rather than the validator.
	if _, _, err := runCLI(t, "config", "set", "transcription.model", "base"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if _, _, err := runCLI(t, "config", "set", "transcription.language", "ja"); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := runCLI(t, "config", "get", "transcription.language")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(stdout) != "ja" {
		t.Errorf("round-trip: got %q, want ja", stdout)
	}
}
