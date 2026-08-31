package spec

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// ResolutionTarget represents the target tree files in a resolution case.
type ResolutionTarget struct {
	Files map[string]string `json:"files"`
}

// ResolutionCase represents one standalone resolution test case from
// testdata/resolution/cases/*.json. Anchor and Expect are retained as
// json.RawMessage so engine tests can assert byte-level equality.
type ResolutionCase struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Anchor      json.RawMessage  `json:"anchor"`
	Target      ResolutionTarget `json:"target"`
	Expect      json.RawMessage  `json:"expect"`
}

// ResolutionVectors loads all resolution test cases from testdata/resolution/cases/
// in sorted order and validates the corpus shape: at least one case, non-empty and
// unique names, name matching filename, non-empty anchor, non-empty expect.
func ResolutionVectors() ([]ResolutionCase, error) {
	const dir = "testdata/resolution/cases"
	entries, err := FS.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("spec: reading resolution cases directory: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("spec: no resolution cases found in %s", dir)
	}

	var filenames []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			filenames = append(filenames, entry.Name())
		}
	}
	sort.Strings(filenames)

	if len(filenames) == 0 {
		return nil, fmt.Errorf("spec: no json files found in %s", dir)
	}

	cases := make([]ResolutionCase, 0, len(filenames))
	seen := make(map[string]bool, len(filenames))

	for _, filename := range filenames {
		filePath := path.Join(dir, filename)
		raw, err := FS.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("spec: reading %s: %w", filePath, err)
		}

		var c ResolutionCase
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("spec: parsing %s: %w", filePath, err)
		}

		expectedName := strings.TrimSuffix(filename, ".json")
		if c.Name == "" {
			return nil, fmt.Errorf("spec: %s has empty name", filePath)
		}
		if c.Name != expectedName {
			return nil, fmt.Errorf("spec: %s name %q does not match filename %q", filePath, c.Name, expectedName)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("spec: duplicate resolution case name %q in %s", c.Name, filePath)
		}
		seen[c.Name] = true

		if len(c.Anchor) == 0 {
			return nil, fmt.Errorf("spec: case %q has empty anchor", c.Name)
		}
		if len(c.Expect) == 0 {
			return nil, fmt.Errorf("spec: case %q has empty expect", c.Name)
		}

		cases = append(cases, c)
	}

	return cases, nil
}
