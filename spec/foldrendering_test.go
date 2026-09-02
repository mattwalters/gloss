package spec_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Writ's folded state must be reproducible byte-for-byte by an implementation
// written in any language from spec/fold.md alone. Go's value-to-string
// conversions are the one thing in this codebase that can quietly destroy
// that: `fmt.Sprint` on an `any` renders a map as `map[email:carol@example.com]`
// and a slice as `[a@b.com]`, which are Go syntax. Both of those shipped as
// normative fold keys (WRIT-124), and `fmt.Sprint(nil)` shipped as the string
// `<nil>` in an assignee set (WRIT-126). `strconv` is the same hazard with a
// different spelling: `strconv.FormatFloat(1e21, 'g', -1, 64)` is `1e+21`,
// whose exponent form is Go's choice and not JSON's.
//
// The conformance corpus cannot catch this class, and that is why this test
// exists rather than another fixture. The corpus byte-compares two Go
// implementations against each other, so where both render the same way they
// agree perfectly — measured on main at f324f58, `spec.Fold` and `writ.Fold`
// emitted `map[email:carol@example.com]` as the same fold key, and every
// vector passed. Agreement is not the property at stake; implementability
// elsewhere is, and only reading the source can see the difference.
//
// It is load-bearing beyond the code it names. Now that a declared field
// carrying a non-string is uninterpretable (spec/fold.md §7.1), the stringifying
// code is unreachable for the very inputs that would expose it, so
// reintroducing `fmt.Sprint` on an item path leaves every merge vector passing.
// The vectors cannot see this regression any more. This test is what does.
//
// The rule: on a fold value path, `fmt.Errorf` is the only call into `fmt` or
// `strconv` allowed. Nothing else in either package turns a value into bytes
// that a reader in another language could be expected to reproduce. Where a
// fold needs a value as a string, the value is a string — an op whose declared
// field carries anything else is uninterpretable and never reaches a reducer —
// so a type assertion says what is meant and this says so when it does not.
//
// Scoped to the fold, deliberately. Elsewhere in the repo `fmt.Sprintf` builds
// log lines, CLI output and map keys that are never anybody's normative bytes.
var foldValuePaths = []string{
	"reffold.go",
	"../engine/internal/fold",
	"../engine/state",
}

// renderingPackages are the packages a fold value path may not render values
// with, mapped to the functions it may still call in them.
//
// engine/internal/fold additionally carries an import allowlist
// (engine/internal/fold/imports_test.go) that keeps strconv out of that package
// entirely. engine/state and spec have no such allowlist, which is why this
// check names the package rather than relying on one.
var renderingPackages = map[string]map[string]bool{
	"fmt":     {"Errorf": true},
	"strconv": {},
}

func TestNoGoRenderingOnFoldValuePaths(t *testing.T) {
	var checked int
	for _, target := range foldValuePaths {
		for _, path := range goFilesUnder(t, target) {
			checked++
			for _, finding := range goRenderingFindings(t, path) {
				t.Error(finding)
			}
		}
	}
	// A path that silently matched nothing — a rename, a moved package — would
	// make this test pass by covering nothing at all.
	if checked < len(foldValuePaths) {
		t.Fatalf("guarded only %d files across %d paths; a fold value path has moved",
			checked, len(foldValuePaths))
	}
}

