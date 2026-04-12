package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// KeyValidator is the structural interface the validator needs from
// the platform layer to check hotkey segments. internal/platform's
// HotkeyConfig satisfies it without any import in this direction.
type KeyValidator interface {
	ValidKey(name string) bool
}

// Allowed enum values. Centralized so the validator and CLI completion
// share one definition. Each accessor returns a fresh slice on every
// call so the package holds zero mutable global state. The functions
// double as the package's "what is allowed?" surface for the NixOS
// generator and CLI completion.

// ValidModes returns the allowed values for general.mode.
func ValidModes() []string { return []string{"hold", "toggle"} }

// ValidBackends returns the allowed values for transcription.backend.
func ValidBackends() []string {
	return []string{"whisperlocal", "groq", "openai", "custom"}
}

// ValidTransformBackends returns the allowed values for transform.backend.
func ValidTransformBackends() []string {
	return []string{"passthrough", "local", "openai"}
}

// ValidElectronStrategies returns the allowed values for
// injection.electron_strategy.
func ValidElectronStrategies() []string {
	return []string{"clipboard", "keystroke"}
}

// ValidHintProviders returns the allowed values for hint.providers.
// This list is maintained in the config package (not imported from
// pkg/yap/hint) to avoid a dependency cycle. It must be kept in
// lockstep with the providers registered in pkg/yap/hint.
func ValidHintProviders() []string {
	return []string{"claudecode", "tmuxpane"}
}

// ValidInjectionStrategies returns the allowed values for
// injection.app_overrides[].strategy and injection.default_strategy.
// The list MUST match the Name() values returned by the registered
// Linux strategies in internal/platform/linux/inject. Keeping one
// list is the single-source-of-truth rule: if a new strategy is
// added (or removed) on the platform side, this list has to move
// in lockstep.
func ValidInjectionStrategies() []string {
	return []string{"tmux", "osc52", "electron", "wayland", "x11"}
}

