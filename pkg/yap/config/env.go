package config

import "os"

// Environment variable names. Exported constants so downstream
// packages (CLI, docs generators) can reference the same source of
// truth and any rename is a compile-time error.
const (
	// EnvAPIKey is the primary transcription API key override.
	EnvAPIKey = "YAP_API_KEY"
	// EnvGroqAPIKey is the legacy alias preserved for compatibility.
	// Only consulted when EnvAPIKey is unset.
	EnvGroqAPIKey = "GROQ_API_KEY"
	// EnvTransformAPIKey is the transform backend API key override.
	EnvTransformAPIKey = "YAP_TRANSFORM_API_KEY"
	// EnvHotkey overrides general.hotkey. Legacy alias from the flat
	// config era; kept for wizard and smoke-test ergonomics.
	EnvHotkey = "YAP_HOTKEY"
	// EnvConfig overrides the config file path. Consumed by
	// internal/config.ConfigPath, not by ApplyEnvOverrides.
	EnvConfig = "YAP_CONFIG"
)

// ApplyEnvOverrides mutates cfg in place with values from environment
// variables. Precedence is env > file > default.
//
//	YAP_API_KEY           -> Transcription.APIKey (primary)
//	GROQ_API_KEY          -> Transcription.APIKey (compat; only if YAP_API_KEY unset)
//	YAP_TRANSFORM_API_KEY -> Transform.APIKey
//	YAP_HOTKEY            -> General.Hotkey (compat)
//
// YAP_CONFIG is not an override of Config itself; it selects the file
// path and is handled in internal/config.ConfigPath.
func ApplyEnvOverrides(cfg *Config) {
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.Transcription.APIKey = v
	} else if v := os.Getenv(EnvGroqAPIKey); v != "" {
		cfg.Transcription.APIKey = v
	}
	if v := os.Getenv(EnvTransformAPIKey); v != "" {
		cfg.Transform.APIKey = v
	}
	if v := os.Getenv(EnvHotkey); v != "" {
		cfg.General.Hotkey = v
	}
}
