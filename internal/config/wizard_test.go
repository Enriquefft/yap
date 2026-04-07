package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/platform"
)

// stubHotkeyConfig is a test double for platform.HotkeyConfig.
// DetectKey fails so the wizard always falls back to manual entry;
// ValidKey accepts a small allow list.
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
	switch name {
	case "KEY_RIGHTCTRL", "KEY_SPACE", "KEY_A", "KEY_K", "KEY_LEFTSHIFT":
		return true
	}
	return false
}

func (s *stubHotkeyConfig) ParseKey(name string) (platform.KeyCode, error) {
	if s.ValidKey(name) {
		return platform.KeyCode(29), nil
	}
	return 0, fmt.Errorf("unknown key %q", name)
}

func newStubHotkeyCfg() platform.HotkeyConfig {
	return &stubHotkeyConfig{detectErr: fmt.Errorf("key detection not available in tests")}
}

// withTestConfigPath temporarily routes ConfigPath to a scratch file.
// Returns a cleanup callback.
func withTestConfigPath(t *testing.T, path string) func() {
	t.Helper()
	orig := ConfigPath
	ConfigPath = func() (string, error) { return path, nil }
	return func() { ConfigPath = orig }
}

func TestRunWizard_SectionAwarePrompts(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	// Clear env vars so wizard runs full prompts.
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	input := "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\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"

	var buf bytes.Buffer
	cfg, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Welcome to yap") {
		t.Errorf("expected welcome, got: %s", output)
	}
	if !strings.Contains(output, "[transcription]") {
		t.Errorf("expected [transcription] section header in prompts, got: %s", output)
	}
	if !strings.Contains(output, "[general]") {
		t.Errorf("expected [general] section header in prompts, got: %s", output)
	}

	if cfg.Transcription.APIKey != "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" {
		t.Errorf("APIKey: %s", cfg.Transcription.APIKey)
	}
	if cfg.General.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("Hotkey: %s", cfg.General.Hotkey)
	}
	if cfg.Transcription.Language != "en" {
		t.Errorf("Language: %s", cfg.Transcription.Language)
	}
	if cfg.Transcription.Backend != "groq" {
		t.Errorf("Backend should be groq, got: %s", cfg.Transcription.Backend)
	}
}

func TestRunWizard_OffersOnlyGroqBackend(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	input := "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\n" +
		"KEY_SPACE\n" +
		"\n"

	var buf bytes.Buffer
	cfg, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}

	if cfg.Transcription.Backend != "groq" {
		t.Errorf("wizard should default to groq, got %q", cfg.Transcription.Backend)
	}
	if !strings.Contains(buf.String(), "Transcription backend: groq") {
		t.Errorf("expected single-backend short-circuit message, got: %s", buf.String())
	}
}

func TestRunWizard_ValidatesAPIKeyFormat(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	// First input is invalid; wizard re-prompts and accepts the second.
	input := "invalid-key\n" +
		"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\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"

	var buf bytes.Buffer
	cfg, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}
	if cfg.Transcription.APIKey != "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" {
		t.Errorf("expected retry to accept valid key, got %q", cfg.Transcription.APIKey)
	}
	if !strings.Contains(buf.String(), "Invalid API key") {
		t.Errorf("expected validation error in output, got: %s", buf.String())
	}
}

func TestRunWizard_WritesNestedTOMLFile(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	input := "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\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"

	var buf bytes.Buffer
	if _, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg()); err != nil {
		t.Fatalf("RunWizard: %v", err)
	}

	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read wizard output: %v", err)
	}
	s := string(data)
	for _, header := range []string{"[general]", "[transcription]", "[transform]", "[injection]", "[tray]"} {
		if !strings.Contains(s, header) {
			t.Errorf("wizard output missing section %s. full:\n%s", header, s)
		}
	}
}

func TestRunWizard_SkippedWhenAPIKeyEnvSet(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	testAPIKey := "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"
	t.Setenv("YAP_API_KEY", testAPIKey)
	t.Setenv("GROQ_API_KEY", "")

	var buf bytes.Buffer
	cfg, err := RunWizard(nil, &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}

	if cfg.Transcription.APIKey != testAPIKey {
		t.Errorf("Expected API key from env var, got: %s", cfg.Transcription.APIKey)
	}
	if strings.Contains(buf.String(), "Welcome to yap") {
		t.Errorf("wizard should be skipped when env key is set, got: %s", buf.String())
	}
	if _, err := os.Stat(cfgFile); err != nil {
		t.Errorf("wizard should still write a default config file: %v", err)
	}
}

func TestRunWizard_SkippedWhenGroqKeyEnvSet(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	testAPIKey := "gsk_legacyAA00000000000000000000000000000000000000000000"
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", testAPIKey)

	var buf bytes.Buffer
	cfg, err := RunWizard(nil, &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}

	if cfg.Transcription.APIKey != testAPIKey {
		t.Errorf("GROQ_API_KEY should populate APIKey, got: %s", cfg.Transcription.APIKey)
	}
}

func TestRunWizard_ManualHotkeyComboValidation(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	// Enter a combo string; stub validates each segment.
	input := "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\n" +
		"KEY_LEFTSHIFT+KEY_SPACE\n" +
		"en\n"

	var buf bytes.Buffer
	cfg, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}
	if cfg.General.Hotkey != "KEY_LEFTSHIFT+KEY_SPACE" {
		t.Errorf("expected combo hotkey, got %q", cfg.General.Hotkey)
	}
}

func TestRunWizard_ManualHotkeyRejectsInvalidSegment(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	defer withTestConfigPath(t, cfgFile)()

	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	// First attempt contains an invalid segment; wizard loops and
	// accepts the second attempt.
	input := "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\n" +
		"KEY_LEFTSHIFT+KEY_BOGUS\n" +
		"KEY_RIGHTCTRL\n" +
		"en\n"

	var buf bytes.Buffer
	cfg, err := RunWizard(strings.NewReader(input), &buf, newStubHotkeyCfg())
	if err != nil {
		t.Fatalf("RunWizard: %v", err)
	}
	if cfg.General.Hotkey != "KEY_RIGHTCTRL" {
		t.Errorf("expected retried hotkey, got %q", cfg.General.Hotkey)
	}
	if !strings.Contains(buf.String(), "Invalid hotkey segment") {
		t.Errorf("expected validation error, got: %s", buf.String())
	}
}
