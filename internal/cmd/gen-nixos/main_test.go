package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestGoldenNixosModules verifies that the committed nixosModules.nix
// matches the generator output byte-for-byte. Drift means someone
// edited the file by hand or forgot to regenerate after a schema
// change. The fix is always the same: run
//
//	go generate ./pkg/yap/config/...
//
// and commit the result.
func TestGoldenNixosModules(t *testing.T) {
	// Walk up from this test file's location until we find go.mod
	// (the repository root). This avoids hard-coding a relative
	// path that breaks when go test is invoked from a different
	// working directory.
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
	if err := Render(&got); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !bytes.Equal(want, got.Bytes()) {
		t.Errorf("nixosModules.nix is stale. Regenerate with:\n  go generate ./pkg/yap/config/...\n\n"+
			"want (%d bytes):\n%s\n\ngot (%d bytes):\n%s",
			len(want), string(want), got.Len(), got.String())
	}
}

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
