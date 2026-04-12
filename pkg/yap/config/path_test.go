package config_test

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/config"
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
		// hint
		{"hint.enabled", "false", "false"},
		{"hint.vocabulary_max_chars", "2000", "2000"},
		{"hint.conversation_max_chars", "16000", "16000"},
		{"hint.timeout_ms", "500", "500"},
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

func TestGet_StructPathReturnsJSON(t *testing.T) {
	// Reading a non-leaf path returns a JSON encoding of the
	// section. JSON over the bare fmt struct repr because the
	// latter is order-dependent and would break scripted callers if
	// a field is renamed or moved. The contract is documented on
	// the Get godoc.
	cfg := config.DefaultConfig()
	got, err := config.Get(&cfg, "general")
	if err != nil {
		t.Fatalf("Get(general) should succeed: %v", err)
	}
	if got == "" {
		t.Fatal("Get(general) returned empty string")
	}
	// JSON output is parseable: it must round-trip through
	// json.Unmarshal into a map without error.
	var m map[string]any
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatalf("Get(general) is not JSON: %v\noutput: %s", err, got)
	}
	// The map must contain the GeneralConfig fields by their toml tag
	// (encoding/json by default uses the json tag, but the structs
	// have no json tag so it falls back to the Go field name —
	// which is what we want here for stability across future toml
	// tag renames). At least one well-known field should appear.
	if _, ok := m["Hotkey"]; !ok {
		// Be permissive to either Go field name or toml tag here:
		// the test is a smoke check, not a contract on encoding/json
		// internals.
		if _, ok := m["hotkey"]; !ok {
			t.Errorf("expected JSON to contain hotkey field; got %s", got)
		}
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

// TestWalkerHandlesEveryConfigLeaf walks DefaultConfig() with
// reflection, enumerates every dotted leaf path under the schema,
// and asserts Get returns no error for each. The test fails the
// build if a future schema change adds a field whose kind is not
// covered by walk (currently struct, slice, and the leaf scalar
// kinds). It is the locked-down regression check the F6 finding
// asks for: any addition of *Foo, map[string]X, or interface{} to
// the schema will surface here instead of silently breaking
// `yap config get/set` for that path.
func TestWalkerHandlesEveryConfigLeaf(t *testing.T) {
	cfg := config.DefaultConfig()
	// AppOverrides is empty in DefaultConfig; populate one element
	// so the slice walker has something to descend into. The leaf
	// fields under each element type still need to be reachable.
	cfg.Injection.AppOverrides = []config.AppOverride{
		{Match: "firefox", Strategy: "clipboard"},
	}

	leaves := enumerateLeafPaths(t, reflect.ValueOf(cfg), "")
	if len(leaves) == 0 {
		t.Fatal("enumerateLeafPaths returned zero paths — reflection walker is broken")
	}
	for _, path := range leaves {
		t.Run(path, func(t *testing.T) {
			if _, err := config.Get(&cfg, path); err != nil {
				t.Errorf("Get(%q) returned error %v — walker missing a kind", path, err)
			}
		})
	}
}

// enumerateLeafPaths walks v and returns every dotted path that
// resolves to a scalar (string, bool, int, float). Struct fields are
// indexed by their toml tag; slice elements are indexed by position.
// The function mirrors the schema walker but with no behavior other
// than path collection.
func enumerateLeafPaths(t *testing.T, v reflect.Value, prefix string) []string {
	t.Helper()
	switch v.Kind() {
	case reflect.Struct:
		var out []string
		typ := v.Type()
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("toml")
			if comma := strings.Index(tag, ","); comma >= 0 {
				tag = tag[:comma]
			}
			if tag == "" {
				continue
			}
			child := tag
			if prefix != "" {
				child = prefix + "." + tag
			}
			out = append(out, enumerateLeafPaths(t, v.Field(i), child)...)
		}
		return out
	case reflect.Slice:
		// For an empty slice we cannot enumerate elements; the parent
		// path itself is reachable via Get and reports the slice
		// length. For a populated slice we descend into each index.
		if v.Len() == 0 {
			return []string{prefix}
		}
		var out []string
		for i := 0; i < v.Len(); i++ {
			child := prefix + "." + strconv.Itoa(i)
			out = append(out, enumerateLeafPaths(t, v.Index(i), child)...)
		}
		return out
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Float32, reflect.Float64:
		return []string{prefix}
	default:
		t.Fatalf("path %q: unexpected kind %s in DefaultConfig — walker would silently break for this path", prefix, v.Kind())
		return nil
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
