package identity_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestLoadReturnsCarryTheReason makes WRIT-143's invariant structural rather
// than conventional.
//
// The invariant — an Identity with an empty PersonID always carries a non-nil
// PersonIDErr — is upheld in Load by convention: a failure returns loadFailed,
// which sets the field, or baseIdent, which already carries whatever
// DerivePersonID said. Nothing stopped the next early return from being
// `return Identity{}, err`, which is precisely the shape the ticket is about,
// and TestLoad_EmptyPersonIDAlwaysExplained enumerates Load's returns by hand
// — the same blind spot as the code. It shipped one return short, and that
// return could have been broken with the suite green.
//
// So read the returns out of the source instead of listing them. Every failing
// return in Load must hand back one of the two shapes that carry a reason; the
// one return that may build an Identity literal is the one that succeeds. A
// bare Identity{} on a failure path fails here by construction, whether or not
// anyone remembered to add a table row for it.
//
// Deliberately narrow: one function, two accepted expressions, no
// configuration. It is a lint in the shape of engine/internal/fold's
// imports_test.go, not a framework — and the table sweep remains the test of
// what the returned values actually contain, which no AST check can see.
func TestLoadReturnsCarryTheReason(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "identity.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse identity.go: %v", err)
	}

	var load *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == "Load" {
			load = fn
			break
		}
	}
	if load == nil {
		t.Fatal("no func Load in identity.go: this test guards Load's returns and has lost track of it")
	}

	var failing, succeeding int
	ast.Inspect(load.Body, func(n ast.Node) bool {
		// Nested function literals return from themselves, not from Load.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if len(ret.Results) != 2 {
			t.Errorf("%s: return with %d results, want 2 (Identity, error)", fset.Position(ret.Pos()), len(ret.Results))
			return true
		}

		if ident, ok := ret.Results[1].(*ast.Ident); ok && ident.Name == "nil" {
			succeeding++
			return true
		}
		failing++

		switch got := ret.Results[0].(type) {
		case *ast.CallExpr:
			if fn, ok := got.Fun.(*ast.Ident); ok && fn.Name == "loadFailed" {
				return true
			}
		case *ast.Ident:
			// baseIdent is built after DerivePersonID has run, so it already
			// carries PersonIDErr when the identifier did not resolve.
			if got.Name == "baseIdent" {
				return true
			}
		}
		t.Errorf("%s: a failing return in Load must hand back loadFailed(err) or baseIdent, so that an empty PersonID carries the reason it is empty (WRIT-143); a bare Identity{} here is the defect the ticket names",
			fset.Position(ret.Pos()))
		return true
	})

	// Both counts are asserted so that a Load rewritten into something this
	// walk no longer sees fails loudly instead of passing vacuously.
	if succeeding != 1 {
		t.Errorf("Load has %d returns with a nil error, want exactly 1", succeeding)
	}
	if failing == 0 {
		t.Error("Load has no failing returns: this test found nothing to check")
	}
}
