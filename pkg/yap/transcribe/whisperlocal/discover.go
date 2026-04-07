package whisperlocal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal/models"
)

// envServerPath is the environment variable users set to override the
// whisper-server binary location without writing it into config.
const envServerPath = "YAP_WHISPER_SERVER"

// nixProfileFallback is the canonical path under a NixOS system
// closure. yap is heavily Nix-friendly and many users will install
// whisper-cpp via `nix profile install nixpkgs#whisper-cpp` or have it
// in their system module — both end up at this prefix.
const nixProfileFallback = "/run/current-system/sw/bin/whisper-server"

// installHints is the human-readable text appended to a "binary not
// found" error. Single source of truth so test assertions and the
// error message stay in sync.
const installHints = `whisper-server binary not found. Install whisper.cpp:
  Nix:      nix profile install nixpkgs#whisper-cpp
  Arch:     pacman -S whisper.cpp     (AUR)
  Debian:   apt install whisper-cpp
  macOS:    brew install whisper-cpp
  Source:   https://github.com/ggerganov/whisper.cpp
Then either put whisper-server on $PATH or set transcription.whisper_server_path.`

// discoverer is a tiny seam over the file/exec lookups so unit tests
// can substitute deterministic stubs without touching package globals.
// All four hooks are required.
type discoverer struct {
	// stat reports whether path exists as a non-directory regular
	// file with the executable bit set. Tests pass a fake.
	stat func(path string) (os.FileInfo, error)
	// lookPath wraps exec.LookPath. Tests pass a fake.
	lookPath func(name string) (string, error)
	// getenv wraps os.Getenv. Tests pass a fake.
	getenv func(key string) string
}

// defaultDiscoverer returns a discoverer wired to the real OS.
func defaultDiscoverer() discoverer {
	return discoverer{
		stat:     os.Stat,
		lookPath: exec.LookPath,
		getenv:   os.Getenv,
	}
}

// discoverServer locates the whisper-server binary in the order
// documented in the package doc. Returns an absolute path on success
// or a wrapped error containing installation hints on failure.
func discoverServer(cfg transcribe.Config) (string, error) {
	return defaultDiscoverer().discoverServer(cfg)
}

// discoverServer is the seam-using variant called by both the public
// helper above and the unit tests.
func (d discoverer) discoverServer(cfg transcribe.Config) (string, error) {
	// 1. Explicit config path: must exist and be regular.
	if cfg.WhisperServerPath != "" {
		if err := d.checkExecutable(cfg.WhisperServerPath); err != nil {
			return "", fmt.Errorf("whisperlocal: transcription.whisper_server_path %q: %w",
				cfg.WhisperServerPath, err)
		}
		return cfg.WhisperServerPath, nil
	}

	// 2. Environment variable: same contract as the config field.
	if env := d.getenv(envServerPath); env != "" {
		if err := d.checkExecutable(env); err != nil {
			return "", fmt.Errorf("whisperlocal: %s=%q: %w", envServerPath, env, err)
		}
		return env, nil
	}

	// 3. PATH lookup: exec.LookPath.
	if p, err := d.lookPath("whisper-server"); err == nil {
		return p, nil
	}

	// 4. Nix system profile fallback.
	if err := d.checkExecutable(nixProfileFallback); err == nil {
		return nixProfileFallback, nil
	}

	return "", errors.New("whisperlocal: " + installHints)
}

// checkExecutable returns nil if path exists and is a regular file.
// We do not assert the executable bit because Nix store paths are
// always 0o555 and the OS itself enforces exec on its end; failing
// here on a missing bit would be a false positive.
func (d discoverer) checkExecutable(path string) error {
	info, err := d.stat(path)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return errors.New("not a regular file")
	}
	return nil
}

// resolveModel returns the absolute path to the ggml-*.bin file the
// subprocess should load. Resolution order:
//
//  1. cfg.ModelPath (transcription.model_path) when set — must exist.
//     This is the air-gapped escape hatch and bypasses the manifest.
//  2. models.Path(cfg.Model) — looks the model up in the pinned
//     manifest, then checks the cache directory for the resolved file.
//
// Returns a clear error pointing the user at `yap models download
// <name>` when the model is in the manifest but not yet installed,
// and at the manifest hint when the name is not pinned.
func resolveModel(cfg transcribe.Config) (string, error) {
	if cfg.ModelPath != "" {
		info, err := os.Stat(cfg.ModelPath)
		if err != nil {
			return "", fmt.Errorf("whisperlocal: transcription.model_path %q: %w",
				cfg.ModelPath, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("whisperlocal: transcription.model_path %q is a directory",
				cfg.ModelPath)
		}
		return cfg.ModelPath, nil
	}

	if cfg.Model == "" {
		return "", errors.New("whisperlocal: transcription.model is required when transcription.model_path is empty")
	}

	p, err := models.Path(cfg.Model)
	if err != nil {
		return "", fmt.Errorf("whisperlocal: %w", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"whisperlocal: model %q is not installed at %s. "+
					"Run `yap models download %s` (or set transcription.model_path "+
					"to a hand-downloaded ggml-%s.bin file)",
				cfg.Model, p, cfg.Model, cfg.Model)
		}
		return "", fmt.Errorf("whisperlocal: stat %s: %w", p, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("whisperlocal: cached model path %s is a directory", p)
	}
	return p, nil
}
