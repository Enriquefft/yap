// Package openai implements a generic OpenAI-compatible speech-to-text
// backend. Any server that speaks the /v1/audio/transcriptions protocol
// (OpenAI itself, Groq, vLLM, llama.cpp server, litellm, Fireworks,
// ...) is a valid target. Callers must provide Config.APIURL — this
// backend does not substitute a default endpoint because there is no
// universal default for "OpenAI-compatible."
//
// Wire behavior, retry semantics, and request shape are identical to
// the Groq backend. The packages are kept separate so each can pick
// its own defaults and so future divergence (e.g. Groq-specific
// response fields) does not cross-contaminate.
//
// Importing this package for side effects registers the backend under
// the name "openai" in the transcribe registry:
//
//	import _ "github.com/hybridz/yap/pkg/yap/transcribe/openai"
package openai
