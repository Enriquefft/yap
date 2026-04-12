package hint_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/transcribe"
	_ "github.com/Enriquefft/yap/pkg/yap/transcribe/groq"
)

// TestVocabularyBias_Groq is an integration test that verifies the hint
// vocabulary prompt biases Whisper toward project-specific terms. It
// requires a live Groq API key in $GROQ_API_KEY and a test audio file
// saying "¿qué es yap?" in testdata/que-es-yap.wav.
//
// Without the vocabulary prompt, Whisper transcribes "yap" as "ya" or
// "Japón". With the prompt containing "yap" as a domain term, the
// transcription correctly includes "yap".
func TestVocabularyBias_Groq(t *testing.T) {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		t.Skip("GROQ_API_KEY not set")
	}

	wavPath := "testdata/que-es-yap.wav"
	if _, err := os.Stat(wavPath); err != nil {
		t.Skipf("test fixture missing: %v", err)
	}

	factory, err := transcribe.Get("groq")
	if err != nil {
		t.Fatalf("get groq factory: %v", err)
	}
	tr, err := factory(transcribe.Config{
		APIKey:   apiKey,
		Model:    "whisper-large-v3-turbo",
		Language: "es",
	})
	if err != nil {
		t.Fatalf("build transcriber: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Baseline: no prompt.
	baseline := transcribeFile(t, ctx, tr, wavPath, transcribe.Options{})
	t.Logf("baseline (no prompt): %q", baseline)

	// With vocabulary prompt. Resolve to repo root via cwd.
	cwd, _ := os.Getwd()
	vocab := hint.ReadVocabularyFiles(cwd, []string{"CLAUDE.md", "README.md"})
	t.Logf("vocabulary terms: %q", vocab)

	withHint := transcribeFile(t, ctx, tr, wavPath, transcribe.Options{Prompt: vocab})
	t.Logf("with hint: %q", withHint)

	lower := strings.ToLower(withHint)
	if !strings.Contains(lower, "yap") {
		t.Errorf("expected 'yap' in transcription with hint, got %q", withHint)
	}
}

func transcribeFile(t *testing.T, ctx context.Context, tr transcribe.Transcriber, path string, opts transcribe.Options) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	ch, err := tr.Transcribe(ctx, f, opts)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}

	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("transcribe chunk error: %v", chunk.Err)
		}
		sb.WriteString(chunk.Text)
	}
	return sb.String()
}
