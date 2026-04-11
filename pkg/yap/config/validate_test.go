package config_test

import (
	"strings"
	"testing"

	"github.com/hybridz/yap/pkg/yap/config"
)

// stubKeyValidator implements config.KeyValidator without importing
// the platform package. The validator only depends on the structural
// interface so the test stub is local.
type stubKeyValidator struct {
	known map[string]struct{}
}

func newStubValidator(keys ...string) *stubKeyValidator {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return &stubKeyValidator{known: m}
}

func (s *stubKeyValidator) ValidKey(name string) bool {
	_, ok := s.known[name]
	return ok
}

// withDefaults returns DefaultConfig with the supplied mutator applied.
func withDefaults(mutate func(*config.Config)) config.Config {
	c := config.DefaultConfig()
	if mutate != nil {
		mutate(&c)
	}
	return c
}

func TestValidate_Default(t *testing.T) {
	// The default config validates with a permissive key validator
	// that knows the default hotkey.
	kv := newStubValidator("KEY_RIGHTCTRL")
	if err := config.DefaultConfig().Validate(kv); err != nil {
		t.Fatalf("default config failed validation: %v", err)
	}
}

func TestValidate_TableDriven(t *testing.T) {
	knownKeys := newStubValidator("KEY_RIGHTCTRL", "KEY_LEFTSHIFT", "KEY_SPACE", "KEY_A")

	cases := []struct {
		name      string
		mutate    func(*config.Config)
		validator config.KeyValidator
		wantErr   string // substring; "" means must succeed
	}{
		// general.hotkey
		{
			name:      "hotkey empty",
			mutate:    func(c *config.Config) { c.General.Hotkey = "" },
			validator: knownKeys,
			wantErr:   "general.hotkey: required",
		},
		{
			name:      "hotkey unknown segment",
			mutate:    func(c *config.Config) { c.General.Hotkey = "KEY_RIGHTCTRL+KEY_BOGUS" },
			validator: knownKeys,
			wantErr:   `general.hotkey: invalid key "KEY_BOGUS"`,
		},
		{
			name:      "hotkey known combo",
			mutate:    func(c *config.Config) { c.General.Hotkey = "KEY_LEFTSHIFT+KEY_SPACE" },
			validator: knownKeys,
		},
		{
			name:      "hotkey nil validator skips segment check",
			mutate:    func(c *config.Config) { c.General.Hotkey = "KEY_BOGUS" },
			validator: nil,
		},
		// general.mode
		{
			name:      "mode invalid",
			mutate:    func(c *config.Config) { c.General.Mode = "always-on" },
			validator: knownKeys,
			wantErr:   "general.mode",
		},
		{
			name:      "mode toggle ok",
			mutate:    func(c *config.Config) { c.General.Mode = "toggle" },
			validator: knownKeys,
		},
		// general.max_duration
		{
			name:      "max_duration zero",
			mutate:    func(c *config.Config) { c.General.MaxDuration = 0 },
			validator: knownKeys,
			wantErr:   "general.max_duration",
		},
		{
			name:      "max_duration too high",
			mutate:    func(c *config.Config) { c.General.MaxDuration = 301 },
			validator: knownKeys,
			wantErr:   "general.max_duration",
		},
		// general.silence_threshold
		{
			name:      "silence_threshold negative",
			mutate:    func(c *config.Config) { c.General.SilenceThreshold = -0.1 },
			validator: knownKeys,
			wantErr:   "general.silence_threshold",
		},
		{
			name:      "silence_threshold above 1",
			mutate:    func(c *config.Config) { c.General.SilenceThreshold = 1.5 },
			validator: knownKeys,
			wantErr:   "general.silence_threshold",
		},
		// general.silence_duration
		{
			name:      "silence_duration zero",
			mutate:    func(c *config.Config) { c.General.SilenceDuration = 0 },
			validator: knownKeys,
			wantErr:   "general.silence_duration",
		},
		// transcription.backend
		{
			name:      "backend invalid",
			mutate:    func(c *config.Config) { c.Transcription.Backend = "potato" },
			validator: knownKeys,
			wantErr:   "transcription.backend",
		},
		// transcription.api_url required for remote backends
		{
			name: "openai backend without api_url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "openai"
				c.Transcription.APIURL = ""
			},
			validator: knownKeys,
			wantErr:   "transcription.api_url: required",
		},
		{
			name: "groq backend supplies default url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "groq"
				c.Transcription.APIURL = ""
			},
			validator: knownKeys,
		},
		{
			name: "custom backend with garbage url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "ftp://nope"
			},
			validator: knownKeys,
			wantErr:   "transcription.api_url",
		},
		{
			name: "custom backend with https url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "https://example.test/v1"
			},
			validator: knownKeys,
		},
		// F7: schemeless / hostless / whitespace URLs must be rejected.
		{
			name: "custom backend with schemeless host (no host part)",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "https://"
			},
			validator: knownKeys,
			wantErr:   "transcription.api_url",
		},
		{
			name: "custom backend with embedded space",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "https://example.com/with spaces/foo"
			},
			validator: knownKeys,
			wantErr:   "transcription.api_url",
		},
		{
			name: "custom backend with trailing newline",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "https://example.com/v1\n"
			},
			validator: knownKeys,
			wantErr:   "transcription.api_url",
		},
		{
			name: "custom backend http url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "custom"
				c.Transcription.APIURL = "http://localhost:8080/v1"
			},
			validator: knownKeys,
		},
		{
			name: "whisperlocal needs no url",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.APIURL = ""
			},
			validator: knownKeys,
		},
		// whisperlocal English-only model + non-en language is a
		// silent misconfiguration: whisper-server logs a warning
		// and transcribes in English anyway. Reject at load time.
		{
			name: "whisperlocal es with base.en rejected",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "base.en"
				c.Transcription.Language = "es"
			},
			validator: knownKeys,
			wantErr:   "transcription.language",
		},
		{
			name: "whisperlocal fr with small.en rejected",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "small.en"
				c.Transcription.Language = "fr"
			},
			validator: knownKeys,
			wantErr:   "transcription.language",
		},
		{
			name: "whisperlocal en with base.en ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "base.en"
				c.Transcription.Language = "en"
			},
			validator: knownKeys,
		},
		{
			name: "whisperlocal es with base (multilingual) ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "base"
				c.Transcription.Language = "es"
			},
			validator: knownKeys,
		},
		{
			name: "whisperlocal auto-detect with base.en ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "base.en"
				c.Transcription.Language = ""
			},
			validator: knownKeys,
		},
		// M2: transcription.model_path is the absolute override —
		// whisperlocal.resolveModel loads the file at ModelPath and
		// never looks at cfg.Model. A leftover model = "base.en"
		// alongside a hand-downloaded multilingual model_path and a
		// non-English language must pass validation; rejecting it
		// would be a false positive for the air-gapped escape hatch.
		{
			name: "whisperlocal es with base.en but multilingual model_path ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "base.en"
				c.Transcription.ModelPath = "/home/user/models/ggml-large-v3.bin"
				c.Transcription.Language = "es"
			},
			validator: knownKeys,
		},
		{
			name: "whisperlocal fr with small.en but multilingual model_path ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "whisperlocal"
				c.Transcription.Model = "small.en"
				c.Transcription.ModelPath = "/opt/whisper/ggml-medium.bin"
				c.Transcription.Language = "fr"
			},
			validator: knownKeys,
		},
		// The English-only guard is whisperlocal-specific — the
		// remote ".en"-named models (if any) are the backend's
		// problem, not ours.
		{
			name: "groq with base.en-named model and es language ok",
			mutate: func(c *config.Config) {
				c.Transcription.Backend = "groq"
				c.Transcription.Model = "whisper-large-v3.en"
				c.Transcription.Language = "es"
			},
			validator: knownKeys,
		},
		// whisper_threads bounds check.
		{
			name: "whisper_threads negative rejected",
			mutate: func(c *config.Config) {
				c.Transcription.WhisperThreads = -1
			},
			validator: knownKeys,
			wantErr:   "transcription.whisper_threads",
		},
		{
			name: "whisper_threads above 64 rejected",
			mutate: func(c *config.Config) {
				c.Transcription.WhisperThreads = 65
			},
			validator: knownKeys,
			wantErr:   "transcription.whisper_threads",
		},
		{
			name: "whisper_threads zero (auto) ok",
			mutate: func(c *config.Config) {
				c.Transcription.WhisperThreads = 0
			},
			validator: knownKeys,
		},
		{
			name: "whisper_threads 8 ok",
			mutate: func(c *config.Config) {
				c.Transcription.WhisperThreads = 8
			},
			validator: knownKeys,
		},
		// transform
		{
			name:      "transform.backend invalid",
			mutate:    func(c *config.Config) { c.Transform.Backend = "neural-net" },
			validator: knownKeys,
			wantErr:   "transform.backend",
		},
		{
			name: "transform.enabled local without model",
			mutate: func(c *config.Config) {
				c.Transform.Enabled = true
				c.Transform.Backend = "local"
				c.Transform.Model = ""
			},
			validator: knownKeys,
			wantErr:   "transform.model: required",
		},
		{
			name: "transform.enabled passthrough does not require model",
			mutate: func(c *config.Config) {
				c.Transform.Enabled = true
				c.Transform.Backend = "passthrough"
			},
			validator: knownKeys,
		},
		// injection.electron_strategy
		{
			name:      "electron_strategy invalid",
			mutate:    func(c *config.Config) { c.Injection.ElectronStrategy = "magic" },
			validator: knownKeys,
			wantErr:   "injection.electron_strategy",
		},
		// injection.app_overrides
		{
			name: "app override missing match",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Strategy: "clipboard"}}
			},
			validator: knownKeys,
			wantErr:   "injection.app_overrides[0].match: required",
		},
		{
			name: "app override missing strategy",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "firefox"}}
			},
			validator: knownKeys,
			wantErr:   "injection.app_overrides[0].strategy: required",
		},
		{
			name: "app override valid",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{
					{Match: "firefox", Strategy: "electron"},
				}
			},
			validator: knownKeys,
		},
		// F8: app_overrides[].strategy must be one of the registered
		// Linux strategies (matching the Name() values in
		// internal/platform/linux/inject). Each valid value goes
		// through the validator; each typo is rejected.
		{
			name: "app override strategy osc52",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "kitty", Strategy: "osc52"}}
			},
			validator: knownKeys,
		},
		{
			name: "app override strategy electron",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "code", Strategy: "electron"}}
			},
			validator: knownKeys,
		},
		{
			name: "app override strategy tmux",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "tmux-session", Strategy: "tmux"}}
			},
			validator: knownKeys,
		},
		{
			name: "app override strategy wayland",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "firefox", Strategy: "wayland"}}
			},
			validator: knownKeys,
		},
		{
			name: "app override strategy x11",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "firefox", Strategy: "x11"}}
			},
			validator: knownKeys,
		},
		{
			name: "app override strategy typo (clipoard)",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "firefox", Strategy: "clipoard"}}
			},
			validator: knownKeys,
			wantErr:   "injection.app_overrides[0].strategy",
		},
		{
			name: "app override strategy typo (osc-52)",
			mutate: func(c *config.Config) {
				c.Injection.AppOverrides = []config.AppOverride{{Match: "kitty", Strategy: "osc-52"}}
			},
			validator: knownKeys,
			wantErr:   "injection.app_overrides[0].strategy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := withDefaults(tc.mutate)
			err := cfg.Validate(tc.validator)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestValidate_AggregatesErrors(t *testing.T) {
	// errors.Join produces a multi-line error string. Confirm that
	// multiple violations all appear so users see everything they
	// need to fix.
	cfg := config.DefaultConfig()
	cfg.General.Mode = "always-on"
	cfg.General.MaxDuration = 1000
	cfg.Transcription.Backend = "potato"

	err := cfg.Validate(newStubValidator("KEY_RIGHTCTRL"))
	if err == nil {
		t.Fatal("expected aggregated validation errors, got nil")
	}
	for _, want := range []string{"general.mode", "general.max_duration", "transcription.backend"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error missing %q. full error: %v", want, err)
		}
	}
}

// TestValidate_InjectionDefaultStrategy guards C7: empty disables,
// any of the known strategy names is accepted, and an unknown name
// is rejected with a clear error.
func TestValidate_InjectionDefaultStrategy(t *testing.T) {
	kv := newStubValidator("KEY_RIGHTCTRL")
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"empty disables", "", false},
		{"tmux", "tmux", false},
		{"osc52", "osc52", false},
		{"electron", "electron", false},
		{"wayland", "wayland", false},
		{"x11", "x11", false},
		{"unknown rejected", "banana", true},
		{"typo rejected", "wayland2", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Injection.DefaultStrategy = tc.value
			err := cfg.Validate(kv)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for default_strategy=%q, got nil", tc.value)
				} else if !strings.Contains(err.Error(), "injection.default_strategy") {
					t.Errorf("error %v does not mention injection.default_strategy", err)
				}
			} else if err != nil {
				t.Errorf("unexpected error for default_strategy=%q: %v", tc.value, err)
			}
		})
	}
}
