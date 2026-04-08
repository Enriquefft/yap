// Package local implements a transform.Transformer backed by the
// Ollama native API (POST /api/chat with a streaming NDJSON
// response). It also satisfies transform.Checker: HealthCheck
// probes GET / on the configured endpoint and expects a 2xx
// response, which Ollama returns with a plain-text "Ollama is
// running" banner.
//
// Endpoint shape:
//
//	POST {api_url}/api/chat
//	{"model":"...","messages":[{"role":"system","content":"..."},{"role":"user","content":"..."}],"stream":true}
//
// Response (one JSON object per line, terminated by "done": true):
//
//	{"message":{"content":"Hello"},"done":false}
//	{"message":{"content":", world"},"done":false}
//	{"message":{"content":"!"},"done":true}
//
// When transform.api_url is empty, the backend defaults to
// http://localhost:11434 — the Ollama default. Users running
// llama.cpp-server, vLLM, or any other OpenAI-compatible endpoint
// should use the openai backend instead, since those servers do not
// implement Ollama's /api/chat shape.
//
// Importing this package for side effects registers the backend
// under the name "local" in the transform registry:
//
//	import _ "github.com/hybridz/yap/pkg/yap/transform/local"
//
// Library consumers that want to skip the registry can construct a
// Backend directly via New.
//
// # Synthetic IsFinal terminator
//
// When the upstream Ollama server closes the stream without
// emitting an explicit "done": true frame (a misbehaving server, a
// dropped connection, or a model that ran out of context), the
// backend emits a synthetic transcribe.TranscriptChunk with
// IsFinal=true and an empty Text so the consumer always sees a
// terminal chunk. Consumers that filter out empty-Text chunks must
// preserve the IsFinal=true marker — otherwise they will silently
// drop the stream-end signal and the surrounding pipeline will
// never see the close.
//
// This package holds zero mutable package-level state; all runtime
// state lives on Backend.
package local
