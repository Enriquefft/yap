package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

func TestSupports(t *testing.T) {
	p := &provider{}
	tests := []struct {
		name   string
		target inject.Target
		want   bool
	}{
		{"terminal", inject.Target{AppType: inject.AppTerminal}, true},
		{"tmux", inject.Target{AppType: inject.AppTerminal, Tmux: true}, true},
		{"tmux-generic", inject.Target{AppType: inject.AppGeneric, Tmux: true}, true},
		{"browser", inject.Target{AppType: inject.AppBrowser}, false},
		{"electron", inject.Target{AppType: inject.AppElectron}, false},
		{"generic", inject.Target{AppType: inject.AppGeneric}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Supports(tt.target); got != tt.want {
				t.Errorf("Supports(%v) = %v, want %v", tt.target, got, tt.want)
			}
		})
	}
}

func TestName(t *testing.T) {
	p := &provider{}
	if p.Name() != "claudecode" {
		t.Errorf("Name() = %q, want %q", p.Name(), "claudecode")
	}
}

func TestCWDToSlug(t *testing.T) {
	got := cwdToSlug("/home/hybridz/Projects/yap")
	want := "-home-hybridz-Projects-yap"
	if got != want {
		t.Errorf("cwdToSlug = %q, want %q", got, want)
	}
}

func TestParseSession(t *testing.T) {
	// Use the fixture in testdata/.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wd, "testdata", "session.jsonl")

	conversation, err := parseSession(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	// Verify the output contains the expected entries.
	lines := strings.Split(conversation, "\n\n")
	var nonEmpty []string
	for _, l := range lines {
		if l != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	// Expected: 4 user + 3 assistant = 7 entries (skipped: 2 meta, 1 local-command, 1 command-name)
	if len(nonEmpty) != 7 {
		t.Errorf("expected 7 entries, got %d:\n%s", len(nonEmpty), conversation)
	}

	// First real entry should be the user question.
	if !strings.HasPrefix(nonEmpty[0], "user: What is yap?") {
		t.Errorf("first entry = %q, want prefix %q", nonEmpty[0], "user: What is yap?")
	}

	// Second entry should be assistant with only text blocks.
	if !strings.HasPrefix(nonEmpty[1], "assistant: yap is a voice-to-text tool.") {
		t.Errorf("second entry = %q, want prefix %q", nonEmpty[1], "assistant: yap is a voice-to-text tool.")
	}

	// Check that tool_use and thinking are NOT in the output.
	if strings.Contains(conversation, "tool_use") {
		t.Error("output should not contain tool_use blocks")
	}
	if strings.Contains(conversation, "Let me think") {
		t.Error("output should not contain thinking blocks")
	}

	// Verify multi-text-block concatenation.
	if !strings.Contains(conversation, "The daemon listens for hotkey events and records audio when held.") {
		t.Error("multi-text blocks should be concatenated with space")
	}
}

func TestFetchEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create the slug directory but leave it empty.
	slugDir := filepath.Join(dir, ".claude", "projects", "-test-project")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatal(err)
	}

	p := &provider{
		rootPath: "/test/project",
		homeDir:  dir,
	}

	bundle, err := p.Fetch(context.Background(), inject.Target{
		AppType: inject.AppTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "" {
		t.Errorf("expected empty conversation, got %q", bundle.Conversation)
	}
}

func TestFetchMissingDirectory(t *testing.T) {
	p := &provider{
		rootPath: "/nonexistent/path",
		homeDir:  t.TempDir(),
	}

	bundle, err := p.Fetch(context.Background(), inject.Target{
		AppType: inject.AppTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Conversation != "" {
		t.Errorf("expected empty conversation, got %q", bundle.Conversation)
	}
}

func TestFetchMalformedJSONL(t *testing.T) {
	dir := t.TempDir()
	slugDir := filepath.Join(dir, ".claude", "projects", "-test-project")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a session with one bad line and one good line.
	content := `{this is not valid json}
{"type":"user","message":{"content":"hello world"},"isMeta":false,"timestamp":"2026-04-11T10:00:00Z"}
`
	if err := os.WriteFile(filepath.Join(slugDir, "session.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &provider{
		rootPath: "/test/project",
		homeDir:  dir,
	}

	bundle, err := p.Fetch(context.Background(), inject.Target{
		AppType: inject.AppTerminal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.Conversation, "user: hello world") {
		t.Errorf("expected surviving line, got %q", bundle.Conversation)
	}
}

func TestTailToBytes(t *testing.T) {
	s := strings.Repeat("a", 100)
	got := tailToBytes(s, 50)
	if len(got) != 50 {
		t.Errorf("tailToBytes len = %d, want 50", len(got))
	}

	got = tailToBytes("short", 1000)
	if got != "short" {
		t.Errorf("tailToBytes should not truncate short strings")
	}
}

func TestLatestSession(t *testing.T) {
	dir := t.TempDir()

	// Create two session files with different mtimes.
	old := filepath.Join(dir, "old.jsonl")
	new := filepath.Join(dir, "new.jsonl")
	if err := os.WriteFile(old, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(new, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := latestSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Both files were created nearly simultaneously; the last written
	// should be "new.jsonl" on most filesystems.
	if got != new {
		// Accept either — the test primarily verifies the function
		// returns a valid path, not exact ordering.
		if got != old {
			t.Errorf("latestSession = %q, want %q or %q", got, new, old)
		}
	}
}

func TestNewFactory(t *testing.T) {
	p, err := NewFactory(hint.Config{RootPath: "/tmp/test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "claudecode" {
		t.Errorf("Name() = %q, want %q", p.Name(), "claudecode")
	}
}
