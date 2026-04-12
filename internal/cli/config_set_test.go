package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	pcfg "github.com/Enriquefft/yap/pkg/yap/config"
)

// seedDefaultConfig writes a fresh default config to cfgFile so Set
// runs against a known starting point.
func seedDefaultConfig(t *testing.T, cfgFile string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(pcfg.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
}

func readPersistedConfig(t *testing.T, cfgFile string) pcfg.Config {
	t.Helper()
	var c pcfg.Config
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := toml.NewDecoder(strings.NewReader(string(data))).Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConfigSet_PersistsNestedValue(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "transform.enabled", "true")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}

	got := readPersistedConfig(t, cfgFile)
	if !got.Transform.Enabled {
		t.Error("transform.enabled was not persisted")
	}
}

func TestConfigSet_ValidatesBeforeSave(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// Set an invalid mode; validator must reject and not write.
	_, _, err := runCLI(t, "config", "set", "general.mode", "always-on")
	if err == nil {
		t.Fatal("expected validation error")
	}

	// File still contains the original default.
	got := readPersistedConfig(t, cfgFile)
	if got.General.Mode != "hold" {
		t.Errorf("mode clobbered: got %q, want hold", got.General.Mode)
	}
}

func TestConfigSet_RejectsBadBool(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "transform.enabled", "maybe")
	if err == nil {
		t.Fatal("expected bool parse error")
	}
}

func TestConfigSet_RejectsBadInt(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	_, _, err := runCLI(t, "config", "set", "general.max_duration", "forever")
	if err == nil {
		t.Fatal("expected integer parse error")
	}
}

func TestConfigSet_RejectsOutOfRangeInt(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// 0 and 301 are both out of validator range [1,300].
	for _, v := range []string{"0", "301"} {
		if _, _, err := runCLI(t, "config", "set", "general.max_duration", v); err == nil {
			t.Errorf("max_duration=%s should fail validation", v)
		}
	}
}

func TestConfigSet_RoundTripString(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)

	// The default whisperlocal model is "base.en" which is
	// English-only; the validator correctly rejects setting
	// language=ja while the model stays .en. Switch to a
	// multilingual model first so the round-trip exercises the
	// set/get path rather than the validator.
	if _, _, err := runCLI(t, "config", "set", "transcription.model", "base"); err != nil {
		t.Fatalf("set model: %v", err)
	}
	if _, _, err := runCLI(t, "config", "set", "transcription.language", "ja"); err != nil {
		t.Fatalf("set: %v", err)
	}
	stdout, _, err := runCLI(t, "config", "get", "transcription.language")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(stdout) != "ja" {
		t.Errorf("round-trip: got %q, want ja", stdout)
	}
}

// commentHeavyFixture is a hand-crafted config file with comments
// above every section, inline comments on some values, a custom
// 4-space indent on keys inside [general], and a comment-only
// closing block. The comment-preservation test asserts that every
// non-target line in this fixture is byte-identical after one
// `config set` invocation.
const commentHeavyFixture = `# yap configuration — hand edited
# Last reviewed 2026-03-14
# Please do not replace this file wholesale.

[general]
    # The hotkey yap watches for.
    hotkey = "KEY_RIGHTCTRL"  # my favorite
    mode = "hold"
    max_duration = 60
    audio_feedback = true
    audio_device = ""
    silence_detection = false
    silence_threshold = 0.02
    silence_duration = 2.0
    history = false
    stream_partials = true

# Transcription backend.
[transcription]
    backend = "whisperlocal"
    model = "base.en"
    model_path = ""
    whisper_server_path = ""
    whisper_threads = 0
    whisper_use_gpu = true
    language = "en"
    api_url = ""
    api_key = ""

[transform]
    enabled = false
    backend = "passthrough"
    model = ""
    system_prompt = "Fix transcription errors and punctuation. Do not rephrase. Preserve original language. Output only corrected text."
    api_url = ""
    api_key = ""

[injection]
    prefer_osc52 = true
    bracketed_paste = true
    electron_strategy = "clipboard"
    default_strategy = ""

[tray]
    enabled = false
# End of file — every byte above is load-bearing.
`

