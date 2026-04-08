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
//
// # Retry semantics
//
// Each backend chooses its own retry policy because the failure
// modes differ. The two flavors currently in use:
//
//   - Remote backends (groq, openai, any OpenAI-compatible) retry
//     transient failures (5xx, transport errors, client timeouts)
//     up to 3 times with 500 ms / 1 s / 2 s backoff. The backoff
//     loop honors ctx cancellation: a cancelled ctx mid-backoff
//     short-circuits within microseconds rather than waiting for
//     the full delay. 4xx responses fail fast — they indicate a
//     client bug that will not recover on retry.
//
//   - whisperlocal retries at most once on subprocess-level
//     failures (network errors against the localhost child, EOF,
//     5xx from whisper-server) with no sleep between attempts. The
//     remediation is "kill the wedged subprocess and respawn", not
//     "wait for the upstream to recover", so backoff would only
//     add latency to the user-visible retry path. Subprocess
//     respawn happens transparently inside Backend.Transcribe.
//
// Errors are delivered on the chunk channel via TranscriptChunk.Err
// and terminate the stream. The implementation must not send
// further chunks after an error. This invariant is shared with
// transform.Transformer and inject.Injector.InjectStream — see
// each interface's godoc for the same rule restated in context.
package transcribe
