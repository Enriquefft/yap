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
//     a compile-time manifest.
//   - Reporting which models are installed for the `yap models list`
//     CLI command.
//
// This is deliberately separate from the whisperlocal backend itself so
// the CLI commands can drive downloads without pulling in the
// subprocess/HTTP code in package whisperlocal.
//
// The package contains exactly one package-level mutable variable —
// downloadClient, the HTTP client used for model downloads. It is
// whitelisted by name in the noglobals AST guard so tests can swap it
// for an httptest client. No other globals are permitted.
package models
