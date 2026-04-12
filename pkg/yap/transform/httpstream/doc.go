// Package httpstream is a small HTTP streaming helper shared by the
// local and openai transform backends. It centralises the JSON
// request encoding, the retry-with-backoff policy, and the 4xx
// fail-fast behaviour so each backend can focus on its own
// wire-format specifics.
//
// The package is deliberately public rather than internal: third
// parties writing their own transform backend under
// github.com/Enriquefft/yap/pkg/yap/transform can reuse the same
// scaffolding without copy-pasting or reaching into an internal/
// directory. The surface is intentionally minimal — a Client, a
// PostJSON method, and a NonRetryableError sentinel.
//
// Retry policy (mirrored from pkg/yap/transcribe/groq): up to three
// attempts, exponential-ish backoff of 500ms / 1s / 2s, transient
// transport errors and 5xx responses are retried, 4xx responses are
// returned immediately as a NonRetryableError. Context cancellation
// short-circuits the retry loop and returns ctx.Err.
//
// Like the rest of pkg/yap, httpstream holds no mutable
// package-level state. Callers construct a Client per backend (or
// share one) and feed requests through it.
package httpstream
