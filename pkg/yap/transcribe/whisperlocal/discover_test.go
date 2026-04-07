package whisperlocal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// fakeFileInfo implements os.FileInfo for the discoverer test stubs.
type fakeFileInfo struct {
	name string
	dir  bool
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() any           { return nil }

// stubDiscoverer composes a discoverer with deterministic hooks. Every
// test case sets the bits it cares about and leaves the rest as fail-
// fast functions so an unintended fallthrough surfaces immediately.
func stubDiscoverer(t *testing.T) *discoverer {
	t.Helper()
	return &discoverer{
		stat: func(path string) (os.FileInfo, error) {
			t.Errorf("unexpected stat(%q)", path)
			return nil, os.ErrNotExist
		},
		lookPath: func(name string) (string, error) {
			t.Errorf("unexpected lookPath(%q)", name)
			return "", errors.New("not configured")
		},
		getenv: func(key string) string {
			return ""
		},
	}
}

func TestDiscoverServer_FromConfig(t *testing.T) {
	d := stubDiscoverer(t)
	d.stat = func(path string) (os.FileInfo, error) {
		if path != "/opt/whisper/whisper-server" {
			t.Errorf("unexpected stat(%q)", path)
		}
		return fakeFileInfo{name: "whisper-server"}, nil
	}
	got, err := d.discoverServer(transcribe.Config{WhisperServerPath: "/opt/whisper/whisper-server"})
	if err != nil {
		t.Fatalf("discoverServer: %v", err)
	}
	if got != "/opt/whisper/whisper-server" {
		t.Errorf("got %q, want %q", got, "/opt/whisper/whisper-server")
	}
}

func TestDiscoverServer_FromConfig_Missing(t *testing.T) {
	d := stubDiscoverer(t)
	d.stat = func(path string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	_, err := d.discoverServer(transcribe.Config{WhisperServerPath: "/no/such/file"})
	if err == nil {
		t.Fatal("expected error for missing config-supplied path")
	}
	if !strings.Contains(err.Error(), "transcription.whisper_server_path") {
		t.Errorf("expected error to mention config field, got %q", err.Error())
	}
}

func TestDiscoverServer_FromEnv(t *testing.T) {
	d := stubDiscoverer(t)
	d.getenv = func(key string) string {
		if key == envServerPath {
			return "/env/whisper-server"
		}
		return ""
	}
	d.stat = func(path string) (os.FileInfo, error) {
		if path != "/env/whisper-server" {
			t.Errorf("unexpected stat(%q)", path)
		}
		return fakeFileInfo{name: "whisper-server"}, nil
	}
	got, err := d.discoverServer(transcribe.Config{})
	if err != nil {
		t.Fatalf("discoverServer: %v", err)
	}
	if got != "/env/whisper-server" {
		t.Errorf("got %q, want %q", got, "/env/whisper-server")
	}
}

func TestDiscoverServer_FromPath(t *testing.T) {
	d := stubDiscoverer(t)
	d.lookPath = func(name string) (string, error) {
		if name != "whisper-server" {
			t.Errorf("unexpected lookPath(%q)", name)
		}
		return "/usr/local/bin/whisper-server", nil
	}
	got, err := d.discoverServer(transcribe.Config{})
	if err != nil {
		t.Fatalf("discoverServer: %v", err)
	}
	if got != "/usr/local/bin/whisper-server" {
		t.Errorf("got %q, want %q", got, "/usr/local/bin/whisper-server")
	}
}

func TestDiscoverServer_NixFallback(t *testing.T) {
	d := stubDiscoverer(t)
	d.lookPath = func(name string) (string, error) {
		return "", errors.New("not on PATH")
	}
	d.stat = func(path string) (os.FileInfo, error) {
		if path == nixProfileFallback {
			return fakeFileInfo{name: "whisper-server"}, nil
		}
		return nil, os.ErrNotExist
	}
	got, err := d.discoverServer(transcribe.Config{})
	if err != nil {
		t.Fatalf("discoverServer: %v", err)
	}
	if got != nixProfileFallback {
		t.Errorf("got %q, want %q", got, nixProfileFallback)
	}
}

func TestDiscoverServer_NotFoundIsHelpful(t *testing.T) {
	d := stubDiscoverer(t)
	d.lookPath = func(name string) (string, error) {
		return "", errors.New("not on PATH")
	}
	d.stat = func(path string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	_, err := d.discoverServer(transcribe.Config{})
	if err == nil {
		t.Fatal("expected error when binary cannot be located")
	}
	msg := err.Error()
	for _, want := range []string{"nix profile install", "brew install", "apt install"} {
		if !strings.Contains(msg, want) {
			t.Errorf("install hint missing %q from error message:\n%s", want, msg)
		}
	}
}

func TestResolveModel_FromModelPath(t *testing.T) {
	dir := t.TempDir()
	modelFile := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(modelFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write fake model: %v", err)
	}
	got, err := resolveModel(transcribe.Config{ModelPath: modelFile})
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if got != modelFile {
		t.Errorf("got %q, want %q", got, modelFile)
	}
}

func TestResolveModel_FromModelPath_Missing(t *testing.T) {
	_, err := resolveModel(transcribe.Config{ModelPath: "/no/such/file"})
	if err == nil {
		t.Fatal("expected error for missing model path")
	}
	if !strings.Contains(err.Error(), "transcription.model_path") {
		t.Errorf("expected error to mention model_path, got %q", err.Error())
	}
}

func TestResolveModel_FromCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	modelFile := filepath.Join(dir, "yap", "models", "ggml-base.en.bin")
	if err := os.MkdirAll(filepath.Dir(modelFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(modelFile, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write fake model: %v", err)
	}
	got, err := resolveModel(transcribe.Config{Model: "base.en"})
	if err != nil {
		t.Fatalf("resolveModel: %v", err)
	}
	if got != modelFile {
		t.Errorf("got %q, want %q", got, modelFile)
	}
}

func TestResolveModel_NotDownloaded_PointsAtCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	_, err := resolveModel(transcribe.Config{Model: "base.en"})
	if err == nil {
		t.Fatal("expected error when model is not yet installed")
	}
	if !strings.Contains(err.Error(), "yap models download base.en") {
		t.Errorf("expected error to point at download command, got %q", err.Error())
	}
}

func TestResolveModel_UnknownModel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	_, err := resolveModel(transcribe.Config{Model: "small.en"})
	if err == nil {
		t.Fatal("expected error for non-pinned model name")
	}
	if !strings.Contains(err.Error(), "not currently pinned") {
		t.Errorf("expected helpful error about pinning, got %q", err.Error())
	}
}

func TestResolveModel_EmptyModel(t *testing.T) {
	_, err := resolveModel(transcribe.Config{})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
	if !strings.Contains(err.Error(), "transcription.model is required") {
		t.Errorf("expected error about required model, got %q", err.Error())
	}
}
