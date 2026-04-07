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
// package. Anything else is a regression: the only mutable global the
// package needs is the HTTP client used for downloads (so tests can
// swap it for an httptest server). The pinned manifest entries are
// var declarations because Go does not allow composite literals as
// constants — they are append-only at init time and never mutated at
// runtime.
var allowedGlobals = map[string]struct{}{
	"downloadClient":   {}, // HTTP client; whitelisted by name
	"known":            {}, // pinned manifest list (init-only)
	"pinnedAlternates": {}, // helpful-error name list (init-only)
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
