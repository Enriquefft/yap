package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// findRepoRoot walks up from start until it finds a directory
// containing go.mod, which marks the repository root.
func findRepoRoot(t *testing.T, start string) string {
	t.Helper()
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", start)
		}
		dir = parent
	}
}

// TestGoldenNixosModules verifies that the committed nixosModules.nix
// matches the generator output byte-for-byte. Drift means someone
// edited the file by hand or forgot to regenerate after a schema
// change. The fix is always the same: run
//
//	go generate ./pkg/yap/config/...
//
// and commit the result.
func TestGoldenNixosModules(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := findRepoRoot(t, filepath.Dir(self))
	golden := filepath.Join(root, "nixosModules.nix")

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read committed nixosModules.nix: %v", err)
	}

	var got bytes.Buffer
	if err := RenderNixOS(&got); err != nil {
		t.Fatalf("RenderNixOS: %v", err)
	}

	if !bytes.Equal(want, got.Bytes()) {
		t.Errorf("nixosModules.nix is stale. Regenerate with:\n  go generate ./pkg/yap/config/...\n\n"+
			"want (%d bytes):\n%s\n\ngot (%d bytes):\n%s",
			len(want), string(want), got.Len(), got.String())
	}
}

// TestGoldenHomeManagerModules verifies that the committed
// homeManagerModules.nix matches the generator output byte-for-byte.
func TestGoldenHomeManagerModules(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	root := findRepoRoot(t, filepath.Dir(self))
	golden := filepath.Join(root, "homeManagerModules.nix")

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read committed homeManagerModules.nix: %v", err)
	}

	var got bytes.Buffer
	if err := RenderHomeManager(&got); err != nil {
		t.Fatalf("RenderHomeManager: %v", err)
	}

	if !bytes.Equal(want, got.Bytes()) {
		t.Errorf("homeManagerModules.nix is stale. Regenerate with:\n  go generate ./pkg/yap/config/...\n\n"+
			"want (%d bytes):\n%s\n\ngot (%d bytes):\n%s",
			len(want), string(want), got.Len(), got.String())
	}
}

// TestSettingsOptionsInvariant verifies that both module templates
// produce the same `settings = { ... }` block, ensuring the config
// schema stays in sync across NixOS and home-manager modules.
func TestSettingsOptionsInvariant(t *testing.T) {
	var nixos, hm bytes.Buffer
	if err := RenderNixOS(&nixos); err != nil {
		t.Fatalf("RenderNixOS: %v", err)
	}
	if err := RenderHomeManager(&hm); err != nil {
		t.Fatalf("RenderHomeManager: %v", err)
	}

	extractSettings := func(content string) string {
		const marker = "    settings = {"
		start := strings.Index(content, marker)
		if start < 0 {
			return ""
		}
		// Find the matching closing brace at column 0 (after 4-space indent)
		depth := 0
		for i := start; i < len(content); i++ {
			if content[i] == '{' {
				depth++
			} else if content[i] == '}' {
				depth--
				if depth == 0 {
					return content[start : i+1]
				}
			}
		}
		return ""
	}

	sNixos := extractSettings(nixos.String())
	sHM := extractSettings(hm.String())
	if sNixos == "" {
		t.Fatal("could not extract settings block from NixOS module")
	}
	if sHM == "" {
		t.Fatal("could not extract settings block from home-manager module")
	}
	if sNixos != sHM {
		t.Errorf("settings blocks differ between NixOS and home-manager modules:\nNixOS:\n%s\n\nHome-Manager:\n%s", sNixos, sHM)
	}
}
