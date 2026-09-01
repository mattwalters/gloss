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
// approval-subject values. That package imports strings and nothing else, so
// admitting it grants fold no capability it did not already have. "strings"
// itself comes off the list: no non-test file here imports it any more.
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
