package hint

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadVocabularyFiles walks from startDir up to the git root (or
// filesystem root) and reads each named file, returning their
// concatenated content separated by "\n---\n". Files that don't exist
// are silently skipped. Returns "" when no files are found.
//
// The walk stops at the first directory that contains a .git entry
// (file or directory) or at the filesystem root, whichever comes
// first. This ensures the vocabulary is scoped to the current
// project and does not leak terms from unrelated parent directories.
//
// ReadVocabularyFiles is a pure function with no side effects beyond
// file reads. It is used by the daemon's base vocabulary layer to
// provide always-on project-level vocabulary regardless of whether
// any hint provider matched.
func ReadVocabularyFiles(startDir string, filenames []string) string {
	if len(filenames) == 0 {
		return ""
	}

	var parts []string
	seen := map[string]struct{}{}

	dir := startDir
	for {
		for _, name := range filenames {
			p := filepath.Join(dir, name)
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			content := strings.TrimSpace(string(data))
			if content != "" {
				parts = append(parts, content)
			}
		}

		// Stop if this directory contains .git (repo root).
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}

		// Walk up to the parent directory.
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root.
			break
		}
		dir = parent
	}

	return strings.Join(parts, "\n---\n")
}
