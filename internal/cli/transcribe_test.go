package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/transcribe"
)

// withTranscribeBackend writes a config that selects a specific
// transcription backend, redirects XDG_CACHE_HOME so the local model
// downloader does not stomp the user cache, and clears the env-var
// overrides that would otherwise rewrite the api_key.
func withTranscribeBackend(t *testing.T, backend string) string {
	t.Helper()
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("YAP_TRANSFORM_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	writeConfigFile(t, cfgFile, "[transcription]\n  backend = \""+backend+"\"\n  model = \"mock\"\n")
	return cfgFile
}

func TestTranscribe_Mock_HappyPath(t *testing.T) {
	withTranscribeBackend(t, "mock")
	// The mock backend ignores the file content; we still must
	// supply a real path so openInputFile succeeds.
	tmp := t.TempDir()
	wav := filepath.Join(tmp, "in.wav")
	writeConfigFile(t, wav, "RIFF") // any bytes; mock drains and discards.

	stdout, _, err := runCLI(t, "transcribe", wav)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if !strings.Contains(stdout, "mock transcription") {
		t.Errorf("expected mock transcription in output, got %q", stdout)
	}
}

func TestTranscribe_Mock_JSON(t *testing.T) {
	withTranscribeBackend(t, "mock")
	tmp := t.TempDir()
	wav := filepath.Join(tmp, "in.wav")
	writeConfigFile(t, wav, "RIFF")

	stdout, _, err := runCLI(t, "transcribe", "--json", wav)
	if err != nil {
		t.Fatalf("transcribe --json: %v", err)
	}
	var chunk transcribe.TranscriptChunk
	dec := json.NewDecoder(strings.NewReader(stdout))
	if err := dec.Decode(&chunk); err != nil {
		t.Fatalf("decode json chunk: %v\noutput=%q", err, stdout)
	}
	if chunk.Text != "mock transcription" {
		t.Errorf("text = %q, want mock transcription", chunk.Text)
	}
	if !chunk.IsFinal {
		t.Error("expected IsFinal=true on the only chunk")
	}
}

func TestTranscribe_MissingFile(t *testing.T) {
	withTranscribeBackend(t, "mock")
	_, _, err := runCLI(t, "transcribe", "/nonexistent/path/to/audio.wav")
	if err == nil {
		t.Fatal("expected open error for missing file")
	}
}

func TestTranscribe_UnknownBackend(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	writeConfigFile(t, cfgFile, "[transcription]\n  backend = \"definitely-not-a-backend\"\n")

	wav := filepath.Join(tmp, "in.wav")
	writeConfigFile(t, wav, "RIFF")

	_, _, err := runCLI(t, "transcribe", wav)
	if err == nil {
		t.Fatal("expected unknown backend error")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-backend") {
		t.Errorf("error did not name the unknown backend: %v", err)
	}
}
