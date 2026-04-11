package inject

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoPackageLevelMutableState is a structural guard. The inject
// package is allowed exactly the following package-level identifiers:
//
//   - terminalClasses, electronClasses, browserClasses — the three
//     allowlists driving classify().
//   - electronRestoreDelay — the bounded clipboard-restore wait used
//     by the electron strategy through Deps.SleepCtx.
//   - focusPollInterval, focusPollMaxAttempts — the X11 focus poll
//     loop tuning constants.
//   - maxResolveTTYNodes — the BFS cap in OSC52 tty resolution.
//   - finalDeliveryBudget — the InjectStream flush-on-cancel
//     deadline.
//   - ErrNoDisplay — sentinel error returned when no display server
//     is detected.
//   - errWlrootsProtocolUnsupported, errWlrootsNoFocusedWindow —
//     sentinel errors returned by the wlroots foreign-toplevel
//     detector to drive fall-through.
//   - detectWlrootsLatencyBudget — the hard 500 ms ceiling on a
//     single wlroots detection roundtrip.
//   - reasonAppOverride, reasonDefaultStrategy, reasonNaturalOrder,
//     reasonNoneApplicable — stable human-readable tokens surfaced
//     by buildStrategyOrder to the Resolve debug surface. They are
//     immutable string constants, not mutable state, and keeping
//     them named prevents drift between the selection logic and
//     the Resolve output format.
//
// Anything else (var or const) at package scope must be moved into a
// Deps field, an InjectionOptions field, or a function-local
// definition. The guard fails the build if a new global appears
// without an explicit allowlist entry.
func TestNoPackageLevelMutableState(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	allowed := map[string]struct{}{
		"terminalClasses":               {},
		"electronClasses":               {},
		"browserClasses":                {},
		"electronRestoreDelay":          {},
		"focusPollInterval":             {},
		"focusPollMaxAttempts":          {},
		"maxResolveTTYNodes":            {},
		"finalDeliveryBudget":           {},
		"ErrNoDisplay":                  {},
		"errWlrootsProtocolUnsupported": {},
		"errWlrootsNoFocusedWindow":     {},
		"detectWlrootsLatencyBudget":    {},
		"reasonAppOverride":             {},
		"reasonDefaultStrategy":         {},
		"reasonNaturalOrder":            {},
		"reasonNoneApplicable":          {},
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
					t.Errorf("disallowed package-level %s %q at %s:%d — move it into Deps, InjectionOptions, or a function-local definition",
						gen.Tok, name.Name, pos.Filename, pos.Line)
				}
			}
		}
	}

	if !sawAny {
		t.Fatal("no production .go files found — guard is ineffective")
	}
}

// TestNoLiteralStdlibSleep is the second structural guard demanded
// by the Phase 4 plan §6: every blocking wait in the inject package
// must route through Deps.Sleep so tests have a single hook for time
// control. The guard greps the production source files for the
// stdlib blocking-sleep token (assembled at runtime so this guard
// itself does not trip the grep verification).
func TestNoLiteralStdlibSleep(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	entries, err := os.ReadDir(wd)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	// Assemble the forbidden token at runtime so the grep verification
	// command in the Phase 4 instructions does not match this guard
	// file itself.
	forbidden := []byte("time" + "." + "Sleep")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(wd, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if bytesIndex(data, forbidden) >= 0 {
			t.Errorf("%s contains literal stdlib blocking sleep — every wait must route through Deps.Sleep", path)
		}
	}
}

// bytesIndex is a tiny re-implementation of bytes.Index to avoid
// pulling the bytes package into this guard's import set. The cost is
// negligible compared to the safety of an explicit byte search.
func bytesIndex(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
