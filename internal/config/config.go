// Package config is a thin shim around pkg/yap/config. The schema and
// validation live in pkg/yap/config; this package only handles disk I/O,
// XDG path resolution, the legacy-flat-to-nested migration, and the
// /etc/yap/config.toml fallback that the NixOS module relies on.
package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/adrg/xdg"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// Config is the on-disk configuration document. It is a type alias for
// pkg/yap/config.Config so callers may use either package without a
// conversion. The shape lives in pkg/yap/config; do not redefine here.
type Config = pcfg.Config

// systemConfigPath is the location written by the NixOS module. The
// user-level XDG file always takes precedence; this is only consulted
// when the user has not run the wizard yet.
const systemConfigPath = "/etc/yap/config.toml"

// ConfigPath resolves the config file path with this precedence:
//
//  1. $YAP_CONFIG (explicit override; used in tests and alternate profiles)
//  2. $XDG_CONFIG_HOME/yap/config.toml (user-level, via adrg/xdg) — if it exists
//  3. /etc/yap/config.toml (system-level, written by the NixOS module) — if it exists
//  4. The XDG user path (used as the default Save target on first run)
//
// xdg.Reload() is called so the function honors XDG_CONFIG_HOME changes
// made after process start (the adrg/xdg library caches resolved
// directories at init time).
//
// ConfigPath is a package-level var so the wizard tests can replace it.
// New code should not introduce additional package-level mutable state.
var ConfigPath = func() (string, error) {
	if p := os.Getenv(pcfg.EnvConfig); p != "" {
		return p, nil
	}
	xdg.Reload()
	user, err := xdg.ConfigFile("yap/config.toml")
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(user); statErr == nil {
		return user, nil
	}
	if _, statErr := os.Stat(systemConfigPath); statErr == nil {
		return systemConfigPath, nil
	}
	return user, nil
}

// Load reads the config file, applies defaults for missing keys, runs
// the legacy-flat-to-nested migration if needed, and applies env var
// overrides. Deprecation notices and unknown-key warnings are written
// to os.Stderr.
//
// A missing config file is NOT an error: Load returns the default
// config with env overrides applied. This handles the first-run
// scenario before the wizard creates a file.
func Load() (Config, error) {
	return LoadWithNotices(os.Stderr)
}

// LoadWithNotices is Load with an explicit notice writer. The daemon
// and the CLI use Load (which writes to stderr); tests capture notices
// by passing a *bytes.Buffer. This is the constructor-injection point
// for migration messages — no package-level writer variable.
func LoadWithNotices(notices io.Writer) (Config, error) {
	cfg := pcfg.DefaultConfig()

	configPath, err := ConfigPath()
	if err != nil {
		return cfg, fmt.Errorf("xdg config path: %w", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			pcfg.ApplyEnvOverrides(&cfg)
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", configPath, err)
	}

	cfg, err = decodeAndMigrate(notices, configPath, data, cfg)
	if err != nil {
		return cfg, err
	}

	pcfg.ApplyEnvOverrides(&cfg)
	return cfg, nil
}

// Save atomically writes cfg to the user-level config path. Save
// always writes to the user XDG path, never to /etc/yap (the system
// path is for NixOS-managed installs).
func Save(cfg Config) error {
	configPath, err := userConfigPath()
	if err != nil {
		return fmt.Errorf("xdg config path: %w", err)
	}

	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tempFile, err := os.CreateTemp(configDir, "yap-config-*.toml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	if err := toml.NewEncoder(tempFile).Encode(cfg); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("encode config: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}

// userConfigPath resolves the user-level XDG config path, ignoring the
// /etc/yap fallback. Used by Save which must never overwrite the
// system-managed file.
func userConfigPath() (string, error) {
	if p := os.Getenv(pcfg.EnvConfig); p != "" {
		return p, nil
	}
	xdg.Reload()
	return xdg.ConfigFile("yap/config.toml")
}
