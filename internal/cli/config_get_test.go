package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hybridz/yap/internal/cli"
)

// runCLI invokes the root command with argv, captures stdout+stderr,
// and returns them plus the command's error. It does not mutate
// os.Args — the cobra command's SetArgs is used via ExecuteForTest.
func runCLI(t *testing.T, argv ...string) (string, string, error) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	err := cli.ExecuteForTest(argv, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), err
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigGet_Dotnotation_EveryLeaf(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("YAP_HOTKEY", "")

	writeConfigFile(t, cfgFile, `
[general]
  hotkey = "KEY_SPACE"
  mode = "toggle"
  max_duration = 90
  audio_feedback = false
  audio_device = "hw:1,0"
  silence_detection = true
  silence_threshold = 0.1
  silence_duration = 3.0
  history = true
  stream_partials = false

[transcription]
  backend = "groq"
  model = "whisper-large-v3-turbo"
  model_path = "/models/custom"
  language = "de"
  prompt = "hint"
  api_url = ""
  api_key = "sk-abc"

[transform]
  enabled = true
  backend = "local"
  model = "llama3.2:3b"
  system_prompt = "fix it"
  api_url = "http://localhost:11434/v1"
  api_key = "tx-key"

[injection]
  prefer_osc52 = false
  bracketed_paste = false
  electron_strategy = "keystroke"

[tray]
  enabled = true
`)

	cases := []struct {
		path string
		want string
	}{
		{"general.hotkey", "KEY_SPACE"},
		{"general.mode", "toggle"},
		{"general.max_duration", "90"},
		{"general.audio_feedback", "false"},
		{"general.audio_device", "hw:1,0"},
		{"general.silence_detection", "true"},
		{"general.silence_threshold", "0.1"},
		{"general.silence_duration", "3"},
		{"general.history", "true"},
		{"general.stream_partials", "false"},
		{"transcription.backend", "groq"},
		{"transcription.model", "whisper-large-v3-turbo"},
		{"transcription.model_path", "/models/custom"},
		{"transcription.language", "de"},
		{"transcription.prompt", "hint"},
		{"transcription.api_key", "sk-abc"},
		{"transform.enabled", "true"},
		{"transform.backend", "local"},
		{"transform.model", "llama3.2:3b"},
		{"transform.system_prompt", "fix it"},
		{"transform.api_url", "http://localhost:11434/v1"},
		{"transform.api_key", "tx-key"},
		{"injection.prefer_osc52", "false"},
		{"injection.bracketed_paste", "false"},
		{"injection.electron_strategy", "keystroke"},
		{"tray.enabled", "true"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			stdout, _, err := runCLI(t, "config", "get", tc.path)
			if err != nil {
				t.Fatalf("config get %s: %v", tc.path, err)
			}
			got := strings.TrimRight(stdout, "\n")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigGet_UnknownKey(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")

	_, _, err := runCLI(t, "config", "get", "general.does_not_exist")
	if err == nil {
		t.Fatal("expected unknown-key error")
	}
}

func TestConfigGet_EnvOverride_YAPApiKeyWins(t *testing.T) {
	// YAP_API_KEY has highest precedence. The file value is only a
	// tie-breaker against the default; env trumps both.
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("GROQ_API_KEY", "groq-value")
	t.Setenv("YAP_API_KEY", "yap-env-wins")

	writeConfigFile(t, cfgFile, `
[transcription]
  api_key = "file-value"
`)

	stdout, _, err := runCLI(t, "config", "get", "transcription.api_key")
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(stdout) != "yap-env-wins" {
		t.Errorf("env override: got %q, want yap-env-wins", stdout)
	}
}

func TestConfigGet_EnvOverride_GroqFallback(t *testing.T) {
	// GROQ_API_KEY is the fallback when YAP_API_KEY is unset.
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "groq-fallback")

	writeConfigFile(t, cfgFile, `
[transcription]
  api_key = "file-value"
`)

	stdout, _, err := runCLI(t, "config", "get", "transcription.api_key")
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if strings.TrimSpace(stdout) != "groq-fallback" {
		t.Errorf("groq fallback: got %q, want groq-fallback", stdout)
	}
}
