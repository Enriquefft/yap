package cli_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/internal/platform"
	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// hintTestProviderName returns a unique provider name per test to avoid
// registry collisions across test runs. The hint registry is append-only
// and panics on duplicate names.
func hintTestProviderName(t *testing.T, suffix string) string {
	t.Helper()
	return "hint_cli_test_" + t.Name() + "_" + suffix
}

// TestHint_PrintsSummary exercises the full `yap hint` path with a
// fake platform that returns a canned injector implementing
// StrategyResolver, a registered fake hint provider, and asserts the
// output format.
func TestHint_PrintsSummary(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	provName := hintTestProviderName(t, "ok")

	// Register a fake provider that matches terminals and returns a
	// known conversation.
	hint.Register(provName, func(cfg hint.Config) (hint.Provider, error) {
		return &fakeHintProvider{
			name:         provName,
			supports:     true,
			conversation: "user: what is yap?\nassistant: yap is a voice-to-text tool.",
		}, nil
	})

	// Config enables hint with our fake provider.
	writeConfigFile(t, cfgFile, "[hint]\n  enabled = true\n  providers = [\""+provName+"\"]\n")

	inj := &resolvingInjector{
		decision: inject.StrategyDecision{
			Target: inject.Target{
				DisplayServer: "wayland",
				AppClass:      "foot",
				AppType:       inject.AppTerminal,
			},
		},
	}
	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return inj, nil
		},
	}

	stdout, _, err := runCLIWithPlatform(t, p, "hint")
	if err != nil {
		t.Fatalf("yap hint: %v", err)
	}

	// Assert the output contains expected fields.
	for _, want := range []string{
		"target:",
		"display_server: wayland",
		"app_class:      foot",
		"app_type:       terminal",
		"provider: " + provName,
		"vocabulary:",
		"conversation:",
		"--- conversation (first 500 bytes) ---",
		"user: what is yap?",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}
}

// TestHint_Disabled asserts that `yap hint` with hint.enabled=false
// prints a short disabled notice and returns without error.
func TestHint_Disabled(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[hint]\n  enabled = false\n")

	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return &recordingInjector{}, nil
		},
	}

	stdout, _, err := runCLIWithPlatform(t, p, "hint")
	if err != nil {
		t.Fatalf("yap hint: %v", err)
	}
	if !strings.Contains(stdout, "disabled") {
		t.Errorf("expected 'disabled' in output, got:\n%s", stdout)
	}
}

// TestHint_NoStrategyResolver asserts that when the injector does not
// implement StrategyResolver, the command degrades gracefully and
// still prints vocabulary info.
func TestHint_NoStrategyResolver(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[hint]\n  enabled = true\n  providers = []\n")

	// recordingInjector does NOT implement StrategyResolver.
	inj := &recordingInjector{}
	p := platform.Platform{
		NewInjector: func(opts platform.InjectionOptions) (inject.Injector, error) {
			return inj, nil
		},
	}

	stdout, _, err := runCLIWithPlatform(t, p, "hint")
	if err != nil {
		t.Fatalf("yap hint: %v", err)
	}
	if !strings.Contains(stdout, "StrategyResolver") {
		t.Errorf("expected StrategyResolver note in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "target:") {
		t.Errorf("expected target block in output, got:\n%s", stdout)
	}
}

// fakeHintProvider is a test-only hint.Provider for CLI hint tests.
type fakeHintProvider struct {
	name         string
	supports     bool
	conversation string
	fetchErr     error
}

func (f *fakeHintProvider) Name() string { return f.name }

func (f *fakeHintProvider) Supports(_ inject.Target) bool { return f.supports }

func (f *fakeHintProvider) Fetch(_ context.Context, _ inject.Target) (hint.Bundle, error) {
	if f.fetchErr != nil {
		return hint.Bundle{}, f.fetchErr
	}
	return hint.Bundle{
		Conversation: f.conversation,
		Source:       f.name,
	}, nil
}
