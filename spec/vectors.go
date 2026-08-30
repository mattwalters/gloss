package spec

import (
	"encoding/json"
	"fmt"
)

// Vector is one canonicalization test vector from
// testdata/canonicalization/vectors.json: either a valid case (Canonical
// holds the exact expected output) or a rejection case (Error names the
// rejection category from spec/canonicalization.md). Exactly one of the
// two is set; Canonical is a pointer so an (invalid) empty canonical
// string can't be mistaken for a rejection case.
type Vector struct {
	Name      string  `json:"name"`
	Input     string  `json:"input"`
	Canonical *string `json:"canonical"`
	Error     string  `json:"error"`
}

// CanonicalizationVectors loads the canonicalization vector corpus from
// the embedded spec data and validates its shape: at least one vector,
// unique non-empty names, a non-empty input, and exactly one of
// canonical/error per entry. Every consumer of the corpus should load it
// through here so the shape rules live in one place.
func CanonicalizationVectors() ([]Vector, error) {
	raw, err := FS.ReadFile("testdata/canonicalization/vectors.json")
	if err != nil {
		return nil, fmt.Errorf("spec: reading canonicalization vectors: %w", err)
	}
	var vecs []Vector
	if err := json.Unmarshal(raw, &vecs); err != nil {
		return nil, fmt.Errorf("spec: parsing canonicalization vectors: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("spec: canonicalization vector file has no vectors")
	}
	seen := make(map[string]bool, len(vecs))
	for i, v := range vecs {
		if v.Name == "" {
			return nil, fmt.Errorf("spec: vector %d has no name", i)
		}
		if seen[v.Name] {
			return nil, fmt.Errorf("spec: duplicate vector name %q", v.Name)
		}
		seen[v.Name] = true
		if v.Input == "" {
			return nil, fmt.Errorf("spec: vector %q has no input", v.Name)
		}
		if (v.Canonical == nil) == (v.Error == "") {
			return nil, fmt.Errorf("spec: vector %q must have exactly one of canonical or error", v.Name)
		}
	}
	return vecs, nil
}
