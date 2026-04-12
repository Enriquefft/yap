package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

const (
	// maxBundleBytes is the raw budget for the conversation text.
	// The engine truncates further per-stage.
	maxBundleBytes = 16 * 1024

	providerName = "claudecode"
)

// provider reads the most recent Claude Code session JSONL.
type provider struct {
	rootPath string
	homeDir  string
}

// NewFactory returns a hint.Factory that constructs claudecode providers.
func NewFactory(cfg hint.Config) (hint.Provider, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("claudecode: resolve home dir: %w", err)
	}
	return &provider{
		rootPath: cfg.RootPath,
		homeDir:  home,
	}, nil
}

func (p *provider) Name() string { return providerName }

func (p *provider) Supports(target inject.Target) bool {
	return target.AppType == inject.AppTerminal || target.Tmux
}

func (p *provider) Fetch(ctx context.Context, target inject.Target) (hint.Bundle, error) {
	slug := cwdToSlug(p.rootPath)
	dir := filepath.Join(p.homeDir, ".claude", "projects", slug)

	sessionPath, err := latestSession(dir)
	if err != nil || sessionPath == "" {
		return hint.Bundle{}, nil
	}

	conversation, err := parseSession(ctx, sessionPath)
	if err != nil {
		slog.Debug("claudecode: parse session", "error", err)
		return hint.Bundle{}, nil
	}

	conversation = tailToBytes(conversation, maxBundleBytes)

	return hint.Bundle{
		Conversation: conversation,
		Source:       providerName,
	}, nil
}

// cwdToSlug converts an absolute path to the Claude Code slug format.
func cwdToSlug(absPath string) string {
	return strings.ReplaceAll(absPath, "/", "-")
}

// latestSession finds the JSONL file with the most recent mtime.
func latestSession(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil //nolint:nilerr // missing dir is not an error
	}

	var (
		latest     string
		latestTime time.Time
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(dir, e.Name())
		}
	}
	return latest, nil
}

// parseSession reads a session JSONL and extracts user/assistant messages.
func parseSession(_ context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil //nolint:nilerr // missing file is not an error
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	// Claude Code sessions can have long lines (e.g. tool output).
	const maxLineSize = 1 << 20 // 1 MiB
	scanner.Buffer(make([]byte, 0, maxLineSize), maxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Flexible parsing: only extract the fields we need.
		var raw struct {
			Type    string          `json:"type"`
			IsMeta  bool            `json:"isMeta"`
			Message json.RawMessage `json:"message"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			slog.Debug("claudecode: skip malformed line", "error", err)
			continue
		}

		switch raw.Type {
		case "user":
			if raw.IsMeta {
				continue
			}
			content := extractUserContent(raw.Message)
			if content == "" {
				continue
			}
			if strings.HasPrefix(content, "<local-command-caveat>") ||
				strings.HasPrefix(content, "<command-name>") {
				continue
			}
			fmt.Fprintf(&b, "user: %s\n\n", content)

		case "assistant":
			content := extractAssistantContent(raw.Message)
			if content == "" {
				continue
			}
			fmt.Fprintf(&b, "assistant: %s\n\n", content)
		}
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// extractUserContent extracts the content string from a user message.
// message.content is a string for user messages.
func extractUserContent(msg json.RawMessage) string {
	if len(msg) == 0 {
		return ""
	}
	var wrapper struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(msg, &wrapper); err != nil {
		return ""
	}

	// Try as plain string first.
	var s string
	if err := json.Unmarshal(wrapper.Content, &s); err == nil {
		return s
	}

	// Try as array of blocks (same format as assistant).
	return extractTextBlocks(wrapper.Content)
}

// extractAssistantContent extracts text from an assistant message.
// message.content is an array of objects; we keep only type=="text".
func extractAssistantContent(msg json.RawMessage) string {
	if len(msg) == 0 {
		return ""
	}
	var wrapper struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(msg, &wrapper); err != nil {
		return ""
	}
	return extractTextBlocks(wrapper.Content)
}

// extractTextBlocks pulls .text from each object in a content array
// where .type == "text".
func extractTextBlocks(raw json.RawMessage) string {
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, bl := range blocks {
		if bl.Type == "text" && bl.Text != "" {
			parts = append(parts, bl.Text)
		}
	}
	return strings.Join(parts, " ")
}

// tailToBytes returns the tail of s that fits within budget bytes,
// clipping on a UTF-8 rune boundary so the returned string never
// starts with a continuation byte.
func tailToBytes(s string, budget int) string {
	if len(s) <= budget {
		return s
	}
	start := len(s) - budget
	for start < len(s) && start > 0 && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
