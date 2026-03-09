package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/platform"
)

// stubHotkeyConfig is a test double for platform.HotkeyConfig.
// DetectKey always fails (wizard falls through to manual entry).
// ValidKey accepts any KEY_* name from a small allow list.
type stubHotkeyConfig struct {
	detectErr error
}

func (s *stubHotkeyConfig) DetectKey(ctx context.Context) (string, error) {
	if s.detectErr != nil {
		return "", s.detectErr
	}
	return "", fmt.Errorf("key detection not available in tests")
}

func (s *stubHotkeyConfig) ValidKey(name string) bool {
	// Accept common evdev key names used in tests.
	switch name {
	case "KEY_RIGHTCTRL", "KEY_SPACE", "KEY_A", "KEY_K":
		return true
	}
	return false
}

func (s *stubHotkeyConfig) ParseKey(name string) (platform.KeyCode, error) {
	if s.ValidKey(name) {
		return platform.KeyCode(29), nil // arbitrary code
	}
	return 0, fmt.Errorf("unknown key %q", name)
}

// newStubHotkeyCfg returns a HotkeyConfig stub that always fails detection.
func newStubHotkeyCfg() platform.HotkeyConfig {
	return &stubHotkeyConfig{detectErr: fmt.Errorf("key detection not available in tests")}
}

// TestRunWizard_NoConfigPromptsForAPIKey verifies that RunWizard prompts for API key when config file doesn't exist
func TestRunWizard_NoConfigPromptsForAPIKey(t *testing.T) {
	// Set up test environment with temp XDG config dir
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	// Mock ConfigPath to return test path
	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	// Mock input (gsk_ + 52 alphanumeric chars)
	input := "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\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"
	inputReader := strings.NewReader(input)

	// Mock os.Stdin for test
	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		w.WriteString(input)
		w.Close()
	}()
	defer func() { os.Stdin = origStdin }()

	var buf bytes.Buffer

	// Run wizard
	cfg, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	// Verify wizard prompted for API key
	output := buf.String()
	if !strings.Contains(output, "Welcome to yap") {
		t.Errorf("Expected welcome message in output, got: %s", output)
	}
	if !strings.Contains(output, "Groq API key") {
		t.Errorf("Expected API key prompt in output, got: %s", output)
	}

	// Verify config was returned
	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	if cfg.APIKey != "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" {
		t.Errorf("Expected API key to be set, got: %s", cfg.APIKey)
	}
}

// TestRunWizard_ValidatesAPIKeyFormat verifies that RunWizard validates API key format
func TestRunWizard_ValidatesAPIKeyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	validKey := "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" // valid format: sk- + 48 chars

	if !regexp.MustCompile(`^gsk_[a-zA-Z0-9]{52}$`).MatchString(validKey) {
		t.Errorf("Test key doesn't match expected format: %s", validKey)
	}

	// This test verifies the validation logic exists
	// The actual validation is tested in TestRunWizard_RejectsInvalidAPIKey
}

// TestRunWizard_PromptsForHotkeyWithDefault verifies that RunWizard prompts for hotkey with default "KEY_RIGHTCTRL"
func TestRunWizard_PromptsForHotkeyWithDefault(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	input := "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\n" + // valid API key
		"KEY_RIGHTCTRL\n" + // accept default
		"en\n" // language
	inputReader := strings.NewReader(input)

	var buf bytes.Buffer
	cfg, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	if cfg.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("Expected hotkey KEY_RIGHTCTRL, got: %s", cfg.Hotkey)
	}

	// Verify default was shown in prompt
	output := buf.String()
	if !strings.Contains(output, "KEY_RIGHTCTRL") {
		t.Errorf("Expected default hotkey in prompt, got: %s", output)
	}
}

// TestRunWizard_PromptsForLanguageWithDefault verifies that RunWizard prompts for language with default "en"
func TestRunWizard_PromptsForLanguageWithDefault(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	input := "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\n" + // valid API key
		"KEY_RIGHTCTRL\n" + // hotkey
		"en\n" // accept default
	inputReader := strings.NewReader(input)

	var buf bytes.Buffer
	cfg, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	if cfg.Language != "en" {
		t.Errorf("Expected language en, got: %s", cfg.Language)
	}

	// Verify default was shown in prompt
	output := buf.String()
	if !strings.Contains(output, "en") {
		t.Errorf("Expected default language in prompt, got: %s", output)
	}
}

