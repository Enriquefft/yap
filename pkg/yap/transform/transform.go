package transform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// Transformer consumes a stream of transcript chunks and emits a
// stream of (possibly rewritten) chunks. Streaming backends may emit
// chunks as they arrive; non-streaming backends accumulate until the
// final chunk and emit the result as a single IsFinal chunk.
//
// Implementations must close the output channel exactly once when
// the input channel closes, when ctx is cancelled, or when they
// deliver a terminal error chunk.
type Transformer interface {
	Transform(ctx context.Context, in <-chan transcribe.TranscriptChunk) (<-chan transcribe.TranscriptChunk, error)
}

// Checker is an optional interface implemented by backends that
// support a startup health probe. Callers (typically the daemon)
// type-assert on this interface after constructing a transformer: if
// the assertion succeeds they call HealthCheck once, log or notify
// on failure, and decide whether to proceed with the configured
// backend or fall back to passthrough.
//
// HealthCheck should be idempotent and cheap — it runs on every
// daemon startup. Implementations typically issue a single GET
// against a low-cost endpoint (Ollama's GET /, OpenAI's GET
// /v1/models) and return nil on any 2xx response.
type Checker interface {
	HealthCheck(ctx context.Context) error
}

// Config is the runtime configuration for a Transformer backend. Like
// transcribe.Config it is deliberately independent from
// pkg/yap/config: third-party library users should not have to pull
// the on-disk schema package just to run a Transformer.
type Config struct {
	// APIURL is the endpoint for remote backends.
	APIURL string
	// APIKey is the bearer token for remote backends.
	APIKey string
	// Model is the model identifier.
	Model string
	// SystemPrompt is the system prompt passed to the LLM. Defaults
	// are backend-specific.
	SystemPrompt string
}

// Factory constructs a Transformer from a Config.
type Factory func(cfg Config) (Transformer, error)

// ErrUnknownBackend is returned by Get when a name is not registered.
var ErrUnknownBackend = errors.New("transform: unknown backend")

// registry holds the backend name → factory mapping. Append-only,
// populated at init(), conceptually equivalent to a compile-time
// constant map. See pkg/yap/transcribe for the analogous pattern.
var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register installs a backend factory under name. Panics if name is
// empty, f is nil, or name is already registered.
func Register(name string, f Factory) {
	if name == "" {
		panic("transform.Register: empty name")
	}
	if f == nil {
		panic("transform.Register: nil factory")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		panic(fmt.Sprintf("transform.Register: backend %q already registered", name))
	}
	registry[name] = f
}

// Get returns the factory for name. Wraps ErrUnknownBackend when no
// backend is registered under that name.
//
// pkg/yap/transcribe.Get has the same shape and the same error
// formatting; if you change the error format here you must also
// update transcribe.Get to keep them in sync.
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
