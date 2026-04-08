package cli_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/hybridz/yap/internal/ipc"
	"github.com/hybridz/yap/internal/pidfile"
)

// TestStatus_NoDaemon prints the local-fallback JSON shape and
// returns a non-nil error so scripts exit with status 1. The
// fallback must populate Mode, Backend, and Model from the on-disk
// config so operators see what would be active if the daemon were
// running. PID stays empty because there is no live process.
func TestStatus_NoDaemon(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	writeConfigFile(t, cfgFile, `[general]
  hotkey = "KEY_RIGHTCTRL"
  mode = "toggle"

[transcription]
  backend = "whisperlocal"
  model = "tiny.en"
`)

	stdout, _, err := runCLI(t, "status")
	if err == nil {
		t.Fatal("expected status to error when daemon is not running")
	}
	var resp ipc.Response
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); jerr != nil {
		t.Fatalf("status output is not JSON: %v\nstdout=%q", jerr, stdout)
	}
	if resp.Ok {
		t.Errorf("expected ok=false, got ok=true")
	}
	if resp.Error == "" {
		t.Errorf("expected non-empty error in fallback shape")
	}
	if resp.Version == "" {
		t.Errorf("expected local version in fallback shape")
	}
	if resp.Mode != "toggle" {
		t.Errorf("expected fallback mode=toggle, got %q", resp.Mode)
	}
	if resp.Backend != "whisperlocal" {
		t.Errorf("expected fallback backend=whisperlocal, got %q", resp.Backend)
	}
	if resp.Model != "tiny.en" {
		t.Errorf("expected fallback model=tiny.en, got %q", resp.Model)
	}
	if resp.PID != 0 {
		t.Errorf("expected fallback PID to be empty, got %d", resp.PID)
	}
	if resp.ConfigPath == "" {
		t.Errorf("expected fallback to populate ConfigPath")
	}
}

// TestStatus_WithFakeDaemon stands up a real ipc.Server in a
// background goroutine, returns an extended status response, and
// asserts the CLI prints every new field correctly.
func TestStatus_WithFakeDaemon(t *testing.T) {
	tmp := t.TempDir()
	cfgFile := filepath.Join(tmp, "config.toml")
	t.Setenv("YAP_CONFIG", cfgFile)
	t.Setenv("YAP_API_KEY", "")
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	writeConfigFile(t, cfgFile, "[general]\n  hotkey = \"KEY_RIGHTCTRL\"\n")

	// xdg.DataFile honors XDG_DATA_HOME — point it at our scratch
	// dir so the CLI's status path resolves to a socket we own.
	xdg.Reload()
	sockPath, err := pidfile.SocketPath()
	if err != nil {
		t.Fatalf("resolve sock path: %v", err)
	}

	srv, err := ipc.NewServer(sockPath)
	if err != nil {
		t.Fatalf("ipc.NewServer: %v", err)
	}
	defer srv.Close()

	srv.SetStatusFn(func() ipc.Response {
		return ipc.Response{
			Ok:         true,
			State:      "recording",
			Mode:       "hold",
			ConfigPath: cfgFile,
			Version:    "0.1.0-test",
			PID:        99999,
			Backend:    "whisperlocal",
			Model:      "base.en",
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)
	// Brief delay so the listener is ready.
	time.Sleep(50 * time.Millisecond)

	stdout, _, runErr := runCLI(t, "status")
	if runErr != nil {
		t.Fatalf("status: %v", runErr)
	}
	var resp ipc.Response
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &resp); jerr != nil {
		t.Fatalf("status output is not JSON: %v\nstdout=%q", jerr, stdout)
	}
	if !resp.Ok {
		t.Errorf("ok = false, want true")
	}
	if resp.State != "recording" {
		t.Errorf("state = %q, want recording", resp.State)
	}
	if resp.Mode != "hold" {
		t.Errorf("mode = %q, want hold", resp.Mode)
	}
	if resp.ConfigPath != cfgFile {
		t.Errorf("config_path = %q, want %q", resp.ConfigPath, cfgFile)
	}
	if resp.Version != "0.1.0-test" {
		t.Errorf("version = %q, want 0.1.0-test", resp.Version)
	}
	if resp.PID != 99999 {
		t.Errorf("pid = %d, want 99999", resp.PID)
	}
	if resp.Backend != "whisperlocal" {
		t.Errorf("backend = %q, want whisperlocal", resp.Backend)
	}
	if resp.Model != "base.en" {
		t.Errorf("model = %q, want base.en", resp.Model)
	}
}
