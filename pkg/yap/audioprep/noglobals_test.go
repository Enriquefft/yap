package audioprep_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPackageLevelVars asserts that the audioprep package has zero
// package-level mutable state — the project-wide invariant from CLAUDE.md.
// The only allowed package-level identifiers are types, consts, and funcs.
func TestNoPackageLevelVars(t *testing.T) {
	dir := filepath.Join("..", "audioprep")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		node, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range node.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if gen.Tok == token.CONST || gen.Tok == token.TYPE {
				continue
			}
			if gen.Tok == token.VAR {
				t.Errorf("%s: package-level var declaration found (gen decl): %s",
					path, fset.Position(gen.Pos()))
			}
		}
		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "init" {
				t.Errorf("%s: init function found — potential package-level mutation",
					path)
			}
		}
	}
}
