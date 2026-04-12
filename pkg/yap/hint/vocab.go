package hint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ReadRawVocabularyFiles walks from startDir up to the git root (or
// filesystem root) and reads each named file, returning their
// concatenated content separated by "\n". Files that don't exist
// are silently skipped. Returns "" when no files are found.
//
// The walk stops at the first directory that contains a .git entry
// (file or directory) or at the filesystem root, whichever comes
// first. This ensures the vocabulary is scoped to the current
// project and does not leak terms from unrelated parent directories.
//
// ReadRawVocabularyFiles is the shared file-reading core used by both
// ReadVocabularyFiles (which extracts terms) and yap init --ai (which
// sends raw prose to an LLM).
func ReadRawVocabularyFiles(startDir string, filenames []string) string {
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

	return strings.Join(parts, "\n")
}

// ReadVocabularyFiles walks from startDir up to the git root and reads
// each named file, extracts domain-specific terms, and returns them as
// a comma-separated string. Files that don't exist are silently
// skipped. Returns "" when no files are found.
//
// ReadVocabularyFiles is a pure function with no side effects beyond
// file reads. It is used by the daemon's base vocabulary layer to
// provide always-on project-level vocabulary regardless of whether
// any hint provider matched.
func ReadVocabularyFiles(startDir string, filenames []string) string {
	raw := ReadRawVocabularyFiles(startDir, filenames)
	if raw == "" {
		return ""
	}
	return ExtractTerms(stripMarkdown(raw))
}

// Common words filtered from vocabulary. Only domain-specific terms
// should reach Whisper. This set covers English since project docs
// are typically in English; the extracted terms (project names,
// technical words) are language-neutral.
var stopwords = map[string]struct{}{
	// articles, determiners
	"a": {}, "an": {}, "the": {}, "this": {}, "that": {}, "these": {}, "those": {},
	// be verbs
	"is": {}, "are": {}, "was": {}, "were": {}, "be": {}, "been": {}, "being": {},
	// have verbs
	"have": {}, "has": {}, "had": {}, "having": {},
	// do verbs
	"do": {}, "does": {}, "did": {}, "doing": {},
	// modals
	"will": {}, "would": {}, "could": {}, "should": {}, "may": {}, "might": {},
	"can": {}, "shall": {}, "must": {},
	// conjunctions, prepositions
	"and": {}, "or": {}, "but": {}, "if": {}, "of": {}, "at": {}, "by": {},
	"for": {}, "with": {}, "about": {}, "to": {}, "from": {}, "in": {},
	"on": {}, "into": {}, "onto": {}, "upon": {}, "between": {}, "through": {},
	"during": {}, "before": {}, "after": {}, "above": {}, "below": {},
	"under": {}, "over": {}, "without": {}, "within": {}, "along": {},
	// pronouns
	"it": {}, "its": {}, "you": {}, "your": {}, "you're": {}, "we": {},
	"our": {}, "they": {}, "their": {}, "them": {}, "he": {}, "she": {},
	"his": {}, "her": {}, "my": {}, "me": {}, "us": {}, "him": {},
	// common verbs
	"use": {}, "used": {}, "uses": {}, "using": {},
	"make": {}, "made": {}, "makes": {}, "making": {},
	"get": {}, "gets": {}, "got": {}, "getting": {},
	"set": {}, "sets": {}, "setting": {},
	"see": {}, "saw": {}, "seen": {}, "seeing": {},
	"run": {}, "runs": {}, "running": {}, "ran": {},
	"take": {}, "takes": {}, "took": {}, "taken": {},
	"give": {}, "gives": {}, "gave": {}, "given": {},
	"go": {}, "goes": {}, "going": {}, "went": {}, "gone": {},
	"come": {}, "comes": {}, "came": {}, "coming": {},
	"keep": {}, "keeps": {}, "kept": {}, "keeping": {},
	"let": {}, "lets": {}, "need": {}, "needs": {}, "want": {}, "wants": {},
	"hold": {}, "holds": {}, "held": {}, "read": {}, "reads": {},
	"write": {}, "writes": {}, "show": {}, "shows": {},
	"work": {}, "works": {}, "start": {}, "stop": {},
	"add": {}, "adds": {}, "put": {}, "puts": {},
	// common adjectives/adverbs
	"not": {}, "no": {}, "so": {}, "as": {}, "than": {}, "also": {},
	"just": {}, "only": {}, "more": {}, "most": {}, "very": {},
	"new": {}, "other": {}, "like": {}, "some": {}, "such": {},
	"first": {}, "last": {}, "next": {}, "same": {}, "own": {},
	"every": {}, "each": {}, "any": {}, "all": {},
	// question/relative words
	"when": {}, "what": {}, "which": {}, "who": {}, "how": {}, "where": {},
	// misc common
	"up": {}, "out": {}, "then": {}, "there": {}, "here": {},
	"well": {}, "way": {}, "even": {}, "still": {}, "yet": {},
	"too": {}, "now": {}, "back": {}, "down": {},
	// filler words common in docs but never domain-specific
	"currently": {}, "available": {}, "provides": {},
	"based": {}, "called": {}, "including": {}, "already": {},
	"following": {}, "appears": {}, "wherever": {}, "intact": {},
	"enabled": {}, "disabled": {}, "supported": {},
	"true": {}, "false": {},
}

// ExtractTerms condenses prose into a comma-separated list of unique
// domain-specific terms. Whisper's prompt parameter works best with
// short, language-neutral terms — not full sentences in a potentially
// different language than the speech. Project names and technical
// words like "yap", "whisperlocal", "Groq" are language-independent.
func ExtractTerms(s string) string {
	words := strings.Fields(s)
	seen := map[string]struct{}{}
	var terms []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'—–-/\\|@#$%^&*~`")
		lower := strings.ToLower(w)
		if len(lower) < 2 {
			continue
		}
		// Skip pure-punctuation or pure-numeric tokens.
		allPunct := true
		for _, r := range lower {
			if r != '-' && r != '_' && r != '.' && (r < '0' || r > '9') {
				allPunct = false
				break
			}
		}
		if allPunct {
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
	s = reLink.ReplaceAllString(s, "$1")
	s = reInlineCode.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Trim(m, "`")
	})
	s = reEmphasis.ReplaceAllString(s, "$1")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = reMultiLine.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
