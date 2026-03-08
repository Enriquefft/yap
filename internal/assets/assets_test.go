package assets_test

import (
	"io"
	"testing"

	"github.com/hybridz/yap/internal/assets"
)

func TestAssetsPresent(t *testing.T) {
	r, err := assets.StartChime()
	if err != nil {
		t.Fatalf("StartChime() error: %v", err)
	}
	if r == nil {
		t.Fatal("StartChime() returned nil reader")
	}

	r, err = assets.StopChime()
	if err != nil {
		t.Fatalf("StopChime() error: %v", err)
	}
	if r == nil {
		t.Fatal("StopChime() returned nil reader")
	}
}

func TestAssetsSize(t *testing.T) {
	const maxSize = 100 * 1024 // 100KB per ASSETS-02

	for _, name := range []string{"start.wav", "stop.wav"} {
		data, err := assets.FS.ReadFile(name)
		if err != nil {
			t.Fatalf("ReadFile(%s) error: %v", name, err)
		}
		if len(data) >= maxSize {
			t.Errorf("%s size %d bytes >= 100KB limit (%d bytes)", name, len(data), maxSize)
		}
		t.Logf("%s: %d bytes (%.1f KB)", name, len(data), float64(len(data))/1024)
	}
}

func TestListAssets(t *testing.T) {
	names, err := assets.ListAssets()
	if err != nil {
		t.Fatalf("ListAssets() error: %v", err)
	}

	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}

	for _, required := range []string{"start.wav", "stop.wav"} {
		if !found[required] {
			t.Errorf("ListAssets(): missing %s; got %v", required, names)
		}
	}
}

func TestStartChimeReadable(t *testing.T) {
	r, err := assets.StartChime()
	if err != nil {
		t.Fatal(err)
	}
	// Read first 44 bytes — minimum WAV header size.
	buf := make([]byte, 44)
	n, err := io.ReadFull(r, buf)
	if err != nil {
		t.Fatalf("reading WAV header: %v (read %d bytes)", err, n)
	}
	// RIFF magic bytes: 'R','I','F','F'
	if string(buf[0:4]) != "RIFF" {
		t.Errorf("start.wav does not begin with RIFF magic; got %q", string(buf[0:4]))
	}
}
