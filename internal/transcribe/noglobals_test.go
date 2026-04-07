package transcribe_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPackageLevelMutableState is a structural guard against
// regressions. Phase 2 de-hardcoded the transcribe package by
// removing every package-level mutable variable. If a future change
// re-adds one of these identifiers (apiURL, clientTimeout, model,
// notifyFn, ...), this test fails at build time and the offender is
// forced to extend Options instead.
func TestNoPackageLevelMutableState(t *testing.T) {
	// The package files live next to this test; walk the directory
	// that contains this file (determined via the go test cwd).
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("read dir %s: %v", wd, err)
	}

	// Identifiers we must never re-introduce as package-level vars.
	// Extend this list whenever you rename a field; the failure
	// message points to the declaration site so nobody can sneak
	// state back in.
	banned := map[string]struct{}{
		"apiURL":        {},
		"model":         {},
		"clientTimeout": {},
		"notifyFn":      {},
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
					if _, bad := banned[name.Name]; bad {
						pos := fset.Position(name.Pos())
						t.Errorf("banned package-level var %q at %s:%d — extend transcribe.Options instead",
							name.Name, pos.Filename, pos.Line)
					}
				}
			}
		}
	}

	if !sawAny {
		t.Fatal("no production .go files found — guard is ineffective; check the glob")
	}
}
