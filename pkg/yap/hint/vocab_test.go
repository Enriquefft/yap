package hint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/hint"
)

func TestReadVocabularyFiles_BasicProject(t *testing.T) {
	// Set up a fake git repo with CLAUDE.md and README.md.
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("yap is a voice tool"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# yap\nVoice-to-text"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	got := hint.ReadVocabularyFiles(root, []string{"CLAUDE.md", "AGENTS.md", "README.md"})
	if !strings.Contains(got, "yap") {
		t.Errorf("missing 'yap' term in %q", got)
	}
	if !strings.Contains(got, "Voice-to-text") {
		t.Errorf("missing 'Voice-to-text' term in %q", got)
	}
	if strings.Contains(got, "#") {
		t.Errorf("markdown heading should be stripped in %q", got)
	}
	if strings.Contains(got, " is ") {
		t.Errorf("stopwords should be filtered in %q", got)
	}
}

func TestReadVocabularyFiles_WalksUpToGitRoot(t *testing.T) {
	// repo/
	//   .git/
	//   CLAUDE.md
	//   sub/
	//     (startDir - no files here)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("root level"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got := hint.ReadVocabularyFiles(sub, []string{"CLAUDE.md"})
	if !strings.Contains(got, "root") && !strings.Contains(got, "level") {
		t.Errorf("expected terms from root CLAUDE.md in %q", got)
	}
}

func TestReadVocabularyFiles_StopsAtGitRoot(t *testing.T) {
	// outer/
	//   CLAUDE.md  <- should NOT be included
	//   inner/
	//     .git/
	//     (startDir)
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "CLAUDE.md"), []byte("outer content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	got := hint.ReadVocabularyFiles(inner, []string{"CLAUDE.md"})
	if got != "" {
		t.Errorf("expected empty (no CLAUDE.md inside git root), got %q", got)
	}
}

func TestReadVocabularyFiles_EmptyFilenames(t *testing.T) {
	got := hint.ReadVocabularyFiles(t.TempDir(), nil)
	if got != "" {
		t.Errorf("expected empty for nil filenames, got %q", got)
	}
	got = hint.ReadVocabularyFiles(t.TempDir(), []string{})
	if got != "" {
		t.Errorf("expected empty for empty filenames, got %q", got)
	}
}

func TestReadVocabularyFiles_NoFilesFound(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := hint.ReadVocabularyFiles(root, []string{"NONEXISTENT.md"})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestReadVocabularyFiles_SkipsEmptyFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("  \n  "), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := hint.ReadVocabularyFiles(root, []string{"CLAUDE.md"})
	if got != "" {
		t.Errorf("expected empty for whitespace-only file, got %q", got)
	}
}
