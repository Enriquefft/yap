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
// share one definition.
var (
	validModes             = []string{"hold", "toggle"}
	validBackends          = []string{"whisperlocal", "groq", "openai", "custom"}
	validTransformBackends = []string{"passthrough", "local", "openai"}
	validElectronStrategy  = []string{"clipboard", "keystroke"}
)

// ValidModes returns the allowed values for general.mode. Returned
// slice is a fresh copy so callers cannot mutate package state.
func ValidModes() []string { return append([]string(nil), validModes...) }

// ValidBackends returns the allowed values for transcription.backend.
func ValidBackends() []string { return append([]string(nil), validBackends...) }

// ValidTransformBackends returns the allowed values for transform.backend.
func ValidTransformBackends() []string {
	return append([]string(nil), validTransformBackends...)
}

// ValidElectronStrategies returns the allowed values for
// injection.electron_strategy.
func ValidElectronStrategies() []string {
	return append([]string(nil), validElectronStrategy...)
}

// Validate returns nil if cfg is internally consistent, or a joined
// error describing every violation. keyValidator may be nil, in which
// case hotkey-segment validation is skipped (useful in unit tests).
func (c Config) Validate(keyValidator KeyValidator) error {
	var errs []error

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
	if !contains(validModes, c.General.Mode) {
		errs = append(errs, fmt.Errorf("general.mode: must be one of %v, got %q", validModes, c.General.Mode))
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
	if !contains(validBackends, c.Transcription.Backend) {
		errs = append(errs, fmt.Errorf("transcription.backend: must be one of %v, got %q", validBackends, c.Transcription.Backend))
	}
	if isRemoteBackend(c.Transcription.Backend) {
		u := c.Transcription.ResolvedAPIURL()
		if u == "" {
			errs = append(errs, fmt.Errorf("transcription.api_url: required for backend %q", c.Transcription.Backend))
		} else if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") {
			errs = append(errs, fmt.Errorf("transcription.api_url: must be http(s) URL, got %q", u))
		}
	}

	// transform
	if !contains(validTransformBackends, c.Transform.Backend) {
		errs = append(errs, fmt.Errorf("transform.backend: must be one of %v, got %q", validTransformBackends, c.Transform.Backend))
	}
	if c.Transform.Enabled && c.Transform.Backend != "passthrough" && c.Transform.Model == "" {
		errs = append(errs, errors.New("transform.model: required when transform.enabled = true and backend is not passthrough"))
	}

	// injection
	if !contains(validElectronStrategy, c.Injection.ElectronStrategy) {
		errs = append(errs, fmt.Errorf("injection.electron_strategy: must be one of %v, got %q", validElectronStrategy, c.Injection.ElectronStrategy))
	}
	for i, ov := range c.Injection.AppOverrides {
		if strings.TrimSpace(ov.Match) == "" {
			errs = append(errs, fmt.Errorf("injection.app_overrides[%d].match: required", i))
		}
		if strings.TrimSpace(ov.Strategy) == "" {
			errs = append(errs, fmt.Errorf("injection.app_overrides[%d].strategy: required", i))
		}
	}

	return errors.Join(errs...)
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
