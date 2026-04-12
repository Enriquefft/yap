package hint

import (
	"os"
	"path/filepath"
	"regexp"
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

	return extractTerms(stripMarkdown(strings.Join(parts, "\n")))
}

// Common words filtered from vocabulary. Only domain-specific terms
// should reach Whisper. This set covers English since project docs
// are typically in English; the extracted terms (project names,
// technical words) are language-neutral.
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "could": {},
	"should": {}, "may": {}, "might": {}, "can": {}, "shall": {},
	"and": {}, "or": {}, "but": {}, "if": {}, "of": {}, "at": {}, "by": {},
	"for": {}, "with": {}, "about": {}, "to": {}, "from": {}, "in": {},
	"on": {}, "it": {}, "its": {}, "that": {}, "this": {}, "not": {},
	"no": {}, "so": {}, "as": {}, "than": {}, "when": {}, "what": {},
	"which": {}, "who": {}, "how": {}, "where": {}, "all": {}, "each": {},
	"every": {}, "any": {}, "your": {}, "you": {}, "we": {}, "our": {},
	"their": {}, "they": {}, "he": {}, "she": {}, "his": {}, "her": {},
	"up": {}, "out": {}, "into": {}, "also": {}, "just": {}, "only": {},
	"more": {}, "most": {}, "very": {}, "then": {}, "there": {},
}

// extractTerms condenses prose into a comma-separated list of unique
// domain-specific terms. Whisper's prompt parameter works best with
// short, language-neutral terms — not full sentences in a potentially
// different language than the speech. Project names and technical
// words like "yap", "whisperlocal", "Groq" are language-independent.
func extractTerms(s string) string {
	words := strings.Fields(s)
	seen := map[string]struct{}{}
	var terms []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		lower := strings.ToLower(w)
		if len(lower) < 2 {
			continue
		}
		if _, ok := stopwords[lower]; ok {
			continue
		}
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		terms = append(terms, w)
		if len(terms) >= 40 {
			break
		}
	}
	return strings.Join(terms, ", ")
}

var (
	reCodeBlock  = regexp.MustCompile("(?s)```[^`]*```")
	reHeading    = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reBullet     = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`)
	reNumbered   = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reInlineCode = regexp.MustCompile("`[^`]+`")
	reEmphasis   = regexp.MustCompile(`[*_]{1,3}([^*_]+)[*_]{1,3}`)
	reHTMLTag    = regexp.MustCompile(`<[^>]+>`)
	reMultiSpace = regexp.MustCompile(`[^\S\n]{2,}`)
	reMultiLine  = regexp.MustCompile(`\n{3,}`)
)

// stripMarkdown removes markdown formatting so the text reads as
// natural prose suitable for a Whisper prompt. Whisper's prompt
// parameter expects "previous transcript" style text — markdown
// headers, bullets, and code blocks confuse the model.
func stripMarkdown(s string) string {
	s = reCodeBlock.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllString(s, "")
	s = reBullet.ReplaceAllString(s, "")
	s = reNumbered.ReplaceAllString(s, "")
	s = reLink.ReplaceAllLiteralString(s, "$1")
	s = reInlineCode.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Trim(m, "`")
	})
	s = reEmphasis.ReplaceAllString(s, "$1")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = reMultiLine.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
