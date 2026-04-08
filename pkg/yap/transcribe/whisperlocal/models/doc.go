// Package models manages the whisper.cpp model cache used by the
// whisperlocal transcription backend.
//
// The package owns three concerns and nothing else:
//
//   - Resolving the on-disk cache directory
//     ($XDG_CACHE_HOME/yap/models on Linux, ~/Library/Caches/yap/models
//     on macOS, %LOCALAPPDATA%/yap/Cache/models on Windows).
//   - Downloading a pinned model file from Hugging Face into that cache
//     atomically (temp-file + rename) and verifying the SHA256 against
//     a compile-time manifest. Concurrent downloads from multiple yap
//     processes are serialized via an advisory file lock on the cache
//     directory's .lock sentinel.
//   - Reporting which models are installed for the `yap models list`
//     CLI command.
//
// This is deliberately separate from the whisperlocal backend itself so
// the CLI commands can drive downloads without pulling in the
// subprocess/HTTP code in package whisperlocal.
//
// The package exposes a Manager struct that owns its HTTP client and
// pinned manifest. Production callers use the package-level wrappers
// (Path, Installed, Download, List) which delegate to a lazily-built
// Default() singleton. Tests construct their own Manager via
// NewManager(WithHTTPClient(...), WithManifest(...)) — there are no
// "ForTest" hooks that mutate package state at runtime.
package models
