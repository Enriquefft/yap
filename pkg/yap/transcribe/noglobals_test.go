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

// TestNoPackageLevelMutableState is a structural guard: pkg/yap/transcribe
// is allowed exactly two package-level identifiers, both of which form
// the backend registry — the lock and the map it protects. Anything
// else (API URLs, default models, HTTP clients, timeouts, notify hooks,
// ...) must go through the Config struct or belong to a sub-package.
//
// Any var or const declaration that does not match the whitelist
// fails this test. ErrUnknownBackend is a sentinel error; it is
// included in the allowlist because it is a package-visible error
// value, conceptually immutable, documented as part of the public API.
func TestNoPackageLevelMutableState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// Allowlist rationale:
	//   registryMu       — sync.RWMutex guarding the registry map.
	//                      Required to make init-time Register calls
	//                      and runtime Get calls concurrency-safe.
	//   registry         — backend name → factory map. Append-only;
	//                      built from init() functions in backend
	//                      sub-packages. Conceptually a compile-time
	//                      constant.
	//   ErrUnknownBackend — sentinel error returned by Get when a
	//                      backend name is not registered. Public API.
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
