package inject_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedGlobals whitelists package-level vars that are legitimate
// sentinels (errors.New return values) rather than mutable state.
// Adding a new entry here is a deliberate decision: read inject.go
// before extending the list.
var allowedGlobals = map[string]struct{}{
	"ErrStrategyUnsupported": {}, // sentinel error; immutable in practice
}

// TestNoUnexpectedGlobals enforces the architecture's "no global
// mutable state" axiom on pkg/yap/inject. Sentinel errors are
// whitelisted by name; everything else is a regression.
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
					t.Errorf("disallowed package-level var %q at %s:%d — move it onto the Injector or Strategy",
						name.Name, pos.Filename, pos.Line)
				}
			}
		}
	}

	if !sawAny {
		t.Fatal("no production .go files found — guard is ineffective")
	}
}
