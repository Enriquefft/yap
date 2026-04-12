package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/internal/platform"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// TestInit_HeuristicExtraction exercises `yap init` without --ai:
// creates a fixture README, runs init, and asserts .yap.toml contains
// extracted terms.
func TestInit_HeuristicExtraction(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	// Create a fake git repo with a README.
	gitDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(gitDir, "README.md")
	if err := os.WriteFile(readme, []byte(`# yaptest
yaptest is a voice-to-text tool using whisperlocal and Groq.
It supports OSC52 injection and wtype for Wayland.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Config points to README.md for vocabulary.
	writeConfigFile(t, cfgFile, "[hint]\nvocabulary_files = [\"README.md\"]\n")

	// Change to the project directory so init writes .yap.toml there.
	origDir, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	stdout, _, err := runCLIWithPlatform(t, p, "init")
	if err != nil {
		t.Fatalf("yap init: %v", err)
	}

	// Assert output mentions the file and terms.
	if !strings.Contains(stdout, ".yap.toml") {
		t.Errorf("stdout should mention .yap.toml, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "yaptest") {
		t.Errorf("stdout should contain 'yaptest', got:\n%s", stdout)
	}

	// Read the generated .yap.toml.
	data, err := os.ReadFile(filepath.Join(gitDir, ".yap.toml"))
	if err != nil {
		t.Fatalf("read .yap.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "vocabulary_terms") {
		t.Errorf(".yap.toml missing vocabulary_terms, got:\n%s", content)
	}
	if !strings.Contains(content, "yaptest") {
		t.Errorf(".yap.toml missing 'yaptest' term, got:\n%s", content)
	}
}

// TestInit_PreservesExistingFields asserts that running `yap init` on
// a directory that already has a .yap.toml preserves other fields
// while updating vocabulary_terms.
func TestInit_PreservesExistingFields(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	// Create a fake git repo.
	gitDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "README.md"), []byte("# myproject\nmyproject uses custom-api"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-existing .yap.toml with vocabulary_max_chars override.
	// Note: vocabulary_files is NOT overridden here — the global
	// config's ["README.md"] is used for extraction.
	existing := `[hint]
vocabulary_max_chars = 500
`
	if err := os.WriteFile(filepath.Join(gitDir, ".yap.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	writeConfigFile(t, cfgFile, "[hint]\nvocabulary_files = [\"README.md\"]\n")

	origDir, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	_, _, err := runCLIWithPlatform(t, p, "init")
	if err != nil {
		t.Fatalf("yap init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(gitDir, ".yap.toml"))
	if err != nil {
		t.Fatalf("read .yap.toml: %v", err)
	}
	content := string(data)

	// Existing fields preserved.
	if !strings.Contains(content, "500") {
		t.Errorf("existing vocabulary_max_chars should be preserved, got:\n%s", content)
	}

	// New field added.
	if !strings.Contains(content, "vocabulary_terms") {
		t.Errorf("vocabulary_terms should be added, got:\n%s", content)
	}
}

// TestInit_OverwritesExistingTerms asserts that running `yap init`
// again replaces the vocabulary_terms with fresh extraction.
func TestInit_OverwritesExistingTerms(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	gitDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "README.md"), []byte("# freshproject\nfreshproject does things"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-existing .yap.toml with old vocabulary_terms.
	existing := `[hint]
vocabulary_terms = ["old_term", "stale_word"]
`
	if err := os.WriteFile(filepath.Join(gitDir, ".yap.toml"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	writeConfigFile(t, cfgFile, "[hint]\nvocabulary_files = [\"README.md\"]\n")

	origDir, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	_, _, err := runCLIWithPlatform(t, p, "init")
	if err != nil {
		t.Fatalf("yap init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(gitDir, ".yap.toml"))
	if err != nil {
		t.Fatalf("read .yap.toml: %v", err)
	}
	content := string(data)

	// Old terms should be gone.
	if strings.Contains(content, "old_term") {
		t.Errorf("old vocabulary_terms should be overwritten, got:\n%s", content)
	}
	if strings.Contains(content, "stale_word") {
		t.Errorf("old vocabulary_terms should be overwritten, got:\n%s", content)
	}

	// New terms should be present.
	if !strings.Contains(content, "freshproject") {
		t.Errorf("new terms should include 'freshproject', got:\n%s", content)
	}
}

// TestInit_AI_NoBackend asserts that --ai errors when no transform
// backend is configured.
func TestInit_AI_NoBackend(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	gitDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "README.md"), []byte("# test\ntest content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Default config has passthrough backend.
	writeConfigFile(t, cfgFile, "")

	origDir, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	_, _, err := runCLIWithPlatform(t, p, "init", "--ai")
	if err == nil {
		t.Fatal("expected error for --ai without backend")
	}
	if !strings.Contains(err.Error(), "configured transform backend") {
		t.Errorf("error should mention transform backend, got: %v", err)
	}
}

// TestInit_NoVocabFiles asserts graceful output when no vocabulary
// files exist.
func TestInit_NoVocabFiles(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	gitDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(filepath.Join(gitDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeConfigFile(t, cfgFile, "[hint]\nvocabulary_files = [\"NONEXISTENT.md\"]\n")

	origDir, _ := os.Getwd()
	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	stdout, _, err := runCLIWithPlatform(t, p, "init")
	if err != nil {
		t.Fatalf("yap init: %v", err)
	}
	if !strings.Contains(stdout, "no terms extracted") {
		t.Errorf("expected 'no terms extracted' message, got:\n%s", stdout)
	}

	// No .yap.toml should be created.
	if _, err := os.Stat(filepath.Join(gitDir, ".yap.toml")); err == nil {
		t.Error(".yap.toml should not be created when no terms found")
	}
}
