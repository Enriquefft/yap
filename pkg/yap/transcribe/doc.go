// Package transcribe is the stable public interface for yap's
// speech-to-text backends. It exposes a streaming Transcriber
// interface, a configuration struct, a chunk type, and a backend
// registry.
//
// This package holds no mutable global state other than the backend
// registry. The registry is append-only and initialized via package
// init() functions in backend sub-packages; it is conceptually a
// compile-time constant map built at program start-up. Consumers
// import the backend sub-packages they need for side effects to pull
// registrations into the registry:
//
//	import (
//	    "github.com/hybridz/yap/pkg/yap/transcribe"
//	    _ "github.com/hybridz/yap/pkg/yap/transcribe/groq"
//	)
//
//	factory, err := transcribe.Get("groq")
//	if err != nil { ... }
//	t, err := factory(transcribe.Config{
//	    APIKey: os.Getenv("GROQ_API_KEY"),
//	    Model:  "whisper-large-v3-turbo",
//	})
//
// Backends deliver transcribed text on a channel. Non-streaming
// backends emit a single TranscriptChunk with IsFinal=true; streaming
// backends emit progressively and close with a final IsFinal chunk.
package transcribe
