package transform_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPackageLevelMutableState guards the transform package against
// re-introducing package-level vars outside of the backend registry.
// The allowlist matches pkg/yap/transcribe: the registry map, its
// mutex, and the sentinel error returned by Get.
func TestNoPackageLevelMutableState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// Allowlist rationale:
	//   registryMu       — sync.RWMutex guarding the registry map.
	//   registry         — backend name → factory map (append-only).
	//   ErrUnknownBackend — sentinel error returned by Get.
	allowed := map[string]struct{}{
		"registryMu":        {},
		"registry":          {},
		"ErrUnknownBackend": {},
	}

	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("read dir: %v", err)
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
			if !ok {
				continue
			}
			if gen.Tok != token.VAR && gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					if _, ok := allowed[name.Name]; ok {
						continue
					}
					pos := fset.Position(name.Pos())
					t.Errorf("disallowed package-level %s %q at %s:%d — move it into Config or a sub-package",
						gen.Tok, name.Name, pos.Filename, pos.Line)
				}
			}
		}
	}

	if !sawAny {
		t.Fatal("no production .go files found — guard is ineffective")
	}
}
