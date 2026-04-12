// Package config is a thin shim around pkg/yap/config. The schema and
// validation live in pkg/yap/config; this package only handles disk I/O,
// XDG path resolution, the legacy-flat-to-nested migration, and the
// /etc/yap/config.toml fallback that the NixOS module relies on.
package config

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/adrg/xdg"
	pcfg "github.com/Enriquefft/yap/pkg/yap/config"
)

// Config is the on-disk configuration document. It is a type alias for
// pkg/yap/config.Config so callers may use either package without a
// conversion. The shape lives in pkg/yap/config; do not redefine here.
type Config = pcfg.Config

// systemConfigPath is the location written by the NixOS module. The
// user-level XDG file always takes precedence; this is only consulted
// when the user has not run the wizard yet.
//
// This is a package-level var (not const) exclusively so tests can
// point it at a TempDir — the production default is the fixed FHS
// path and must never be mutated outside tests. Test overrides go
// through SetSystemConfigPathForTest in testhooks.go.
var systemConfigPath = "/etc/yap/config.toml"

// CandidateName labels one entry in the candidate-paths table.
type CandidateName string

const (
	// CandidateEnv is the $YAP_CONFIG environment override.
	CandidateEnv CandidateName = "$YAP_CONFIG"
	// CandidateUser is the per-user XDG path
	// ($XDG_CONFIG_HOME/yap/config.toml).
	CandidateUser CandidateName = "user XDG"
	// CandidateSystem is the NixOS-managed /etc/yap/config.toml.
	CandidateSystem CandidateName = "system (/etc/yap)"
)

// Candidate is one row in the candidate-paths table returned by
// CandidatePaths. Exactly one Candidate in a returned slice has
// Active=true — it is the path Load() will read.
type Candidate struct {
	// Name is the human-readable source identifier.
	Name CandidateName
	// Path is the resolved filesystem path, or "" if the candidate
	// is unset (e.g. $YAP_CONFIG with no value).
	Path string
	// Exists reports whether a regular file is present at Path.
	// Always false when Path is "".
	Exists bool
	// Active is true for the single candidate that Load() will
	// actually read. When no file exists anywhere, the first-run
	// Save target (user XDG) is marked Active so the caller can
	// display "this is where the wizard will write".
	Active bool
}

// CandidatePaths enumerates every config path yap considers, in
// precedence order, with metadata describing which one wins. This is
// the single source of truth for the resolution order — ConfigPath
// and the `yap config path` command are both thin wrappers over it.
//
// The returned slice always has a deterministic shape:
//
//  1. CandidateEnv — present iff $YAP_CONFIG is set. If set, this
//     candidate is Active regardless of whether the file exists
//     (the env override is explicit intent; a dangling path is the
//     user's problem and Load() will surface the error).
//  2. CandidateUser — the $XDG_CONFIG_HOME/yap/config.toml path,
//     always present.
//  3. CandidateSystem — /etc/yap/config.toml, always present.
//
// Exactly one candidate has Active=true. The Active winner is:
//
//   - CandidateEnv, if $YAP_CONFIG is set; else
//   - CandidateUser, if the user file exists; else
//   - CandidateSystem, if the system file exists; else
//   - CandidateUser (the first-run Save target).
//
// xdg.Reload() is called so the function honors XDG_CONFIG_HOME
// changes made after process start.
//
// CandidatePaths is a pure query: it has no filesystem side effects.
// In particular, it must NOT create $XDG_CONFIG_HOME/yap/ just to
// resolve the user path — that would surprise `yap config path`
// users inspecting config state on a fresh system. The user-path
// resolver (resolveUserConfigPath) uses xdg.ConfigHome (the resolved
// base directory, no side effects) rather than xdg.ConfigFile (which
// calls MkdirAll). Save() remains the sole caller responsible for
// creating the directory, and only when it is about to write a file.
func CandidatePaths() ([]Candidate, error) {
	out := make([]Candidate, 0, 3)

	envPath := os.Getenv(pcfg.EnvConfig)
	if envPath != "" {
		_, statErr := os.Stat(envPath)
		out = append(out, Candidate{
			Name:   CandidateEnv,
			Path:   envPath,
			Exists: statErr == nil,
			Active: true, // env override always wins
		})
	}

	userPath := resolveUserConfigPath()
	_, userStatErr := os.Stat(userPath)
	userExists := userStatErr == nil
	out = append(out, Candidate{
		Name:   CandidateUser,
		Path:   userPath,
		Exists: userExists,
	})

	_, sysStatErr := os.Stat(systemConfigPath)
	sysExists := sysStatErr == nil
	out = append(out, Candidate{
		Name:   CandidateSystem,
		Path:   systemConfigPath,
		Exists: sysExists,
	})

	// If the env candidate is present it is already marked Active
	// above. Otherwise pick the first existing file, falling back
	// to the user path as the first-run Save target.
	if envPath == "" {
		switch {
		case userExists:
			out[0].Active = true // user XDG is index 0 when env is absent
		case sysExists:
			out[1].Active = true // system is index 1 when env is absent
		default:
			out[0].Active = true // user XDG as first-run Save target
		}
	}

	return out, nil
}

