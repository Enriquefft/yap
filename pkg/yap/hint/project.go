package hint

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// projectFile is the on-disk schema for .yap.toml. Only the [hint]
// section is supported — other config sections are silently ignored
// so the file stays focused on project-level customization.
type projectFile struct {
	Hint *projectHint `toml:"hint"`
}

// projectHint mirrors the fields of pkg/yap/config.HintConfig with
// pointer types so we can distinguish "not set" from "set to zero".
type projectHint struct {
	Enabled              *bool     `toml:"enabled"`
	VocabularyFiles      *[]string `toml:"vocabulary_files"`
	Providers            *[]string `toml:"providers"`
	VocabularyMaxChars   *int      `toml:"vocabulary_max_chars"`
	ConversationMaxChars *int      `toml:"conversation_max_chars"`
	TimeoutMS            *int      `toml:"timeout_ms"`
}

// ProjectOverrides holds per-project hint overrides parsed from
// .yap.toml. Nil pointer fields mean "not overridden, keep global
// default." The caller is responsible for merging these into the
// active HintConfig.
type ProjectOverrides struct {
	Enabled              *bool
	VocabularyFiles      *[]string
	Providers            *[]string
	VocabularyMaxChars   *int
	ConversationMaxChars *int
	TimeoutMS            *int
}

// LoadProjectOverrides walks from startDir to the git root (or
// filesystem root) looking for .yap.toml. Returns the parsed
// overrides from the first file found. Returns a zero-value
// ProjectOverrides (all nil) when no file exists — this is not an
// error. Malformed files return an error.
func LoadProjectOverrides(startDir string) (ProjectOverrides, error) {
	dir := startDir
	for {
		p := filepath.Join(dir, ".yap.toml")
		data, err := os.ReadFile(p)
		if err == nil {
			return parseProjectFile(data)
		}

		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ProjectOverrides{}, nil
}

func parseProjectFile(data []byte) (ProjectOverrides, error) {
	var pf projectFile
	if err := toml.Unmarshal(data, &pf); err != nil {
		return ProjectOverrides{}, err
	}
	if pf.Hint == nil {
		return ProjectOverrides{}, nil
	}
	return ProjectOverrides{
		Enabled:              pf.Hint.Enabled,
		VocabularyFiles:      pf.Hint.VocabularyFiles,
		Providers:            pf.Hint.Providers,
		VocabularyMaxChars:   pf.Hint.VocabularyMaxChars,
		ConversationMaxChars: pf.Hint.ConversationMaxChars,
		TimeoutMS:            pf.Hint.TimeoutMS,
	}, nil
}
