package whisperlocal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/hybridz/yap/pkg/yap/transcribe"
	"github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal/models"
)

// ggmlMagic is the 4-byte prefix of every whisper.cpp model file.
// The bytes are the little-endian uint32 0x67676d6c — "ggml" in
// ASCII. whisper.cpp source pins this as GGML_FILE_MAGIC and rejects
// any model whose first uint32 differs; we mirror the check at
// resolveModel time so users who drop a 404 HTML body or a renamed
// ZIP into the cache get a clear error up front instead of a
// cryptic "subprocess exited during startup" thirty seconds later.
//
// The byte order (lmgg) is the on-disk representation on
// little-endian hosts, which covers every platform yap targets.
const ggmlMagic = "lmgg"

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
//  1. cfg.ModelPath (transcription.model_path) when set — must exist
//     and carry the ggml magic bytes. This is the air-gapped escape
//     hatch and bypasses the manifest SHA256 check but still refuses
//     obviously-wrong files.
//  2. models.Path(cfg.Model) — looks the model up in the pinned
//     manifest, then checks the cache directory for the resolved file
//     and verifies the ggml magic bytes.
//
// Returns a clear error pointing the user at `yap models download
// <name>` when the model is in the manifest but not yet installed,
// at the manifest hint when the name is not pinned, and at a
// "redownload via yap models download" hint when the file exists
// but is not a real whisper.cpp model (e.g. a saved 404 HTML body).
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
		if err := verifyGGMLMagic(cfg.ModelPath); err != nil {
			return "", err
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
	if err := verifyGGMLMagic(p); err != nil {
		return "", err
	}
	return p, nil
}

// verifyGGMLMagic opens path, reads the first four bytes, and
// returns nil if they match the ggml magic prefix. Any other state
// (read error, short file, wrong magic) returns an error that
// tells the user exactly how to recover.
//
// The check is cheap (one open, one 4-byte read, one close) so it
// runs on every resolveModel call rather than being cached.
func verifyGGMLMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("whisperlocal: open %s: %w", path, err)
	}
	defer f.Close()
	var head [4]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf(
			"whisperlocal: %s is not a whisper.cpp model file (file too short to read magic bytes); redownload via yap models download <name>",
			path)
	}
	if !bytes.Equal(head[:], []byte(ggmlMagic)) {
		return fmt.Errorf(
			"whisperlocal: %s is not a whisper.cpp model file (expected ggml magic bytes); redownload via yap models download <name>",
			path)
	}
	return nil
}
