package hint

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// Bundle carries the two orthogonal context layers assembled by the
// daemon before each recording. Vocabulary feeds the Whisper prompt
// (lexical bias); Conversation feeds the transform context (intent
// grounding). Both fields are optional: the pipeline still benefits
// from whichever layer is present.
type Bundle struct {
	// Vocabulary is project-level text (CLAUDE.md, AGENTS.md,
	// README.md contents) that biases Whisper's token probabilities
	// toward domain terms. Populated by the daemon's base vocabulary
	// layer via ReadVocabularyFiles, not by individual providers.
	Vocabulary string
	// Conversation is app-specific state (session JSONL, tmux
	// scrollback) that grounds the transform LLM. Populated by the
	// matched hint provider's Fetch method.
	Conversation string
	// Source is the Name() of the provider that produced the
	// Conversation field. Empty when no provider matched.
	Source string
}

// Config carries construction-time parameters for a Provider.
type Config struct {
	// RootPath is the working directory for file lookups. Providers
	// that need to resolve paths (e.g. Claude Code session directory)
	// use this as the base.
	RootPath string
}

// Provider is the interface hint providers implement. Each provider
// knows how to detect whether it applies to a given Target and, when
// it does, fetch conversation context from the focused application.
type Provider interface {
	// Name returns a stable identifier for this provider (e.g.
	// "claudecode", "termscroll"). Used in config, logs, and
	// Bundle.Source.
	Name() string
	// Supports returns true when this provider can produce useful
	// context for the given target.
	Supports(target inject.Target) bool
	// Fetch retrieves conversation context for the given target.
	// Returns a Bundle with at least Conversation populated on
	// success. An empty Bundle (not an error) signals "provider
	// matched but found nothing useful". Errors are non-fatal to
	// the pipeline: the daemon logs them and tries the next provider.
	Fetch(ctx context.Context, target inject.Target) (Bundle, error)
}

// Factory constructs a Provider from a Config. Implementations should
// validate the Config and return a non-nil error when any required
// field is missing.
type Factory func(cfg Config) (Provider, error)

// ErrUnknownProvider is returned by Get when a name is not registered.
var ErrUnknownProvider = errors.New("hint: unknown provider")

// registry holds the provider name -> factory mapping. It is
// append-only once populated from init() functions; Register panics
// on duplicate or invalid input so registration errors surface at
// program start rather than at runtime.
var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register installs a provider factory under name. Panics if name is
// empty, f is nil, or name is already registered. Safe to call from
// init(). The registry is append-only; there is no Unregister.
func Register(name string, f Factory) {
	if name == "" {
		panic("hint.Register: empty name")
	}
	if f == nil {
		panic("hint.Register: nil factory")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		panic(fmt.Sprintf("hint.Register: provider %q already registered", name))
	}
	registry[name] = f
}

// Get returns the factory for name. Returns an error wrapping
// ErrUnknownProvider when no provider is registered under that name.
// The returned error lists every currently registered provider to aid
// diagnostics.
func Get(name string) (Factory, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %v)", ErrUnknownProvider, name, providersLocked())
	}
	return f, nil
}

// Providers returns a sorted slice of registered provider names.
func Providers() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return providersLocked()
}

// providersLocked returns the sorted provider name list. Callers must
// hold registryMu.
func providersLocked() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
