package spec_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Writ's folded state must be reproducible byte-for-byte by an implementation
// written in any language from spec/fold.md alone. Go's `fmt` verbs are the
// one thing in this codebase that can quietly destroy that: `fmt.Sprint` on an
// `any` renders a map as `map[email:carol@example.com]` and a slice as
// `[a@b.com]`, which are Go syntax. Both of those shipped as normative fold
// keys (WRIT-124), and `fmt.Sprint(nil)` shipped as the string `<nil>` in an
// assignee set (WRIT-126).
//
// The conformance corpus cannot catch this class, and that is why this test
// exists rather than another fixture. The corpus byte-compares two Go
// implementations against each other, so where both call `fmt.Sprint` they
// agree perfectly — measured on main at f324f58, `spec.Fold` and `writ.Fold`
// emitted `map[email:carol@example.com]` as the same fold key, and every
// vector passed. Agreement is not the property at stake; implementability
// elsewhere is, and only reading the source can see the difference.
//
// The rule: on a fold value path, `fmt.Errorf` is the only `fmt` function
// allowed. Nothing else in `fmt` turns a value into bytes that a reader in
// another language could be expected to reproduce. Where a fold needs a value
// as a string, the value is a string — an op whose declared field carries
// anything else is uninterpretable and never reaches a reducer
// (spec/fold.md §7.1) — so a type assertion says what is meant and this says
// so when it does not.
//
// Scoped to the fold, deliberately. Elsewhere in the repo `fmt.Sprintf` builds
// log lines, CLI output and map keys that are never anybody's normative bytes.
var foldValuePaths = []string{
	"reffold.go",
	"../engine/internal/fold",
	"../engine/state",
}

// allowedFmtFuncs are the fmt functions a fold value path may call.
var allowedFmtFuncs = map[string]bool{"Errorf": true}

func TestNoGoRenderingOnFoldValuePaths(t *testing.T) {
	var checked int
	for _, target := range foldValuePaths {
		for _, path := range goFilesUnder(t, target) {
			checked++
			checkFileForFmtRendering(t, path)
		}
	}
	// A path that silently matched nothing — a rename, a moved package — would
	// make this test pass by covering nothing at all.
	if checked < len(foldValuePaths) {
		t.Fatalf("guarded only %d files across %d paths; a fold value path has moved",
			checked, len(foldValuePaths))
	}
}

// goFilesUnder returns the non-test Go files at target, which is either one
// file or a directory.
func goFilesUnder(t *testing.T, target string) []string {
	t.Helper()
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("fold value path %s is gone; update this test deliberately: %v", target, err)
	}
	if !info.IsDir() {
		return []string{target}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("reading %s: %v", target, err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(target, name))
	}
	if len(files) == 0 {
		t.Fatalf("fold value path %s holds no non-test Go files; update this test deliberately", target)
	}
	return files
}

func checkFileForFmtRendering(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	// Track the local name of the fmt import: a file that renames it must not
	// slip past the selector check.
	fmtName := ""
	for _, imp := range file.Imports {
		if imp.Path.Value != `"fmt"` {
			continue
		}
		fmtName = "fmt"
		if imp.Name != nil {
			fmtName = imp.Name.Name
		}
	}
	if fmtName == "" || fmtName == "_" {
		return
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != fmtName {
			return true
		}
		if allowedFmtFuncs[sel.Sel.Name] {
			return true
		}
		t.Errorf("%s: %s.%s renders a value using Go's formatting verbs on a fold value path.\n"+
			"Folded state must be reproducible from spec/fold.md by an implementation in any "+
			"language, and fmt's rendering of a map, slice or nil is Go syntax that no other "+
			"implementation produces. Assert the type you mean instead; fmt.Errorf is the only "+
			"fmt function allowed here.",
			fset.Position(call.Pos()), fmtName, sel.Sel.Name)
		return true
	})
}
