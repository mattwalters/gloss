package spec

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// FieldRule specifies the merge strategy and parameters for an (op_type, field) tuple.
type FieldRule struct {
	OpType     string   `json:"op_type,omitempty"`
	OpVersion  int64    `json:"op_version,omitempty"`
	Field      string   `json:"field"`
	Strategy   string   `json:"strategy"`
	Key        []string `json:"key,omitempty"`
	Lattice    []string `json:"lattice,omitempty"`
	Vocabulary string   `json:"-"`
}

// FieldRules loads all field-rules.json files from the embedded spec.FS and validates each entry
// against the closed catalogue of strategies defined in spec/fold.md.
func FieldRules() ([]FieldRule, error) {
	var allRules []FieldRule
	seen := make(map[string]bool)

	err := fs.WalkDir(FS, "testdata", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(filePath, "field-rules.json") {
			return nil
		}

		raw, err := FS.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("spec: reading %s: %w", filePath, err)
		}

		var rules []FieldRule
		if err := json.Unmarshal(raw, &rules); err != nil {
			return fmt.Errorf("spec: decoding %s: %w", filePath, err)
		}

		vocab := path.Base(path.Dir(filePath))
		for _, r := range rules {
			if r.OpType == "" {
				return fmt.Errorf("spec: %s contains rule with empty op_type", filePath)
			}
			if r.OpVersion < 1 {
				return fmt.Errorf("spec: %s contains rule with invalid op_version: %d", filePath, r.OpVersion)
			}
			if r.Field == "" {
				return fmt.Errorf("spec: %s contains rule with empty field", filePath)
			}
			if !KnownCatalogueStrategies[r.Strategy] {
				return fmt.Errorf("spec: %s rule for (%s, %s) has unknown strategy %q", filePath, r.OpType, r.Field, r.Strategy)
			}
			if r.Strategy == "keyed-lww" && len(r.Key) == 0 {
				return fmt.Errorf("spec: %s rule for (%s, %s) uses keyed-lww but declares no key", filePath, r.OpType, r.Field)
			}
			if r.Strategy == "lattice" && len(r.Lattice) == 0 {
				return fmt.Errorf("spec: %s rule for (%s, %s) uses lattice but defines no elements", filePath, r.OpType, r.Field)
			}

			key := fmt.Sprintf("%s:%s:%d:%s", path.Dir(filePath), r.OpType, r.OpVersion, r.Field)
			if seen[key] {
				return fmt.Errorf("spec: %s has duplicate rule for %s", filePath, key)
			}
			seen[key] = true

			r.Vocabulary = vocab
			allRules = append(allRules, r)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("spec: loading field rules: %w", err)
	}

	return allRules, nil
}
