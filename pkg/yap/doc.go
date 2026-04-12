// Package yap is the top-level client for yap's composable primitives:
// Transcribe, Transform, and (via sub-packages) Inject. A Client wraps
// a Transcriber and a Transformer and exposes both batch and streaming
// entry points. Third-party Go programs can import this package and
// drive transcription without touching the daemon, CLI, or config
// packages.
//
// Typical usage:
//
//	import (
//	    "context"
//	    "os"
//
//	    "github.com/Enriquefft/yap/pkg/yap"
//	    "github.com/Enriquefft/yap/pkg/yap/transcribe"
//	    "github.com/Enriquefft/yap/pkg/yap/transcribe/groq"
//	)
//
//	backend, err := groq.New(transcribe.Config{
//	    APIKey: os.Getenv("GROQ_API_KEY"),
//	    Model:  "whisper-large-v3-turbo",
//	})
//	if err != nil { ... }
//
//	client, err := yap.New(yap.WithTranscriber(backend))
//	if err != nil { ... }
//
//	text, err := client.Transcribe(ctx, audio)
//
// By default a Client uses the passthrough Transformer, so the
// pipeline behaves identically to the bare Transcriber until a
// different Transformer is supplied with WithTransformer.
package yap