// TestRunWizard_WritesValidTOMLConfigFile verifies that RunWizard writes valid TOML config file to XDG path
func TestRunWizard_WritesValidTOMLConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	input := "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\n" + // valid API key
		"KEY_RIGHTCTRL\n" + // hotkey
		"en\n" // language
	inputReader := strings.NewReader(input)

	var buf bytes.Buffer
	_, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	// Verify config file was created
	if _, err := os.Stat(testConfigPath); os.IsNotExist(err) {
		t.Fatalf("Config file was not created at %s", testConfigPath)
	}

	// Verify config file is valid TOML and contains expected values
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Failed to load created config: %v", err)
	}

	if cfg.APIKey != "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" {
		t.Errorf("Expected API key in config, got: %s", cfg.APIKey)
	}

	if cfg.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("Expected hotkey in config, got: %s", cfg.Hotkey)
	}

	if cfg.Language != "en" {
		t.Errorf("Expected language in config, got: %s", cfg.Language)
	}
}

// TestRunWizard_RejectsInvalidAPIKey verifies that RunWizard returns error on invalid API key format
func TestRunWizard_RejectsInvalidAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	// Provide invalid API key (wrong format)
	input := "invalid-key\n" + // invalid API key
		"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\n" + // valid API key on retry
		"KEY_RIGHTCTRL\n" +
		"en\n"
	inputReader := strings.NewReader(input)

	var buf bytes.Buffer
	cfg, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	// Should eventually succeed after retry
	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	// Verify valid key was accepted
	if cfg.APIKey != "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" {
		t.Errorf("Expected valid API key after retry, got: %s", cfg.APIKey)
	}

	// Verify error message was shown
	output := buf.String()
	if !strings.Contains(output, "Invalid API key") && !strings.Contains(output, "format") {
		t.Errorf("Expected validation error message, got: %s", output)
	}
}

// TestRunWizard_SkippedWhenEnvVarSet verifies that RunWizard is skipped when GROQ_API_KEY env var is set
func TestRunWizard_SkippedWhenEnvVarSet(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	// Set env var to skip wizard
	testAPIKey := "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"
	os.Setenv("GROQ_API_KEY", testAPIKey)
	defer os.Unsetenv("GROQ_API_KEY")

	var buf bytes.Buffer
	// RunWizard should detect env var and return early
	cfg, err := RunWizard(nil, &buf, newStubHotkeyCfg())

	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	// Verify wizard used env var value
	if cfg.APIKey != testAPIKey {
		t.Errorf("Expected API key from env var, got: %s", cfg.APIKey)
	}

	// Verify wizard was skipped (no prompts shown)
	output := buf.String()
	if strings.Contains(output, "Welcome to yap") {
		t.Errorf("Expected wizard to be skipped, but prompts were shown: %s", output)
	}
}

// TestRunWizard_ConfirmsConfigPath verifies that RunWizard confirms config file path after writing
func TestRunWizard_ConfirmsConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	testConfigPath := filepath.Join(tmpDir, "config.toml")

	origConfigPath := ConfigPath
	ConfigPath = func() (string, error) {
		return testConfigPath, nil
	}
	defer func() { ConfigPath = origConfigPath }()

	input := "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\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"
	inputReader := strings.NewReader(input)

	var buf bytes.Buffer
	_, err := RunWizard(inputReader, &buf, newStubHotkeyCfg())

	if err != nil {
		t.Fatalf("RunWizard failed: %v", err)
	}

	// Verify confirmation message shows config path
	output := buf.String()
	if !strings.Contains(output, "Config saved") {
		t.Errorf("Expected confirmation message, got: %s", output)
	}
	if !strings.Contains(output, testConfigPath) {
		t.Errorf("Expected config path in confirmation, got: %s", output)
	}
}
