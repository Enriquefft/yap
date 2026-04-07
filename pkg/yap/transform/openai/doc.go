// Package openai implements a transform.Transformer backed by any
// OpenAI-compatible /chat/completions endpoint that streams via
// Server-Sent Events. It works against real OpenAI, llama.cpp-server,
// vLLM, Ollama's /v1 compat layer, Together.ai, Groq's chat
// endpoint, and any other vendor that implements the same shape.
//
// URL format: transform.api_url must point at the collection root
// that exposes /chat/completions directly beneath it. For real
// OpenAI that is "https://api.openai.com/v1"; for llama.cpp-server
// it is typically "http://localhost:8080/v1"; for Ollama's compat
// layer it is "http://localhost:11434/v1". The backend appends
// "/chat/completions" to the configured URL to form the request
// endpoint.
//
// Because the URL semantics depend on the upstream server, this
// backend rejects an empty api_url at construction time — there is
// no sensible default.
//
// Request body:
//
//	{"model":"gpt-4o-mini","messages":[...],"stream":true}
//
// Response: Server-Sent Events, one frame per delta, terminated by
// the literal "data: [DONE]" line:
//
//	data: {"choices":[{"delta":{"content":"Hel"}}]}
//
//	data: {"choices":[{"delta":{"content":"lo"}}]}
//
//	data: [DONE]
//
// HealthCheck probes GET {api_url}/models and succeeds on any 2xx
// response.
//
// Importing this package for side effects registers it under the
// name "openai":
//
//	import _ "github.com/hybridz/yap/pkg/yap/transform/openai"
//
// Library consumers that want to skip the registry can construct a
// Backend directly via New.
package openai
