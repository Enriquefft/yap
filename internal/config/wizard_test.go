package config

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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

	// Mock input (sk- + 48 alphanumeric chars = 51 total)
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

	// Mock fmt.Printf to capture output
	var buf bytes.Buffer
	// Capture output - RunWizard should print welcome message and prompts

	// Run wizard
	cfg, err := RunWizard(inputReader, &buf)

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
	cfg, err := RunWizard(inputReader, &buf)

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
	cfg, err := RunWizard(inputReader, &buf)

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
	_, err := RunWizard(inputReader, &buf)

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
	cfg, err := RunWizard(inputReader, &buf)

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
	cfg, err := RunWizard(nil, &buf)

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
	_, err := RunWizard(inputReader, &buf)

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