// ConfigPath resolves the config file path with this precedence:
//
//  1. $YAP_CONFIG (explicit override; used in tests and alternate profiles)
//  2. $XDG_CONFIG_HOME/yap/config.toml (user-level, via adrg/xdg) — if it exists
//  3. /etc/yap/config.toml (system-level, written by the NixOS module) — if it exists
//  4. The XDG user path (used as the default Save target on first run)
//
// ConfigPath is a thin wrapper over CandidatePaths that returns only
// the Active winner. All resolution logic lives in CandidatePaths.
//
// ConfigPath is a package-level var so the wizard tests can replace it.
// New code should not introduce additional package-level mutable state.
var ConfigPath = func() (string, error) {
	candidates, err := CandidatePaths()
	if err != nil {
		return "", err
	}
	for _, c := range candidates {
		if c.Active {
			return c.Path, nil
		}
	}
	// Unreachable: CandidatePaths always marks exactly one Active.
	return "", fmt.Errorf("config: no active candidate (internal invariant violated)")
}

// shadowWarningOnce gates the shadow-warning slog message so it
// fires at most once per process. The condition is cheap to detect
// (two os.Stat calls) but noisy if printed on every Load() call —
// the daemon, wizard, and CLI subcommands may each call Load()
// independently within one invocation.
//
// This is the single permitted package-level state for the warning:
// a one-shot guard, not a mutable knob. Tests reset it via
// ResetShadowWarningForTest in testhooks.go.
var shadowWarningOnce sync.Once

// emitShadowWarningIfNeeded inspects candidates and logs a single
// slog.Warn when the user XDG file and the system /etc/yap/config.toml
// both exist. The warning names both paths and states which one
// wins — operators on NixOS systems reported debugging-time lost to
// this silent shadowing, and a surfacing log is the fix.
//
// The warning is emitted via slog.Default() so it flows through the
// daemon logger (Phase 7 logging convention). Tests install a
// capture handler on slog.Default() to assert the message appears.
//
// When $YAP_CONFIG is set the warning is suppressed: the env override
// is explicit intent and neither of the disk paths is being "used"
// in a way the user could be confused about.
func emitShadowWarningIfNeeded(candidates []Candidate) {
	var user, system Candidate
	var haveUser, haveSystem, haveEnv bool
	for _, c := range candidates {
		switch c.Name {
		case CandidateEnv:
			haveEnv = true
		case CandidateUser:
			user = c
			haveUser = true
		case CandidateSystem:
			system = c
			haveSystem = true
		}
	}
	if haveEnv {
		return
	}
	if !haveUser || !haveSystem {
		return
	}
	if !user.Exists || !system.Exists {
		return
	}

	shadowWarningOnce.Do(func() {
		active := "user"
		if system.Active {
			active = "system"
		}
		slog.Default().Warn(
			"config file shadowed: both user XDG and system /etc/yap configs exist — user file wins",
			"user_path", user.Path,
			"system_path", system.Path,
			"active", active,
			"hint", "delete one file or set $YAP_CONFIG to silence this warning",
		)
	})
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
//
// Shadow-detection warnings are emitted via slog.Default() (not the
// notices writer) so they flow through the daemon's structured
// logger. Tests install a capture handler on slog.Default() to
// assert the message appears.
func LoadWithNotices(notices io.Writer) (Config, error) {
	cfg := pcfg.DefaultConfig()

	candidates, err := CandidatePaths()
	if err != nil {
		return cfg, fmt.Errorf("xdg config path: %w", err)
	}
	emitShadowWarningIfNeeded(candidates)

	var configPath string
	for _, c := range candidates {
		if c.Active {
			configPath = c.Path
			break
		}
	}
	if configPath == "" {
		// Unreachable: CandidatePaths always marks exactly one
		// Active. Kept as a defensive guard.
		return cfg, fmt.Errorf("config: no active candidate (internal invariant violated)")
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
	configPath := userConfigPath()

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

// userConfigPath resolves the user-level XDG config path, ignoring
// the /etc/yap fallback. Used by Save which must never overwrite the
// system-managed file.
//
// When $YAP_CONFIG is set it wins unconditionally (explicit user
// intent; callers that need to reject empty values should check
// elsewhere — the wizard and Save both treat empty as "unset" via
// the os.Getenv check below). Otherwise the path is derived from
// resolveUserConfigPath so Save and CandidatePaths share one
// source of truth for the XDG layout.
//
// userConfigPath itself has no filesystem side effects. Save's
// MkdirAll is the sole caller that creates the parent directory,
// and only when it is about to write a file.
func userConfigPath() string {
	if p := os.Getenv(pcfg.EnvConfig); p != "" {
		return p
	}
	return resolveUserConfigPath()
}

// resolveUserConfigPath returns the user-level XDG config path for
// yap without creating any directories. It is the single source of
// truth for the $XDG_CONFIG_HOME/yap/config.toml layout shared
// between CandidatePaths (pure query) and userConfigPath
// (Save write target).
//
// xdg.Reload() is called so the helper honors XDG_CONFIG_HOME
// changes made after process start — tests that t.Setenv the base
// directory rely on this behavior. xdg.ConfigHome itself is a pure
// string field populated by Reload from the env with fallback to
// $HOME/.config; it performs no filesystem operations.
//
// This helper exists instead of xdg.ConfigFile(...) — the latter
// calls pathutil.Create which MkdirAll's the parent as a side
// effect, breaking the query-only semantics of CandidatePaths and
// making `yap config path` create directories on a fresh host.
func resolveUserConfigPath() string {
	xdg.Reload()
	return filepath.Join(xdg.ConfigHome, "yap", "config.toml")
}
