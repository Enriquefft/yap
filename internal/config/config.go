package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/adrg/xdg"
)

// Config holds all yap configuration. Passed via dependency injection — never stored globally.
type Config struct {
	APIKey         string `toml:"api_key"`
	Hotkey         string `toml:"hotkey"`
	Language       string `toml:"language"`
	MicDevice      string `toml:"mic_device"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

// defaults returns a Config with safe defaults for a missing config file.
func defaults() Config {
	return Config{
		Hotkey:         "KEY_RIGHTCTRL",
		Language:       "en",
		TimeoutSeconds: 60,
	}
}

// ConfigPath returns the XDG-compliant path to the config file.
// Uses adrg/xdg — NOT os.UserConfigDir() which has a known XDG_CONFIG_HOME bug (Go issue #76320).
// xdg.Reload() is called to re-read the current environment (adrg/xdg caches dirs in init()).
func ConfigPath() (string, error) {
	xdg.Reload()
	return xdg.ConfigFile("yap/config.toml")
}

// Load reads the config file, applies defaults for missing keys, and applies env var overrides.
// A missing config file is NOT an error — it returns defaults.
func Load() (Config, error) {
	cfg := defaults()

	configPath, err := ConfigPath()
	if err != nil {
		return cfg, fmt.Errorf("xdg config path: %w", err)
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Missing config file — return defaults, not an error.
		// This handles first-run scenario before the wizard creates a config.
		applyEnvOverrides(&cfg)
		return cfg, nil
	}

	md, err := toml.DecodeFile(configPath, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", configPath, err)
	}

	// Warn about unrecognized keys (non-fatal).
	for _, key := range md.Undecoded() {
		fmt.Fprintf(os.Stderr, "yap: warning: unknown config key: %s\n", key)
	}

	applyEnvOverrides(&cfg)
	return cfg, nil
}

// applyEnvOverrides applies environment variable overrides after TOML decode.
// GROQ_API_KEY overrides api_key; YAP_HOTKEY overrides hotkey.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GROQ_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("YAP_HOTKEY"); v != "" {
		cfg.Hotkey = v
	}
}

// Save atomically writes the config to the config file.
// Pattern: Write to temp file → sync → rename (atomic, prevents corruption).
func Save(cfg Config) error {
	configPath, err := ConfigPath()
	if err != nil {
		return fmt.Errorf("xdg config path: %w", err)
	}

	// Create parent directories if they don't exist
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Create temp file in same directory (same filesystem for atomic rename)
	tempFile, err := os.CreateTemp(configDir, "yap-config-*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Encode config to temp file
	if err := toml.NewEncoder(tempFile).Encode(cfg); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("encode config: %w", err)
	}

	// Sync temp file to disk
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("sync temp file: %w", err)
	}

	// Close temp file
	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename temp file to config file
	if err := os.Rename(tempPath, configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