// Validate returns nil if cfg is internally consistent, or a joined
// error describing every violation. keyValidator may be nil, in which
// case hotkey-segment validation is skipped (useful in unit tests).
func (c Config) Validate(keyValidator KeyValidator) error {
	var errs []error

	modes := ValidModes()
	backends := ValidBackends()
	transformBackends := ValidTransformBackends()
	electronStrategies := ValidElectronStrategies()
	injectionStrategies := ValidInjectionStrategies()

	// general
	if c.General.Hotkey == "" {
		errs = append(errs, errors.New("general.hotkey: required"))
	} else if keyValidator != nil {
		for _, seg := range strings.Split(c.General.Hotkey, "+") {
			seg = strings.TrimSpace(seg)
			if !keyValidator.ValidKey(seg) {
				errs = append(errs, fmt.Errorf("general.hotkey: invalid key %q", seg))
			}
		}
	}
	if !contains(modes, c.General.Mode) {
		errs = append(errs, fmt.Errorf("general.mode: must be one of %v, got %q", modes, c.General.Mode))
	}
	if c.General.MaxDuration < 1 || c.General.MaxDuration > 300 {
		errs = append(errs, fmt.Errorf("general.max_duration: must be in [1,300], got %d", c.General.MaxDuration))
	}
	if c.General.SilenceThreshold < 0 || c.General.SilenceThreshold > 1 {
		errs = append(errs, fmt.Errorf("general.silence_threshold: must be in [0,1], got %g", c.General.SilenceThreshold))
	}
	if c.General.SilenceDuration <= 0 {
		errs = append(errs, fmt.Errorf("general.silence_duration: must be > 0, got %g", c.General.SilenceDuration))
	}

	// transcription
	if !contains(backends, c.Transcription.Backend) {
		errs = append(errs, fmt.Errorf("transcription.backend: must be one of %v, got %q", backends, c.Transcription.Backend))
	}
	if isRemoteBackend(c.Transcription.Backend) {
		u := c.Transcription.ResolvedAPIURL()
		if u == "" {
			errs = append(errs, fmt.Errorf("transcription.api_url: required for backend %q", c.Transcription.Backend))
		} else if !isValidHTTPURL(u) {
			errs = append(errs, fmt.Errorf("transcription.api_url: must be http(s) URL with a host and no whitespace, got %q", u))
		}
	}
	// English-only whisper.cpp models (name ending in ".en") cannot
	// transcribe other languages — whisper-server silently drops the
	// language hint. Reject the combination at config load so users
	// see the error up front rather than mysterious English output.
	//
	// When transcription.model_path is set the name-based check is
	// meaningless: whisperlocal.resolveModel ignores cfg.Model entirely
	// and loads the file at ModelPath directly (see
	// pkg/yap/transcribe/whisperlocal/discover.go). An air-gapped user
	// with a hand-downloaded multilingual ggml-large-v3.bin plus a
	// leftover model = "base.en" in their config is a supported setup;
	// rejecting it here would be a false positive.
	if c.Transcription.Backend == "whisperlocal" &&
		c.Transcription.ModelPath == "" &&
		strings.HasSuffix(c.Transcription.Model, ".en") &&
		c.Transcription.Language != "" &&
		c.Transcription.Language != "en" {
		errs = append(errs, fmt.Errorf(
			"transcription.language: %q is not supported with English-only model %q; use a multilingual model (base, small, medium) or set language = \"en\"",
			c.Transcription.Language, c.Transcription.Model))
	}
	if c.Transcription.WhisperThreads < 0 || c.Transcription.WhisperThreads > 64 {
		errs = append(errs, fmt.Errorf("transcription.whisper_threads: must be in [0,64], got %d", c.Transcription.WhisperThreads))
	}

	// transform
	if !contains(transformBackends, c.Transform.Backend) {
		errs = append(errs, fmt.Errorf("transform.backend: must be one of %v, got %q", transformBackends, c.Transform.Backend))
	}
	if c.Transform.Enabled && c.Transform.Backend != "passthrough" && c.Transform.Model == "" {
		errs = append(errs, errors.New("transform.model: required when transform.enabled = true and backend is not passthrough"))
	}

	// injection
	if !contains(electronStrategies, c.Injection.ElectronStrategy) {
		errs = append(errs, fmt.Errorf("injection.electron_strategy: must be one of %v, got %q", electronStrategies, c.Injection.ElectronStrategy))
	}
	// injection.default_strategy: empty string disables the wildcard
	// fallback; otherwise must name a known strategy so selection can
	// prepend it for unclassified targets.
	if c.Injection.DefaultStrategy != "" && !contains(injectionStrategies, c.Injection.DefaultStrategy) {
		errs = append(errs, fmt.Errorf("injection.default_strategy: must be empty or one of %v, got %q", injectionStrategies, c.Injection.DefaultStrategy))
	}
	for i, ov := range c.Injection.AppOverrides {
		if strings.TrimSpace(ov.Match) == "" {
			errs = append(errs, fmt.Errorf("injection.app_overrides[%d].match: required", i))
		}
		strategy := strings.TrimSpace(ov.Strategy)
		if strategy == "" {
			errs = append(errs, fmt.Errorf("injection.app_overrides[%d].strategy: required", i))
		} else if !contains(injectionStrategies, strategy) {
			errs = append(errs, fmt.Errorf("injection.app_overrides[%d].strategy: must be one of %v, got %q", i, injectionStrategies, ov.Strategy))
		}
	}

	// hint
	hintProviders := ValidHintProviders()
	if c.Hint.VocabularyMaxChars < 0 || c.Hint.VocabularyMaxChars > 8000 {
		errs = append(errs, fmt.Errorf("hint.vocabulary_max_chars: must be in [0,8000], got %d", c.Hint.VocabularyMaxChars))
	}
	if c.Hint.ConversationMaxChars < 0 || c.Hint.ConversationMaxChars > 32000 {
		errs = append(errs, fmt.Errorf("hint.conversation_max_chars: must be in [0,32000], got %d", c.Hint.ConversationMaxChars))
	}
	if c.Hint.TimeoutMS < 0 || c.Hint.TimeoutMS > 5000 {
		errs = append(errs, fmt.Errorf("hint.timeout_ms: must be in [0,5000], got %d", c.Hint.TimeoutMS))
	}
	for i, p := range c.Hint.Providers {
		if !contains(hintProviders, p) {
			errs = append(errs, fmt.Errorf("hint.providers[%d]: must be one of %v, got %q", i, hintProviders, p))
		}
	}

	return errors.Join(errs...)
}

// isValidHTTPURL reports whether u is a usable http(s) URL: it must
// parse, use the http or https scheme, contain no whitespace, and
// have a non-empty host. url.Parse alone is too permissive — it
// accepts "https://" (no host) and "https://example.com/with
// spaces" (whitespace), both of which fail at HTTP-call time with
// confusing dial errors.
func isValidHTTPURL(u string) bool {
	if strings.ContainsAny(u, " \t\n\r") {
		return false
	}
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return false
	}
	if p.Host == "" {
		return false
	}
	return true
}

// isRemoteBackend reports whether backend uses a network endpoint.
func isRemoteBackend(b string) bool {
	switch b {
	case "groq", "openai", "custom":
		return true
	}
	return false
}

// contains returns true if needle is a member of haystack.
func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
