package config

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/BurntSushi/toml"
	pcfg "github.com/hybridz/yap/pkg/yap/config"
)

// flatLegacy is the pre-Phase-2 flat config schema. Decoding into this
// type is the only place these field names appear in current code.
//
// Field rename history (Phase 2):
//
//	timeout_seconds → general.max_duration
//	mic_device      → general.audio_device
//	api_key         → transcription.api_key
//	hotkey          → general.hotkey
//	language        → transcription.language
type flatLegacy struct {
	APIKey         string `toml:"api_key"`
	Hotkey         string `toml:"hotkey"`
	Language       string `toml:"language"`
	MicDevice      string `toml:"mic_device"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

// migrationNoticeOnce gates the deprecation notice so it prints at
// most once per process. The notice is purely informational, so a
// single message across multiple Load() calls (e.g. wizard then
// daemon) is correct UX.
//
// This is the single permitted package-level state in the migration
// path: a one-shot guard, not a mutable knob. Tests reset it via
// resetMigrationNoticeForTest in the _test.go file via export_test.go.
var migrationNoticeOnce sync.Once

// groqDeprecationNoticeOnce gates the Phase 6 informational notice
// that prints when a user has `transcription.backend = "groq"`
// explicitly set in their config file. The default flipped to
// "whisperlocal" in Phase 6, so users who left their old groq line in
// place are now opting in to the remote backend rather than getting it
// by default. The notice tells them they can switch to the local
// backend if they want to. Like the migration notice, it prints at
// most once per process.
var groqDeprecationNoticeOnce sync.Once

// printMigrationNotice writes the deprecation notice exactly once.
// w is the destination (os.Stderr in production, a *bytes.Buffer in
// tests). The function is a no-op on subsequent calls in the same
// process.
func printMigrationNotice(w io.Writer, path string) {
	migrationNoticeOnce.Do(func() {
		fmt.Fprintf(w, "yap: migrated legacy flat config at %s to nested schema (in memory). Save the file via `yap config set ...` or the wizard to persist the new format.\n", path)
	})
}

// printGroqDeprecationNotice writes the Phase 6 informational notice
// exactly once per process. It is informational, not a deprecation:
// the groq backend remains supported. The notice exists so users who
// left a long-standing `backend = "groq"` line in place know they
// have the option of switching to the local default.
func printGroqDeprecationNotice(w io.Writer) {
	groqDeprecationNoticeOnce.Do(func() {
		fmt.Fprintln(w, "yap: transcription.backend = \"groq\" is set explicitly. The default in Phase 6+ is \"whisperlocal\" (local whisper.cpp). Run `yap config set transcription.backend whisperlocal && yap models download base.en` to switch.")
	})
}

// looksLegacy is conservative: it only returns true when the file
// contains at least one legacy flat key AND no nested section header.
// A hybrid file (some flat keys plus a [transcription] table) is
// treated as nested-with-warnings, never silently flattened.
func looksLegacy(md toml.MetaData, data []byte) bool {
	hasFlatKey := false
	for _, k := range md.Keys() {
		switch k.String() {
		case "api_key", "hotkey", "language", "mic_device", "timeout_seconds":
			hasFlatKey = true
		}
	}
	if !hasFlatKey {
		return false
	}
	// If any nested section header is present, the user has already
	// started migrating; do not flatten silently.
	for _, header := range [][]byte{
		[]byte("[general]"),
		[]byte("[transcription]"),
		[]byte("[transform]"),
		[]byte("[injection]"),
		[]byte("[tray]"),
	} {
		if bytes.Contains(data, header) {
			return false
		}
	}
	return true
}

// migrateFlat copies values from a flatLegacy struct into the nested
// Config. Defaults already populated by DefaultConfig() are preserved
// for any field the legacy file omitted.
func migrateFlat(legacy flatLegacy, cfg pcfg.Config) pcfg.Config {
	if legacy.Hotkey != "" {
		cfg.General.Hotkey = legacy.Hotkey
	}
	if legacy.TimeoutSeconds > 0 {
		cfg.General.MaxDuration = legacy.TimeoutSeconds
	}
	if legacy.MicDevice != "" {
		cfg.General.AudioDevice = legacy.MicDevice
	}
	if legacy.APIKey != "" {
		cfg.Transcription.APIKey = legacy.APIKey
	}
	if legacy.Language != "" {
		cfg.Transcription.Language = legacy.Language
	}
	return cfg
}

// decodeAndMigrate decodes data into cfg, transparently migrating a
// pre-Phase-2 flat schema if detected. The returned config has every
// field populated; sections absent from the file fall back to defaults.
//
// Migration leaves the on-disk file unchanged. The user-visible
// migration happens on the next Save (e.g. via the wizard or
// `yap config set`).
//
// notices is the writer that receives the deprecation notice and any
// unknown-key warnings. Production passes os.Stderr.
func decodeAndMigrate(notices io.Writer, path string, data []byte, cfg pcfg.Config) (pcfg.Config, error) {
	// Pass 1: try the nested schema. If decode succeeds and the file
	// is not legacy-shaped, we are done.
	nested := cfg
	md, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&nested)
	if err == nil && !looksLegacy(md, data) {
		warnUndecoded(notices, path, md)
		// Phase 6 informational notice: an explicit groq backend
		// in a Phase-6+ config tells the user the local backend is
		// available. md.IsDefined is the right discriminator: it
		// returns true only when the file actually contained the
		// key, not when DefaultConfig() supplied the value.
		if md.IsDefined("transcription", "backend") && nested.Transcription.Backend == "groq" {
			printGroqDeprecationNotice(notices)
		}
		return nested, nil
	}

	// Pass 2: try the legacy flat schema only if the file looks legacy.
	if looksLegacy(md, data) {
		var legacy flatLegacy
		if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&legacy); err != nil {
			return cfg, fmt.Errorf("parse legacy config %s: %w", path, err)
		}
		printMigrationNotice(notices, path)
		return migrateFlat(legacy, cfg), nil
	}

	// Nested decode failed and the file is not legacy-shaped. Surface
	// the original error.
	if err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	warnUndecoded(notices, path, md)
	return nested, nil
}

// warnUndecoded prints a one-line warning per unknown key. Warnings
// are advisory: unknown keys do not fail Load().
func warnUndecoded(w io.Writer, path string, md toml.MetaData) {
	for _, key := range md.Undecoded() {
		fmt.Fprintf(w, "yap: warning: unknown config key in %s: %s\n", path, key.String())
	}
}