// TestNoGoRenderingLintBites pins the check against the two ways it could
// silently cover nothing: a qualified call it must catch, and a dot-import
// that makes the same call unqualified. The dot-import case is not
// hypothetical — `import . "fmt"` turns `fmt.Sprint(item)` into a bare
// `Sprint(item)`, which is not a selector expression at all, so a check that
// only inspects selectors reports nothing and passes. Nothing else in this
// repository flags a dot import: there is no .golangci.yml, and no linter
// enabled by default rejects one.
func TestNoGoRenderingLintBites(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "qualified fmt.Sprint",
			src: "package p\n\nimport \"fmt\"\n\n" +
				"func f(v any) string { return fmt.Sprint(v) }\n",
			want: "fmt.Sprint",
		},
		{
			name: "renamed fmt import",
			src: "package p\n\nimport gofmt \"fmt\"\n\n" +
				"func f(v any) string { return gofmt.Sprint(v) }\n",
			want: "gofmt.Sprint",
		},
		{
			name: "dot-imported fmt",
			src: "package p\n\nimport . \"fmt\"\n\n" +
				"func f(v any) string { return Sprint(v) }\n",
			want: "dot-imports",
		},
		{
			name: "strconv rendering",
			src: "package p\n\nimport \"strconv\"\n\n" +
				"func f(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }\n",
			want: "strconv.FormatFloat",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sample.go")
			if err := os.WriteFile(path, []byte(tc.src), 0o644); err != nil {
				t.Fatalf("writing sample: %v", err)
			}
			findings := goRenderingFindings(t, path)
			if len(findings) == 0 {
				t.Fatalf("check reported nothing for %s; it covers nothing", tc.name)
			}
			if !strings.Contains(strings.Join(findings, "\n"), tc.want) {
				t.Errorf("finding does not name %q:\n%s", tc.want, strings.Join(findings, "\n"))
			}
		})
	}

	// And the converse: the one allowed call must not be reported, or the
	// check would be noise nobody could keep green.
	path := filepath.Join(t.TempDir(), "allowed.go")
	src := "package p\n\nimport \"fmt\"\n\nfunc f() error { return fmt.Errorf(\"boom\") }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("writing sample: %v", err)
	}
	if findings := goRenderingFindings(t, path); len(findings) != 0 {
		t.Errorf("fmt.Errorf reported: %s", strings.Join(findings, "\n"))
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

// goRenderingFindings returns one finding per call into a rendering package
// that the fold value path rule forbids, plus one per dot import of such a
// package.
func goRenderingFindings(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var findings []string

	// Track the local name each rendering package is imported under: a file
	// that renames it must not slip past the selector check, and one that
	// dot-imports it makes every call a bare identifier the selector check
	// cannot see at all. A dot import is therefore the finding.
	localNames := make(map[string]string) // local name -> package path
	var dotImported []string
	for _, imp := range file.Imports {
		pkg := strings.Trim(imp.Path.Value, `"`)
		if _, forbidden := renderingPackages[pkg]; !forbidden {
			continue
		}
		local := pkg
		if imp.Name != nil {
			local = imp.Name.Name
		}
		switch local {
		case "_":
			continue
		case ".":
			dotImported = append(dotImported, pkg)
			continue
		}
		localNames[local] = pkg
	}

	sort.Strings(dotImported)
	for _, pkg := range dotImported {
		findings = append(findings, fmt.Sprintf(
			"%s: dot-imports %q on a fold value path.\n"+
				"A dot import makes every call to it an unqualified identifier, which this check "+
				"cannot distinguish from a local function — %s.Sprint(v) written as Sprint(v) would "+
				"go unreported. Import it under its own name so the rule below can be enforced.",
			fset.Position(imp0Pos(file, pkg)), pkg, pkg))
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
		if !ok {
			return true
		}
		pkg, forbidden := localNames[ident.Name]
		if !forbidden {
			return true
		}
		if renderingPackages[pkg][sel.Sel.Name] {
			return true
		}
		findings = append(findings, fmt.Sprintf(
			"%s: %s.%s renders a value using Go's own formatting on a fold value path.\n"+
				"Folded state must be reproducible from spec/fold.md by an implementation in any "+
				"language, and Go's rendering of a map, a slice, nil or a float exponent is Go "+
				"syntax that no other implementation produces. Assert the type you mean instead; "+
				"fmt.Errorf is the only call into fmt or strconv allowed here.",
			fset.Position(call.Pos()), ident.Name, sel.Sel.Name))
		return true
	})

	return findings
}

// imp0Pos returns the position of the import of pkg, for a readable message.
func imp0Pos(file *ast.File, pkg string) token.Pos {
	for _, imp := range file.Imports {
		if strings.Trim(imp.Path.Value, `"`) == pkg {
			return imp.Pos()
		}
	}
	return file.Pos()
}
