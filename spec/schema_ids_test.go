package spec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/spec"
)

// findRefs recursively traverses a parsed JSON data structure and collects
// all values associated with the "$ref" key.
func findRefs(v any) []string {
	var refs []string
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					refs = append(refs, s)
				}
			} else {
				refs = append(refs, findRefs(child)...)
			}
		}
	case []any:
		for _, child := range val {
			refs = append(refs, findRefs(child)...)
		}
	}
	return refs
}

// TestSchemaIDsAndRefs enforces the schema identity and cross-reference rule:
//  1. Every schema in spec/schemas/ must declare a $id equal to
//     "https://writ.dev/spec/" + filename.
//  2. Every absolute $ref across all schemas must resolve to a known schema's $id.
func TestSchemaIDsAndRefs(t *testing.T) {
	entries, err := spec.FS.ReadDir("schemas")
	if err != nil {
		t.Fatalf("reading schemas directory: %v", err)
	}

	const expectedPrefix = "https://writ.dev/spec/"

	knownIDs := make(map[string]string) // id -> filename
	schemaDocs := make(map[string]map[string]any)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, err := spec.FS.ReadFile("schemas/" + entry.Name())
		if err != nil {
			t.Fatalf("reading schema %s: %v", entry.Name(), err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parsing schema %s: %v", entry.Name(), err)
		}

		idVal, ok := doc["$id"].(string)
		if !ok || idVal == "" {
			t.Errorf("schema %s is missing $id", entry.Name())
			continue
		}

		wantID := expectedPrefix + entry.Name()
		if idVal != wantID {
			t.Errorf("schema %s has $id = %q, want %q", entry.Name(), idVal, wantID)
		}

		knownIDs[idVal] = entry.Name()
		schemaDocs[entry.Name()] = doc
	}

	if len(knownIDs) == 0 {
		t.Fatal("no schemas found to validate")
	}

	for filename, doc := range schemaDocs {
		refs := findRefs(doc)
		for _, ref := range refs {
			if strings.HasPrefix(ref, "#") {
				// Local definition reference (e.g. #/$defs/...)
				continue
			}
			if _, ok := knownIDs[ref]; !ok {
				t.Errorf("schema %s has absolute $ref %q which does not match any sibling schema $id in spec/schemas", filename, ref)
			}
		}
	}
}
