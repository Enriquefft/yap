package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Enriquefft/yap/internal/config"
	"github.com/Enriquefft/yap/internal/daemon"
	"github.com/Enriquefft/yap/internal/platform"
	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	"github.com/Enriquefft/yap/pkg/yap/transform"
	"github.com/spf13/cobra"
)

// newInitCmd builds the `yap init` command that generates a per-project
// .yap.toml with extracted vocabulary terms for Whisper speech
// recognition biasing. The command is idempotent: running it again
// overwrites vocabulary_terms while preserving other .yap.toml fields.
func newInitCmd(cfg *config.Config, _ platform.Platform) *cobra.Command {
	var useAI bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "generate .yap.toml with project vocabulary for speech recognition",
		Long: `init reads project documentation and extracts domain-specific terms
that Whisper needs help recognizing. The terms are written to .yap.toml
in the current directory.

Without --ai, terms are extracted via heuristic filtering (stopwords,
deduplication). With --ai, the configured transform backend (Ollama,
OpenAI) picks the most significant terms via a single LLM call.

Run this once per project. Review and edit .yap.toml as needed.
The file can be committed to the repository.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, cfg, useAI)
		},
	}
	cmd.Flags().BoolVar(&useAI, "ai", false, "use LLM to extract terms (requires configured transform backend)")
	return cmd
}

// runInit is the implementation of `yap init`. It reads project
// documentation, extracts terms (heuristic or LLM), and writes them
// to .yap.toml in the current directory.
func runInit(cmd *cobra.Command, cfg *config.Config, useAI bool) error {
	out := cmd.OutOrStdout()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("init: getwd: %w", err)
	}

	// Find the git root by walking up from cwd.
	gitRoot := findGitRoot(cwd)

	startDir := cwd
	if gitRoot != "" {
		startDir = gitRoot
	}

	// Apply per-project .yap.toml overrides so vocabulary_files
	// reflects any project-level customization.
	vocabFiles := cfg.Hint.VocabularyFiles
	if ov, err := hint.LoadProjectOverrides(startDir); err == nil {
		if ov.VocabularyFiles != nil {
			vocabFiles = *ov.VocabularyFiles
		}
	}
	if len(vocabFiles) == 0 {
		vocabFiles = []string{"CLAUDE.md", "AGENTS.md", "README.md"}
	}

	var terms []string

	if useAI {
		terms, err = extractTermsAI(cmd, cfg, startDir, vocabFiles)
		if err != nil {
			return fmt.Errorf("init: ai extraction: %w", err)
		}
	} else {
		terms, err = extractTermsHeuristic(startDir, vocabFiles)
		if err != nil {
			return fmt.Errorf("init: heuristic extraction: %w", err)
		}
	}

	if len(terms) == 0 {
		fmt.Fprintln(out, "init: no terms extracted from project documentation")
		fmt.Fprintln(out, "      ensure vocabulary_files exist in the project root")
		return nil
	}

	// Write .yap.toml in cwd.
	tomlPath := filepath.Join(cwd, ".yap.toml")
	if err := writeProjectToml(tomlPath, terms); err != nil {
		return fmt.Errorf("init: write %s: %w", tomlPath, err)
	}

	// Print the result.
	fmt.Fprintf(out, "wrote %s\n", tomlPath)
	fmt.Fprintf(out, "terms (%d): %s\n", len(terms), strings.Join(terms, ", "))
	return nil
}

// findGitRoot walks from dir up to the filesystem root looking for a
// .git entry. Returns the directory containing .git, or "" if none
// found.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// extractTermsHeuristic uses the existing stopword + deduplication
// pipeline to extract terms from vocabulary files.
func extractTermsHeuristic(startDir string, vocabFiles []string) ([]string, error) {
	csv := hint.ReadVocabularyFiles(startDir, vocabFiles)
	if csv == "" {
		return nil, nil
	}
	return splitTerms(csv), nil
}

// extractTermsAI sends project documentation to the configured
// transform backend and parses the comma-separated term list from the
// LLM response.
func extractTermsAI(cmd *cobra.Command, cfg *config.Config, startDir string, vocabFiles []string) ([]string, error) {
	tc := cfg.Transform
	if tc.Backend == "" || tc.Backend == "passthrough" {
		return nil, fmt.Errorf(
			"yap init --ai requires a configured transform backend; " +
				"set transform.backend and transform.model in your config, " +
				"or run yap init without --ai")
	}

	raw := hint.ReadRawVocabularyFiles(startDir, vocabFiles)
	if raw == "" {
		return nil, nil
	}

	// Cap at 4000 chars to stay within typical context windows.
	const maxDocChars = 4000
	if len(raw) > maxDocChars {
		raw = raw[:maxDocChars]
	}

	prompt := `You are configuring Whisper speech recognition. Whisper has a "prompt" parameter that biases token probabilities toward specific words.

Extract 10-15 project-specific terms from the documentation below. These terms will be injected into Whisper's prompt so it recognizes them during voice dictation.

INCLUDE (words Whisper would likely MISRECOGNIZE without help):
- The project name (MOST IMPORTANT — always first)
- Custom tool names, internal library names, invented words
- Project-specific acronyms and compound terms
- Proper nouns unique to this project

EXCLUDE (words Whisper already knows from training data):
- Common English words (lightweight, daemon, record, transcribe, hotkey)
- Well-known tech terms (API, HTTP, JSON, CLI, Docker, Git)
- Popular language/framework names (Go, Python, React, Linux, macOS)
- Generic programming concepts (function, variable, config, error)

Rule: if you'd find the word in a general-purpose dictionary or on the first page of a popular tech tutorial, Whisper already knows it. Don't include it.

Output ONLY a comma-separated list. No explanations, no quotes, no numbering.

Documentation:
` + raw

	// Build a fresh transform config with our extraction prompt as
	// the system prompt.
	extractCfg := cfg.Transform
	extractCfg.SystemPrompt = prompt
	extractCfg.Enabled = true

	tr, err := daemon.NewTransformer(extractCfg)
	if err != nil {
		return nil, fmt.Errorf("build transformer: %w", err)
	}
	defer closeIfCloser(tr, "init-transformer")

	// Send a single empty-ish user message; the system prompt carries
	// the real payload. Some backends require non-empty user input.
	in := make(chan transcribe.TranscriptChunk, 1)
	in <- transcribe.TranscriptChunk{Text: "Extract terms.", IsFinal: true}
	close(in)

	outCh, err := tr.Transform(cmd.Context(), in, transform.Options{})
	if err != nil {
		return nil, fmt.Errorf("transform: %w", err)
	}

	var result strings.Builder
	for chunk := range outCh {
		if chunk.Err != nil {
			return nil, fmt.Errorf("transform chunk: %w", chunk.Err)
		}
		result.WriteString(chunk.Text)
	}

	return splitTerms(result.String()), nil
}

// splitTerms splits a comma-separated string into unique, trimmed,
// non-empty terms.
func splitTerms(csv string) []string {
	parts := strings.Split(csv, ",")
	seen := map[string]struct{}{}
	var terms []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		terms = append(terms, t)
	}
	return terms
}

// initProjectFile is the TOML structure for .yap.toml as written by
// `yap init`. It mirrors hint.projectFile but uses concrete types for
// writing (the hint package uses pointer types for optional-field
// detection on read).
type initProjectFile struct {
	Hint initProjectHint `toml:"hint"`
}

// initProjectHint carries the [hint] section fields for .yap.toml
// writing. Fields use pointer types so that omitempty/omitzero works:
// nil pointer fields are omitted from the TOML output, preserving only
// the fields we explicitly set.
type initProjectHint struct {
	Enabled              *bool     `toml:"enabled,omitempty"`
	VocabularyFiles      *[]string `toml:"vocabulary_files,omitempty"`
	VocabularyTerms      *[]string `toml:"vocabulary_terms,omitempty"`
	Providers            *[]string `toml:"providers,omitempty"`
	VocabularyMaxChars   *int      `toml:"vocabulary_max_chars,omitempty"`
	ConversationMaxChars *int      `toml:"conversation_max_chars,omitempty"`
	TimeoutMS            *int      `toml:"timeout_ms,omitempty"`
}

// writeProjectToml reads the existing .yap.toml (if any), updates the
// vocabulary_terms field, and writes back. Other fields are preserved.
func writeProjectToml(path string, terms []string) error {
	var pf initProjectFile

	// Read existing file if present.
	data, err := os.ReadFile(path)
	if err == nil {
		if _, err := toml.Decode(string(data), &pf); err != nil {
			return fmt.Errorf("parse existing %s: %w", path, err)
		}
	}

	// Set the vocabulary_terms field.
	pf.Hint.VocabularyTerms = &terms

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(pf); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

