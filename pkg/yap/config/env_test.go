package config_test

import (
	"testing"

	"github.com/hybridz/yap/pkg/yap/config"
)

// clearEnv unsets every yap-related env var so each test starts from
// a clean slate. t.Setenv handles restoration after the test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		config.EnvAPIKey,
		config.EnvGroqAPIKey,
		config.EnvTransformAPIKey,
		config.EnvHotkey,
	} {
		t.Setenv(k, "")
	}
}

func TestApplyEnvOverrides_NoEnvKeepsFileValues(t *testing.T) {
	clearEnv(t)
	cfg := config.DefaultConfig()
	cfg.Transcription.APIKey = "from-file"
	cfg.Transform.APIKey = "transform-from-file"
	cfg.General.Hotkey = "KEY_FROM_FILE"

	config.ApplyEnvOverrides(&cfg)

	if cfg.Transcription.APIKey != "from-file" {
		t.Errorf("Transcription.APIKey: got %q, want from-file", cfg.Transcription.APIKey)
	}
	if cfg.Transform.APIKey != "transform-from-file" {
		t.Errorf("Transform.APIKey: got %q, want transform-from-file", cfg.Transform.APIKey)
	}
	if cfg.General.Hotkey != "KEY_FROM_FILE" {
		t.Errorf("General.Hotkey: got %q, want KEY_FROM_FILE", cfg.General.Hotkey)
	}
}

func TestApplyEnvOverrides_YapAPIKeyWins(t *testing.T) {
	clearEnv(t)
	t.Setenv(config.EnvAPIKey, "yap-key")
	t.Setenv(config.EnvGroqAPIKey, "groq-key")

	cfg := config.DefaultConfig()
	cfg.Transcription.APIKey = "from-file"
	config.ApplyEnvOverrides(&cfg)

	if cfg.Transcription.APIKey != "yap-key" {
		t.Errorf("YAP_API_KEY should win over GROQ_API_KEY and file: got %q", cfg.Transcription.APIKey)
	}
}

func TestApplyEnvOverrides_GroqAPIKeyFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv(config.EnvGroqAPIKey, "groq-key")

	cfg := config.DefaultConfig()
	cfg.Transcription.APIKey = "from-file"
	config.ApplyEnvOverrides(&cfg)

	if cfg.Transcription.APIKey != "groq-key" {
		t.Errorf("GROQ_API_KEY should override file when YAP_API_KEY unset: got %q", cfg.Transcription.APIKey)
	}
}

func TestApplyEnvOverrides_TransformAPIKey(t *testing.T) {
	clearEnv(t)
	t.Setenv(config.EnvTransformAPIKey, "tx-key")

	cfg := config.DefaultConfig()
	cfg.Transform.APIKey = "from-file"
	config.ApplyEnvOverrides(&cfg)

	if cfg.Transform.APIKey != "tx-key" {
		t.Errorf("YAP_TRANSFORM_API_KEY should override Transform.APIKey: got %q", cfg.Transform.APIKey)
	}
}

func TestApplyEnvOverrides_Hotkey(t *testing.T) {
	clearEnv(t)
	t.Setenv(config.EnvHotkey, "KEY_F2")

	cfg := config.DefaultConfig()
	cfg.General.Hotkey = "KEY_RIGHTCTRL"
	config.ApplyEnvOverrides(&cfg)

	if cfg.General.Hotkey != "KEY_F2" {
		t.Errorf("YAP_HOTKEY should override General.Hotkey: got %q", cfg.General.Hotkey)
	}
}

func TestApplyEnvOverrides_DoesNotTouchUnrelatedFields(t *testing.T) {
	clearEnv(t)
	t.Setenv(config.EnvAPIKey, "yap-key")

	cfg := config.DefaultConfig()
	cfg.General.MaxDuration = 42
	cfg.Transcription.Model = "custom-model"
	cfg.Injection.ElectronStrategy = "keystroke"
	config.ApplyEnvOverrides(&cfg)

	if cfg.General.MaxDuration != 42 {
		t.Errorf("MaxDuration mutated: got %d", cfg.General.MaxDuration)
	}
	if cfg.Transcription.Model != "custom-model" {
		t.Errorf("Model mutated: got %q", cfg.Transcription.Model)
	}
	if cfg.Injection.ElectronStrategy != "keystroke" {
		t.Errorf("ElectronStrategy mutated: got %q", cfg.Injection.ElectronStrategy)
	}
}
