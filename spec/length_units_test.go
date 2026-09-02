package spec_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/writtendev/writ/spec"
)

// The length bounds in the schemas (identifiers.schema.json,
// anchor.schema.json) are JSON Schema maxLength keywords, which count Unicode
// code points. Bytes and UTF-16 code units are the two units an
// implementation reaches for by accident instead: Go's len, Rust's
// String::len and Python's len(bytes) count octets; JavaScript's
// String.prototype.length and Java's String.length() count UTF-16 code units.
//
// Prose cannot stop any of that. AGENTS.md is explicit that "the spec is the
// conformance fixtures, not the prose", so the corpus has to be able to tell
// the three units apart on its own — a bound only one implementation's own
// test suite pins is not specified.
//
// This file is the executable form of that claim. It does not assert that the
// corpus discriminates; it computes, for each wrong unit, a vector the corpus
// calls valid whose length in that unit is over the bound. A validator
// counting in that unit rejects that vector, and so fails the corpus. If a
// future edit removes the last such vector, this test fails and says which
// unit went blind.
//
// Only that direction is checkable. Bytes and UTF-16 code units are each at
// least the code-point count for every string, so neither can ever accept
// something the code-point bound rejects; there is no over-limit vector that
// separates them and this test does not look for one.

// lengthUnits are the three ways an implementation might count a maxLength.
var lengthUnits = []struct {
	name  string
	count func(string) int
}{
	{"code points", utf8.RuneCountInString},
	{"bytes", func(s string) int { return len(s) }},
	{"UTF-16 code units", func(s string) int { return len(utf16.Encode([]rune(s))) }},
}

// schemaMaxLength reads a $defs entry's maxLength out of the given schema
// file, so this test cannot drift from the number the schema actually
// declares.
func schemaMaxLength(t *testing.T, schemaFile, def string) int {
	t.Helper()
	raw, err := spec.FS.ReadFile(schemaFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			MaxLength *int `json:"maxLength"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding %s: %v", schemaFile, err)
	}
	d, ok := doc.Defs[def]
	if !ok {
		t.Fatalf("%s has no $defs/%s", schemaFile, def)
	}
	if d.MaxLength == nil {
		t.Fatalf("$defs/%s declares no maxLength in %s", def, schemaFile)
	}
	return *d.MaxLength
}

// corpusStrings loads the bounded strings out of one valid-vector directory.
func corpusStrings(t *testing.T, dir string, field func([]byte) (string, bool)) map[string]string {
	t.Helper()
	out := make(map[string]string)
	for _, name := range readDirNames(t, dir) {
		raw, err := spec.FS.ReadFile(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := field(raw)
		if !ok {
			continue
		}
		out[name] = s
	}
	if len(out) == 0 {
		t.Fatalf("%s yielded no vectors", dir)
	}
	return out
}

// checkUnitsAreDistinguishable is the whole test, once per bounded string
// type: every valid vector must fit the bound in code points, and every unit
// that is not code points must be caught out by at least one of them.
func checkUnitsAreDistinguishable(t *testing.T, bound int, vectors map[string]string) {
	t.Helper()
	for _, unit := range lengthUnits {
		var witness string
		var witnessLen int
		for name, s := range vectors {
			n := unit.count(s)
			if n <= bound {
				continue
			}
			// Deterministic witness: the corpus is a map, so pick the
			// lexicographically first name that works rather than whichever
			// the runtime hands over first.
			if witness == "" || name < witness {
				witness, witnessLen = name, n
			}
		}
		if unit.name == "code points" {
			if witness != "" {
				t.Errorf("%s is %d code points, over the bound of %d — the corpus contradicts itself",
					witness, witnessLen, bound)
			}
			continue
		}
		if witness == "" {
			t.Errorf("no valid vector separates a bound counted in %s from one counted in code points: "+
				"an implementation counting %s passes this corpus with the wrong unit. "+
				"Add an at-limit vector whose length in %s exceeds %d.",
				unit.name, unit.name, unit.name, bound)
			continue
		}
		t.Logf("a bound counted in %s rejects %s (%d %s, bound %d), which the corpus requires to be accepted",
			unit.name, witness, witnessLen, unit.name, bound)
	}
}

// TestPersonIDLengthUnitIsCodePoints pins the unit of person-id's maxLength
// against the persons corpus.
func TestPersonIDLengthUnitIsCodePoints(t *testing.T) {
	bound := schemaMaxLength(t, "schemas/identifiers.schema.json", "person-id")
	vectors := corpusStrings(t, "testdata/persons/valid", func(raw []byte) (string, bool) {
		var v personVector
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", false
		}
		// The bound applies to the normalized form, which is the string the
		// schema is asked about in TestValidPersonVectors.
		return v.Normalized, v.Normalized != ""
	})
	checkUnitsAreDistinguishable(t, bound, vectors)
}

// TestReferenceLengthUnitIsCodePoints does the same for reference, one of
// the three bounded strings (person-id, reference, anchor line) whose pattern
// admits non-ASCII and so where the unit is observable.
func TestReferenceLengthUnitIsCodePoints(t *testing.T) {
	bound := schemaMaxLength(t, "schemas/identifiers.schema.json", "reference")
	vectors := corpusStrings(t, "testdata/references/valid", func(raw []byte) (string, bool) {
		var v struct {
			Reference string `json:"reference"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", false
		}
		return v.Reference, v.Reference != ""
	})
	checkUnitsAreDistinguishable(t, bound, vectors)
}

// TestAnchorLineLengthUnitIsCodePoints does the same for anchor context lines,
// traversing before, lines, and after across old and new sides of valid anchor
// vectors.
func TestAnchorLineLengthUnitIsCodePoints(t *testing.T) {
	bound := schemaMaxLength(t, "schemas/anchor.schema.json", "line")
	type anchorSide struct {
		Context *struct {
			Before []string `json:"before"`
			Lines  []string `json:"lines"`
			After  []string `json:"after"`
		} `json:"context"`
	}
	type anchorVector struct {
		Old *anchorSide `json:"old"`
		New *anchorSide `json:"new"`
	}

	vectors := make(map[string]string)
	for _, name := range readDirNames(t, "testdata/anchors/valid") {
		raw, err := spec.FS.ReadFile("testdata/anchors/valid/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var v anchorVector
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}
		collectSide := func(sideName string, side *anchorSide) {
			if side == nil || side.Context == nil {
				return
			}
			collectList := func(listName string, list []string) {
				for i, s := range list {
					key := fmt.Sprintf("%s:%s.%s[%d]", name, sideName, listName, i)
					vectors[key] = s
				}
			}
			collectList("before", side.Context.Before)
			collectList("lines", side.Context.Lines)
			collectList("after", side.Context.After)
		}
		collectSide("old", v.Old)
		collectSide("new", v.New)
	}
	if len(vectors) == 0 {
		t.Fatal("testdata/anchors/valid yielded no context lines")
	}
	checkUnitsAreDistinguishable(t, bound, vectors)
}