// TestConfigSet_PreservesComments asserts that `yap config set`
// preserves comments, custom indentation, and key ordering when
// editing a hand-edited TOML file. This is the headline user-
// visible benefit of the custom editor; a regression here would
// reintroduce the original bug the Bug 9 work exists to fix.
func TestConfigSet_PreservesComments(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("YAP_HOTKEY", "")

	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(commentHeavyFixture), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCLI(t, "config", "set", "general.hotkey", "KEY_F24")
	if err != nil {
		t.Fatalf("config set: %v", err)
	}

	got, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}

	// Every line except the target must match the original byte
	// for byte. The target line is `    hotkey = "KEY_F24"  # my favorite`.
	inLines := strings.SplitAfter(commentHeavyFixture, "\n")
	outLines := strings.SplitAfter(string(got), "\n")
	if len(inLines) != len(outLines) {
		t.Fatalf("line count changed: in=%d out=%d", len(inLines), len(outLines))
	}
	wantTarget := `    hotkey = "KEY_F24"  # my favorite` + "\n"
	targetIdx := -1
	for i := range inLines {
		if strings.HasPrefix(strings.TrimLeft(inLines[i], " \t"), `hotkey`) {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		t.Fatal("could not locate target line in fixture")
	}
	for i := range inLines {
		if i == targetIdx {
			if outLines[i] != wantTarget {
				t.Errorf("target line = %q, want %q", outLines[i], wantTarget)
			}
			continue
		}
		if inLines[i] != outLines[i] {
			t.Errorf("line %d changed:\n in: %q\nout: %q", i+1, inLines[i], outLines[i])
		}
	}
}

// TestConfigSet_RefusesNixStorePath exercises the /nix/store
// read-only refusal. The harness points YAP_CONFIG at a synthetic
// /nix/store path inside a TempDir so the refusal fires without
// needing the real store layout; the CLI matches the prefix string.
//
// The test runs under a temp HOME override so no sibling tests can
// race on XDG resolution.
func TestConfigSet_RefusesNixStorePath(t *testing.T) {
	tmp := t.TempDir()
	// Build a fake /nix/store path under the TempDir. The CLI
	// matches the PREFIX string, so any path beginning with
	// "/nix/store/" triggers the refusal regardless of whether
	// the file exists or is writable.
	fakeStore := "/nix/store/abc-yap/config.toml"
	t.Setenv("YAP_CONFIG", fakeStore)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("HOME", tmp)

	_, stderr, err := runCLI(t, "config", "set", "general.hotkey", "KEY_F24")
	if err == nil {
		t.Fatal("expected refusal error")
	}
	combined := err.Error() + stderr
	if !strings.Contains(combined, "/nix/store/") {
		t.Errorf("refusal message did not mention /nix/store/: %q", combined)
	}
	if !strings.Contains(combined, "NixOS") && !strings.Contains(combined, "nixos") {
		t.Errorf("refusal message did not mention NixOS: %q", combined)
	}
}

// TestConfigSet_RefusesInvalidType covers the fail-fast type check:
// the schema-aware serializer rejects a non-integer value before
// the editor touches the file, so the on-disk bytes stay untouched.
func TestConfigSet_RefusesInvalidType(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	seedDefaultConfig(t, cfgFile)
	before, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := runCLI(t, "config", "set", "general.max_duration", "not a number"); err == nil {
		t.Fatal("expected type error")
	}

	after, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("file was mutated on type-error path:\n--- before ---\n%s\n--- after ---\n%s",
			string(before), string(after))
	}
}

// TestConfigSet_PostWriteValidationFailure asserts that a value
// that parses cleanly (and the editor happily rewrites) but fails
// schema validation leaves the user's file untouched on disk. Here
// we set max_duration to 500 — above the validator's [1,300] range
// — on a comment-heavy fixture. The expected behavior is: the
// editor runs, produces edited bytes, validation fails, the atomic
// write is refused, and the original file bytes remain.
func TestConfigSet_PostWriteValidationFailure(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(commentHeavyFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := runCLI(t, "config", "set", "general.max_duration", "500"); err == nil {
		t.Fatal("expected validation error")
	}

	after, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("file was mutated on validation failure:\n--- before ---\n%s\n--- after ---\n%s",
			string(before), string(after))
	}
}

// TestConfigSet_FirstRunCreatesFile asserts the first-run path:
// if no config file exists yet, `config set` writes a fresh
// defaults-based file via the original Save path (which is
// comment-free by definition since there are no comments to
// preserve on a brand-new install).
func TestConfigSet_FirstRunCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")

	if _, err := os.Stat(cfgFile); err == nil {
		t.Fatalf("precondition: config should not exist yet")
	}

	if _, _, err := runCLI(t, "config", "set", "general.hotkey", "KEY_F1"); err != nil {
		t.Fatalf("first-run set: %v", err)
	}
	got := readPersistedConfig(t, cfgFile)
	if got.General.Hotkey != "KEY_F1" {
		t.Errorf("hotkey = %q, want KEY_F1", got.General.Hotkey)
	}
}
