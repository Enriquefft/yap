// Package hint provides the context-aware hint pipeline for yap's
// Phase 12. It defines the Provider interface, the Bundle type that
// carries vocabulary and conversation context, a Factory type for
// provider construction, and a registry (Register/Get/Providers) that
// mirrors the pattern in pkg/yap/transcribe and pkg/yap/transform.
//
// Providers are responsible for fetching application-specific
// conversation context (e.g. Claude Code session JSONL, tmux pane
// scrollback). Project-level vocabulary (CLAUDE.md, AGENTS.md,
// README.md) is handled by the daemon's base layer via
// ReadVocabularyFiles, not by individual providers.
//
// The hint package is deliberately free of platform-specific imports.
// Concrete providers live in sub-packages (claudecode, termscroll) and
// register themselves via init().
package hint
