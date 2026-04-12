package config

import "github.com/Enriquefft/yap/pkg/yap/hint"

// ApplyProjectOverrides merges per-project .yap.toml overrides into
// cfg.Hint. Fields not set in the override (nil pointers) keep their
// global value. This is called by the daemon after loading the global
// config to apply project-level customization.
func ApplyProjectOverrides(cfg *Config, ov hint.ProjectOverrides) {
	if ov.Enabled != nil {
		cfg.Hint.Enabled = *ov.Enabled
	}
	if ov.VocabularyFiles != nil {
		cfg.Hint.VocabularyFiles = *ov.VocabularyFiles
	}
	if ov.Providers != nil {
		cfg.Hint.Providers = *ov.Providers
	}
	if ov.VocabularyMaxChars != nil {
		cfg.Hint.VocabularyMaxChars = *ov.VocabularyMaxChars
	}
	if ov.ConversationMaxChars != nil {
		cfg.Hint.ConversationMaxChars = *ov.ConversationMaxChars
	}
	if ov.TimeoutMS != nil {
		cfg.Hint.TimeoutMS = *ov.TimeoutMS
	}
}
