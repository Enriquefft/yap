// Package transform is the stable public interface for yap's
// LLM-based post-processing stage. A Transformer consumes the chunk
// stream produced by a Transcriber and emits a (possibly rewritten)
// chunk stream suitable for injection.
//
// Phase 3 ships the interface and a passthrough default. Phase 8
// introduces the concrete local (Ollama / llama.cpp) and remote
// (OpenAI-compatible) backends. The dependency direction is
// transform → transcribe; transform imports transcribe for the
// TranscriptChunk type. Do not invert this.
//
// Like pkg/yap/transcribe, this package holds no mutable package-level
// state other than the backend registry and its lock.
package transform
