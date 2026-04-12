package hint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectOverrides_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".yap.toml"), []byte(`
[hint]
vocabulary_files = ["GLOSSARY.md", "TERMS.md"]
vocabulary_max_chars = 2000
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ov, err := LoadProjectOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ov.VocabularyFiles == nil {
		t.Fatal("expected VocabularyFiles override")
	}
	if len(*ov.VocabularyFiles) != 2 || (*ov.VocabularyFiles)[0] != "GLOSSARY.md" {
		t.Fatalf("unexpected VocabularyFiles: %v", *ov.VocabularyFiles)
	}
	if ov.VocabularyMaxChars == nil || *ov.VocabularyMaxChars != 2000 {
		t.Fatalf("unexpected VocabularyMaxChars: %v", ov.VocabularyMaxChars)
	}
	if ov.Providers != nil {
		t.Fatal("Providers should be nil (not set)")
	}
}

func TestLoadProjectOverrides_NoFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ov, err := LoadProjectOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ov.VocabularyFiles != nil || ov.Providers != nil || ov.Enabled != nil {
		t.Fatal("expected all nil overrides")
	}
}

func TestLoadProjectOverrides_WalksUp(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".yap.toml"), []byte(`
[hint]
enabled = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	ov, err := LoadProjectOverrides(sub)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Enabled == nil || *ov.Enabled != false {
		t.Fatal("expected Enabled=false from parent .yap.toml")
	}
}

func TestLoadProjectOverrides_VocabularyTerms(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".yap.toml"), []byte(`
[hint]
vocabulary_terms = ["yap", "whisperlocal", "Groq"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ov, err := LoadProjectOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ov.VocabularyTerms == nil {
		t.Fatal("expected VocabularyTerms override")
	}
	terms := *ov.VocabularyTerms
	if len(terms) != 3 || terms[0] != "yap" || terms[1] != "whisperlocal" || terms[2] != "Groq" {
		t.Fatalf("unexpected VocabularyTerms: %v", terms)
	}
}

func TestLoadProjectOverrides_EmptyHintSection(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".yap.toml"), []byte(`
[hint]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ov, err := LoadProjectOverrides(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ov.VocabularyFiles != nil {
		t.Fatal("expected nil")
	}
}
