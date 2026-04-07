package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/hybridz/yap/pkg/yap/config"
)

func TestDefaultConfig_Deterministic(t *testing.T) {
	// Two independent calls return equal structs. The defaults are
	// a value type so there cannot be shared pointers, but the test
	// documents the contract.
	a := config.DefaultConfig()
	b := config.DefaultConfig()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("DefaultConfig() returned non-deterministic values:\n  a=%+v\n  b=%+v", a, b)
	}
}

func TestDefaultConfig_EveryFieldPopulated(t *testing.T) {
	// Every leaf field documented in ARCHITECTURE.md must have an
	// explicit value. Missing fields are a regression because they
	// cause the golden file to drift.
	cfg := config.DefaultConfig()
	if cfg.General.Hotkey == "" {
		t.Error("General.Hotkey not set by default")
	}
	if cfg.General.Mode == "" {
		t.Error("General.Mode not set by default")
	}
	if cfg.General.MaxDuration == 0 {
		t.Error("General.MaxDuration not set by default")
	}
	if cfg.General.SilenceDuration == 0 {
		t.Error("General.SilenceDuration not set by default")
	}
	if cfg.Transcription.Backend == "" {
		t.Error("Transcription.Backend not set by default")
	}
	if cfg.Transcription.Model == "" {
		t.Error("Transcription.Model not set by default")
	}
	if cfg.Transform.Backend == "" {
		t.Error("Transform.Backend not set by default")
	}
	if cfg.Transform.SystemPrompt == "" {
		t.Error("Transform.SystemPrompt not set by default")
	}
	if cfg.Injection.ElectronStrategy == "" {
		t.Error("Injection.ElectronStrategy not set by default")
	}
}

func TestDefaultConfig_RoundTripsThroughTOML(t *testing.T) {
	original := config.DefaultConfig()

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(original); err != nil {
		t.Fatalf("encode default config: %v", err)
	}

	var decoded config.Config
	if _, err := toml.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("decode encoded default config: %v", err)
	}

	if !reflect.DeepEqual(original, decoded) {
		t.Fatalf("round-trip drift:\n  original=%+v\n  decoded=%+v", original, decoded)
	}
}

func TestDefaultConfig_GoldenTOML(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "default_config.toml"))
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(config.DefaultConfig()); err != nil {
		t.Fatalf("encode default config: %v", err)
	}

	if !bytes.Equal(want, buf.Bytes()) {
		t.Errorf("golden mismatch. Regenerate testdata/default_config.toml.\n"+
			"got:\n%s\nwant:\n%s", buf.String(), string(want))
	}
}

func TestResolvedAPIURL(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.TranscriptionConfig
		want string
	}{
		{
			name: "explicit url wins",
			cfg:  config.TranscriptionConfig{Backend: "groq", APIURL: "https://example.test/v1"},
			want: "https://example.test/v1",
		},
		{
			name: "groq default",
			cfg:  config.TranscriptionConfig{Backend: "groq"},
			want: "https://api.groq.com/openai/v1/audio/transcriptions",
		},
		{
			name: "custom has no default",
			cfg:  config.TranscriptionConfig{Backend: "custom"},
			want: "",
		},
		{
			name: "whisperlocal has no url",
			cfg:  config.TranscriptionConfig{Backend: "whisperlocal"},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.ResolvedAPIURL(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultConfig_PartialSectionMergesCleanly(t *testing.T) {
	// A user-written TOML file with only a single section must
	// decode without clobbering other sections' defaults. This is
	// the contract the Load() shim depends on.
	partial := []byte(`
[general]
  hotkey = "KEY_SPACE"
  max_duration = 30
`)
	cfg := config.DefaultConfig()
	if _, err := toml.NewDecoder(bytes.NewReader(partial)).Decode(&cfg); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if cfg.General.Hotkey != "KEY_SPACE" {
		t.Errorf("partial decode did not apply hotkey, got %q", cfg.General.Hotkey)
	}
	if cfg.General.MaxDuration != 30 {
		t.Errorf("partial decode did not apply max_duration, got %d", cfg.General.MaxDuration)
	}
	// Other sections retain defaults.
	if cfg.Transcription.Backend != "whisperlocal" {
		t.Errorf("partial decode clobbered Transcription.Backend, got %q", cfg.Transcription.Backend)
	}
	if cfg.Injection.ElectronStrategy != "clipboard" {
		t.Errorf("partial decode clobbered Injection.ElectronStrategy, got %q", cfg.Injection.ElectronStrategy)
	}
}
