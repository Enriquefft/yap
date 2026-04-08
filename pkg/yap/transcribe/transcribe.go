package transcribe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Transcriber consumes audio and emits transcript chunks on a channel.
//
// Non-streaming backends deliver a single TranscriptChunk with
// IsFinal=true. Streaming backends deliver multiple chunks ending
// with one where IsFinal=true.
//
// The returned channel is closed when the backend finishes or ctx is
// cancelled. Errors are delivered on the channel via
// TranscriptChunk.Err and terminate the stream; the implementation
// must not send further chunks after an error.
type Transcriber interface {
	Transcribe(ctx context.Context, audio io.Reader) (<-chan TranscriptChunk, error)
}

// TranscriptChunk is a single emission from a Transcriber.
type TranscriptChunk struct {
	// Text is the transcribed text for this chunk.
	Text string
	// IsFinal is true on the last chunk of the stream.
	IsFinal bool
	// Offset is the start position of this chunk relative to the
	// beginning of the audio stream.
	Offset time.Duration
	// Language is the detected or configured language for this chunk.
	Language string
	// Err is non-nil when a backend encountered a failure. When set,
	// the chunk marks the end of the stream; no further chunks follow.
	Err error
}

// Config is the runtime configuration for a Transcriber backend. It
// deliberately does not depend on pkg/yap/config — that package is
// the on-disk schema, this one is the runtime library. Conversion
// between them lives in the caller (e.g. the daemon).
type Config struct {
	// APIURL is the full endpoint URL for remote backends. Backends
	// may default this when empty.
	APIURL string
	// APIKey is the bearer token for remote backends.
	APIKey string
	// Model is the model identifier (e.g. "whisper-large-v3-turbo").
	Model string
	// Language is an ISO language code. Empty means auto-detect.
	Language string
	// Prompt is an optional context hint forwarded to backends that
	// support it.
	Prompt string
	// ModelPath points at a local model file. Used by whisperlocal;
	// ignored by remote backends.
	ModelPath string
	// WhisperServerPath points at a whisper.cpp `whisper-server`
	// binary. Used only by the whisperlocal backend; ignored by
	// remote backends. When empty, whisperlocal falls back to the
	// $YAP_WHISPER_SERVER environment variable, then $PATH lookup,
	// then a Nix profile fallback.
	WhisperServerPath string
	// Timeout is the per-request timeout. Zero means the backend's
	// default. Ignored when HTTPClient is set.
	Timeout time.Duration
	// HTTPClient is an optional HTTP client. When nil, backends
	// construct one from Timeout. Tests inject httptest clients here.
	HTTPClient *http.Client
}

// Factory constructs a Transcriber from a Config. Implementations
// should validate the Config and return a non-nil error when any
// required field is missing.
type Factory func(cfg Config) (Transcriber, error)

// ErrUnknownBackend is returned by Get when a name is not registered.
var ErrUnknownBackend = errors.New("transcribe: unknown backend")

// registry holds the backend name → factory mapping. It is append-only
// once populated from init() functions; Register panics on duplicate
// or invalid input so registration errors surface at program start
// rather than at runtime.
var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register installs a backend factory under name. Panics if name is
// empty, f is nil, or name is already registered. Safe to call from
// init(). The registry is append-only; there is no Unregister.
func Register(name string, f Factory) {
	if name == "" {
		panic("transcribe.Register: empty name")
	}
	if f == nil {
		panic("transcribe.Register: nil factory")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		panic(fmt.Sprintf("transcribe.Register: backend %q already registered", name))
	}
	registry[name] = f
}

// Get returns the factory for name. Returns an error wrapping
// ErrUnknownBackend when no backend is registered under that name.
// The returned error lists every currently registered backend to aid
// diagnostics.
//
// pkg/yap/transform.Get has the same shape and the same error
// formatting; if you change the error format here you must also
// update transform.Get to keep them in sync. The duplication is
// intentional — extracting a generic registry would couple the two
// otherwise-independent backend lists.
func Get(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %v)", ErrUnknownBackend, name, backendsLocked())
	}
	return f, nil
}

// Backends returns a sorted slice of registered backend names.
func Backends() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return backendsLocked()
}

// backendsLocked returns the sorted backend name list. Callers must
// hold registryMu.
func backendsLocked() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
