package termscroll

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Enriquefft/yap/pkg/yap/inject"
)

// kittyStrategy reads scrollback from kitty via `kitty @ get-text`.
type kittyStrategy struct {
	// execCommand is the function used to create commands. Injected
	// for testing.
	execCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
	// getenv reads environment variables. Injected for testing.
	getenv func(string) string
	// getuid returns the current user's uid. Injected for testing.
	getuid func() int
}

// newKittyStrategy constructs a kittyStrategy with production defaults.
func newKittyStrategy() *kittyStrategy {
	return &kittyStrategy{
		execCommand: exec.CommandContext,
		getenv:      os.Getenv,
		getuid:      os.Getuid,
	}
}

func (k *kittyStrategy) Name() string { return "kitty" }

func (k *kittyStrategy) Supports(target inject.Target) bool {
	return strings.EqualFold(target.AppClass, "kitty")
}

func (k *kittyStrategy) Read(ctx context.Context) (string, error) {
	socket := k.detectSocket()

	args := []string{"@", "get-text", "--extent=screen"}
	if socket != "" {
		args = []string{"@", "--to=" + socket, "get-text", "--extent=screen"}
	}

	cmd := k.execCommand(ctx, "kitty", args...)
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("termscroll/kitty: command failed", "error", err)
		return "", nil //nolint:nilerr // graceful skip
	}
	return string(out), nil
}

// detectSocket finds the kitty remote control socket.
func (k *kittyStrategy) detectSocket() string {
	if env := k.getenv("KITTY_LISTEN_ON"); env != "" {
		return env
	}

	// Probe for /tmp/kitty-{uid}-* sockets.
	uid := k.getuid()
	pattern := filepath.Join(os.TempDir(), fmt.Sprintf("kitty-%d-*", uid))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return ""
	}
	// Return the first match.
	return "unix:" + matches[0]
}
