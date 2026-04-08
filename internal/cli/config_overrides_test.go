package cli_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigOverrides_AddListRemoveClear(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// List on empty config.
	stdout, _, err := runCLI(t, "config", "overrides", "list")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if !strings.Contains(stdout, "no app_overrides") {
		t.Errorf("expected empty-list message, got %q", stdout)
	}

	// Add two entries.
	if _, _, err := runCLI(t, "config", "overrides", "add", "firefox", "electron"); err != nil {
		t.Fatalf("add 1: %v", err)
	}
	if _, _, err := runCLI(t, "config", "overrides", "add", "kitty", "osc52"); err != nil {
		t.Fatalf("add 2: %v", err)
	}

	// List shows both in insertion order.
	stdout, _, err = runCLI(t, "config", "overrides", "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(stdout, "firefox") || !strings.Contains(stdout, "kitty") {
		t.Errorf("list missing entries: %q", stdout)
	}

	// Verify via dot-notation get.
	got, _, err := runCLI(t, "config", "get", "injection.app_overrides.0.match")
	if err != nil {
		t.Fatalf("get match: %v", err)
	}
	if strings.TrimSpace(got) != "firefox" {
		t.Errorf("overrides[0].match: got %q, want firefox", got)
	}

	// Remove index 0.
	if _, _, err := runCLI(t, "config", "overrides", "remove", "0"); err != nil {
		t.Fatalf("remove 0: %v", err)
	}
	got, _, err = runCLI(t, "config", "get", "injection.app_overrides.0.match")
	if err != nil {
		t.Fatalf("get after remove: %v", err)
	}
	if strings.TrimSpace(got) != "kitty" {
		t.Errorf("after remove: got %q, want kitty", got)
	}

	// Clear.
	if _, _, err := runCLI(t, "config", "overrides", "clear"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	stdout, _, err = runCLI(t, "config", "overrides", "list")
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if !strings.Contains(stdout, "no app_overrides") {
		t.Errorf("expected empty list after clear, got %q", stdout)
	}
}

func TestConfigOverrides_RemoveOutOfRange(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	if _, _, err := runCLI(t, "config", "overrides", "remove", "5"); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestConfigOverrides_AddRejectsEmpty(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// Empty match is invalid; validator should reject.
	if _, _, err := runCLI(t, "config", "overrides", "add", "", "clipboard"); err == nil {
		t.Fatal("expected validation error for empty match")
	}
}
