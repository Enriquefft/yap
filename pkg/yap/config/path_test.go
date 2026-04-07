package config_test

import (
	"strings"
	"testing"

	"github.com/hybridz/yap/pkg/yap/config"
)

// pathCase exercises a single dot-notation path through Set and Get.
type pathCase struct {
	path  string
	value string
	want  string
}

func TestGetSet_EveryLeafField(t *testing.T) {
	cases := []pathCase{
		// general
		{"general.hotkey", "KEY_F1", "KEY_F1"},
		{"general.mode", "toggle", "toggle"},
		{"general.max_duration", "120", "120"},
		{"general.audio_feedback", "false", "false"},
		{"general.audio_device", "hw:1,0", "hw:1,0"},
		{"general.silence_detection", "true", "true"},
		{"general.silence_threshold", "0.05", "0.05"},
		{"general.silence_duration", "1.5", "1.5"},
		{"general.history", "true", "true"},
		{"general.stream_partials", "false", "false"},
		// transcription
		{"transcription.backend", "openai", "openai"},
		{"transcription.model", "whisper-1", "whisper-1"},
		{"transcription.model_path", "/srv/models/base.en", "/srv/models/base.en"},
		{"transcription.language", "fr", "fr"},
		{"transcription.prompt", "yap dictation", "yap dictation"},
		{"transcription.api_url", "https://api.example.test/v1", "https://api.example.test/v1"},
		{"transcription.api_key", "sk-xyz", "sk-xyz"},
		// transform
		{"transform.enabled", "true", "true"},
		{"transform.backend", "local", "local"},
		{"transform.model", "llama3.2:3b", "llama3.2:3b"},
		{"transform.system_prompt", "Be terse.", "Be terse."},
		{"transform.api_url", "http://localhost:11434/v1", "http://localhost:11434/v1"},
		{"transform.api_key", "tx-key", "tx-key"},
		// injection
		{"injection.prefer_osc52", "false", "false"},
		{"injection.bracketed_paste", "false", "false"},
		{"injection.electron_strategy", "keystroke", "keystroke"},
		// tray
		{"tray.enabled", "true", "true"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			cfg := config.DefaultConfig()
			if err := config.Set(&cfg, tc.path, tc.value); err != nil {
				t.Fatalf("Set(%q,%q) error: %v", tc.path, tc.value, err)
			}
			got, err := config.Get(&cfg, tc.path)
			if err != nil {
				t.Fatalf("Get(%q) error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Errorf("round-trip: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGet_AppOverrideElement(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Injection.AppOverrides = []config.AppOverride{
		{Match: "firefox", Strategy: "clipboard"},
		{Match: "kitty", Strategy: "osc52"},
	}
	got, err := config.Get(&cfg, "injection.app_overrides.1.strategy")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got != "osc52" {
		t.Errorf("got %q, want osc52", got)
	}

	// Top-level slice format reports length so users know to drill in.
	summary, err := config.Get(&cfg, "injection.app_overrides")
	if err != nil {
		t.Fatalf("Get summary error: %v", err)
	}
	if !strings.Contains(summary, "2 items") {
		t.Errorf("slice summary missing length: %q", summary)
	}
}

func TestSet_TypeCoercionErrors(t *testing.T) {
	cfg := config.DefaultConfig()

	if err := config.Set(&cfg, "general.max_duration", "not-a-number"); err == nil {
		t.Error("expected integer parse error")
	}
	if err := config.Set(&cfg, "general.silence_threshold", "potato"); err == nil {
		t.Error("expected float parse error")
	}
	if err := config.Set(&cfg, "general.audio_feedback", "maybe"); err == nil {
		t.Error("expected bool parse error")
	}
}

func TestGet_UnknownPaths(t *testing.T) {
	cfg := config.DefaultConfig()

	cases := []string{
		"",
		"general.bogus",
		"general.hotkey.too.deep",
		"missing_section.field",
		"injection.app_overrides.99.match", // out of range
		"injection.app_overrides.x.match",  // non-numeric index
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			if _, err := config.Get(&cfg, path); err == nil {
				t.Errorf("Get(%q) should fail", path)
			}
		})
	}
}

func TestGet_StructPathReturnsRepr(t *testing.T) {
	// Reading a non-leaf path returns a stringified struct repr,
	// which is fine for human inspection. The contract is "Get does
	// not error on intermediate nodes". CLI users should drill in.
	cfg := config.DefaultConfig()
	got, err := config.Get(&cfg, "general")
	if err != nil {
		t.Fatalf("Get(general) should succeed: %v", err)
	}
	if got == "" {
		t.Error("Get(general) returned empty string")
	}
}

func TestSet_NilPointer(t *testing.T) {
	if err := config.Set(nil, "general.hotkey", "KEY_F1"); err == nil {
		t.Error("Set(nil, ...) should fail")
	}
	if _, err := config.Get(nil, "general.hotkey"); err == nil {
		t.Error("Get(nil, ...) should fail")
	}
}

func TestGet_DefaultValuesFormat(t *testing.T) {
	cfg := config.DefaultConfig()

	cases := []struct {
		path string
		want string
	}{
		{"general.hotkey", "KEY_RIGHTCTRL"},
		{"general.mode", "hold"},
		{"general.max_duration", "60"},
		{"general.audio_feedback", "true"},
		{"general.silence_threshold", "0.02"},
		{"general.silence_duration", "2"},
		{"transcription.backend", "whisperlocal"},
		{"transform.enabled", "false"},
		{"injection.electron_strategy", "clipboard"},
		{"tray.enabled", "false"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := config.Get(&cfg, tc.path)
			if err != nil {
				t.Fatalf("Get error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
