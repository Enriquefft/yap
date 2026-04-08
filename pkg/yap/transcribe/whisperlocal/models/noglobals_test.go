package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedGlobals is the whitelist of package-level vars in the models
// package. The Manager refactor eliminated the per-test mutation
// pattern; the remaining package-level vars are the production
// manifest (init-only, never mutated at runtime), the lazily-built
// production singleton, its sync.Once guard, and the sync.Mutex
// that serializes CacheDir calls against the adrg/xdg vendor
// library (which races its own package globals on concurrent
// Reload calls).
var allowedGlobals = map[string]struct{}{
	"known":          {}, // pinned manifest list (init-only)
	"defaultManager": {}, // lazily-built production singleton
	"defaultOnce":    {}, // sync.Once guard for defaultManager
	"cacheDirMu":     {}, // serializes CacheDir against adrg/xdg races
}

// TestNoUnexpectedGlobals walks every production .go file under the
// models package and fails the build if any package-level var
// declaration is not in allowedGlobals.
func TestNoUnexpectedGlobals(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("read dir %s: %v", wd, err)
	}

	fset := token.NewFileSet()
	sawAny := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		sawAny = true

		path := filepath.Join(wd, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if _, allowed := allowedGlobals[name.Name]; allowed {
						continue
					}
					pos := fset.Position(name.Pos())
					t.Errorf("disallowed package-level var %q at %s:%d",
						name.Name, pos.Filename, pos.Line)
				}
			}
		}
	}

	if !sawAny {
		t.Fatal("no production .go files found — guard is ineffective")
	}
}
