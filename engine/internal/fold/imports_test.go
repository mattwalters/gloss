package fold_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// engine/internal/person is on this list for the one definition of the
// person-identifier normalization rule, which fold applies to assignee and
// approval-subject values. "strings" itself is off the list: no non-test file
// here imports it any more.
//
// WRIT-117 changed what that entry reaches. person now imports
// golang.org/x/text/unicode/norm and golang.org/x/text/cases, because the
// normalization rule spec/identifiers.md pins is defined over Unicode tables
// that neither the standard library nor this repository carries. Recorded
// deliberately, because this list is a source-level check and cannot express
// transitive safety — an entry grants whatever that package can reach:
//
//   - Neither x/text package performs I/O. Both are table-driven computation
//     over generated Unicode data: no filesystem, no network, no processes,
//     no clock. cases additionally reaches x/text/language, which is table
//     lookup over language tags.
//   - person's transitive closure does now contain os, reached through fmt,
//     which x/text uses for error formatting. That is true of norm on its own
//     and cannot be avoided by any Go implementation of NFC. It grants fold
//     nothing new in practice: this test reads fold's own imports, so fold
//     calling os would still mean an import here that this list rejects, and
//     person's own source imports strings, unicode/utf8 and the two x/text
//     packages and nothing else. Keep it that way.
//
// fold's own import list is unchanged by all of this: it reaches the rule
// through person, exactly as before.
var allowedImports = map[string]bool{
	`"container/heap"`:                                    true,
	`"encoding/json"`:                                     true,
	`"errors"`:                                            true,
	`"fmt"`:                                               true,
	`"sort"`:                                              true,
	`"github.com/writtendev/writ/engine/codec"`:           true,
	`"github.com/writtendev/writ/engine/internal/person"`: true,
}

func TestImportsAllowlist(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseDir: %v", err)
	}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			if strings.HasSuffix(filename, "_test.go") {
				continue
			}
			for _, imp := range file.Imports {
				pathVal := imp.Path.Value
				if !allowedImports[pathVal] {
					t.Errorf("%s imports forbidden package %s (fold must remain pure and free of I/O)", filepath.Base(filename), pathVal)
				}
			}
		}
	}
}
