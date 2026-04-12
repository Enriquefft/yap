package termscroll

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Enriquefft/yap/pkg/yap/inject"
)

func TestKittySupports(t *testing.T) {
	k := newKittyStrategy()
	tests := []struct {
		name   string
		target inject.Target
		want   bool
	}{
		{"kitty", inject.Target{AppClass: "kitty"}, true},
		{"Kitty", inject.Target{AppClass: "Kitty"}, true},
		{"KITTY", inject.Target{AppClass: "KITTY"}, true},
		{"foot", inject.Target{AppClass: "foot"}, false},
		{"empty", inject.Target{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := k.Supports(tt.target); got != tt.want {
				t.Errorf("Supports() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKittyName(t *testing.T) {
	k := newKittyStrategy()
	if k.Name() != "kitty" {
		t.Errorf("Name() = %q, want %q", k.Name(), "kitty")
	}
}

func TestKittyReadWithFakeScript(t *testing.T) {
	// Create a fake kitty script that outputs canned text with ANSI.
	dir := t.TempDir()
	script := filepath.Join(dir, "kitty")
	content := `#!/bin/sh
echo -e "hello \033[31mred\033[0m world"
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	k := &kittyStrategy{
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Replace kitty with our fake script.
			return exec.CommandContext(ctx, script)
		},
		getenv: func(key string) string {
			if key == "KITTY_LISTEN_ON" {
				return "unix:/tmp/fake-socket"
			}
			return ""
		},
		getuid: func() int { return 1000 },
	}

	text, err := k.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "hello") {
		t.Errorf("Read() = %q, want to contain 'hello'", text)
	}
}

func TestKittyReadNoKitty(t *testing.T) {
	// execCommand that always fails (simulates no kitty on PATH).
	k := &kittyStrategy{
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "false")
		},
		getenv: func(_ string) string { return "" },
		getuid: func() int { return 1000 },
	}

	text, err := k.Read(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}

func TestKittyReadError(t *testing.T) {
	// Simulate kitty returning a non-zero exit (remote control disabled).
	k := &kittyStrategy{
		execCommand: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "exit 1")
		},
		getenv: func(_ string) string { return "" },
		getuid: func() int { return 1000 },
	}

	text, err := k.Read(context.Background())
	if err != nil {
		t.Errorf("expected nil error for graceful skip, got %v", err)
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}

func TestKittyDetectSocketEnv(t *testing.T) {
	k := &kittyStrategy{
		getenv: func(key string) string {
			if key == "KITTY_LISTEN_ON" {
				return "unix:/tmp/kitty-1000-abc"
			}
			return ""
		},
		getuid: func() int { return 1000 },
	}

	socket := k.detectSocket()
	if socket != "unix:/tmp/kitty-1000-abc" {
		t.Errorf("detectSocket() = %q, want %q", socket, "unix:/tmp/kitty-1000-abc")
	}
}

func TestKittyDetectSocketNone(t *testing.T) {
	k := &kittyStrategy{
		getenv: func(_ string) string { return "" },
		getuid: func() int { return 99999 },
	}

	socket := k.detectSocket()
	if socket != "" {
		t.Errorf("detectSocket() = %q, want empty", socket)
	}
}
