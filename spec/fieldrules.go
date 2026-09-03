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
	OpType     string         `json:"op_type,omitempty"`
	OpVersion  int64          `json:"op_version,omitempty"`
	Field      string         `json:"field"`
	Strategy   string         `json:"strategy"`
	Key        []string       `json:"key,omitempty"`
	Lattice    []string       `json:"lattice,omitempty"`
	Normalize  *NormalizeRule `json:"normalize,omitempty"`
	Vocabulary string         `json:"-"`
}

// NormalizeRule specifies the target structural positions for normalization.
type NormalizeRule struct {
	Value string   `json:"value,omitempty"`
	Items string   `json:"items,omitempty"`
	Key   []string `json:"key,omitempty"`
}

// NormalizesKey reports whether keyCol is declared for key normalization.
func (r FieldRule) NormalizesKey(keyCol string) bool {
	if r.Normalize == nil {
		return false
	}
	for _, k := range r.Normalize.Key {
		if k == keyCol {
			return true
		}
	}
	return false
}

// NormalizesValue reports whether scalar value normalization is declared.
func (r FieldRule) NormalizesValue() bool {
	return r.Normalize != nil && r.Normalize.Value == "person"
}

// NormalizesItems reports whether collection element normalization is declared.
func (r FieldRule) NormalizesItems() bool {
	return r.Normalize != nil && r.Normalize.Items == "person"
}

// ruleKey identifies one declared rule for duplicate detection. It is a struct
// rather than a formatted string because every non-test file in this package is
// a fold value path (spec/foldrendering_test.go): `fmt.Errorf` is the only call
// into `fmt` allowed here, and a composite map key needs no formatting at all.
type ruleKey struct {
	Dir       string
	OpType    string
	OpVersion int64
	Field     string
}

// ValidateFieldRule validates an individual field rule definition.
func ValidateFieldRule(r FieldRule) error {
	if r.OpType == "" {
		return fmt.Errorf("rule with empty op_type")
	}
	if r.OpVersion < 1 {
		return fmt.Errorf("rule with invalid op_version: %d", r.OpVersion)
	}
	if r.Field == "" {
		return fmt.Errorf("rule with empty field")
	}
	if !KnownCatalogueStrategies[r.Strategy] {
		return fmt.Errorf("rule for (%s, %s) has unknown strategy %q", r.OpType, r.Field, r.Strategy)
	}
	if r.Strategy == "keyed-lww" && len(r.Key) == 0 {
		return fmt.Errorf("rule for (%s, %s) uses keyed-lww but declares no key", r.OpType, r.Field)
	}
	if r.Strategy == "lattice" && len(r.Lattice) == 0 {
		return fmt.Errorf("rule for (%s, %s) uses lattice but defines no elements", r.OpType, r.Field)
	}
	if r.Normalize != nil {
		if r.Normalize.Value == "" && r.Normalize.Items == "" && len(r.Normalize.Key) == 0 {
			return fmt.Errorf("rule for (%s, %s) declares empty normalize object", r.OpType, r.Field)
		}
		if r.Normalize.Value != "" && r.Normalize.Value != "person" {
			return fmt.Errorf("rule for (%s, %s) declares unknown normalize value algorithm %q", r.OpType, r.Field, r.Normalize.Value)
		}
		if r.Normalize.Items != "" && r.Normalize.Items != "person" {
			return fmt.Errorf("rule for (%s, %s) declares unknown normalize items algorithm %q", r.OpType, r.Field, r.Normalize.Items)
		}
		if r.Normalize.Value != "" && (r.Strategy == "set-observed-remove" || r.Strategy == "set-union") {
			return fmt.Errorf("rule for (%s, %s) declares normalize.value on collection strategy %q", r.OpType, r.Field, r.Strategy)
		}
		if r.Normalize.Items != "" && r.Strategy != "set-observed-remove" && r.Strategy != "set-union" {
			return fmt.Errorf("rule for (%s, %s) declares normalize.items on non-collection strategy %q", r.OpType, r.Field, r.Strategy)
		}
		if len(r.Normalize.Key) > 0 {
			if r.Strategy != "keyed-lww" {
				return fmt.Errorf("rule for (%s, %s) declares normalize.key on non-keyed-lww strategy %q", r.OpType, r.Field, r.Strategy)
			}
			for _, nk := range r.Normalize.Key {
				found := false
				for _, k := range r.Key {
					if k == nk {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("rule for (%s, %s) declares normalized key component %q not in rule key %v", r.OpType, r.Field, nk, r.Key)
				}
			}
		}
	}
	return nil
}

// FieldRules loads all field-rules.json files from the embedded spec.FS and validates each entry
// against the closed catalogue of strategies defined in spec/fold.md.
func FieldRules() ([]FieldRule, error) {
	var allRules []FieldRule
	seen := make(map[ruleKey]bool)

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
			if err := ValidateFieldRule(r); err != nil {
				return fmt.Errorf("spec: %s %w", filePath, err)
			}

			key := ruleKey{Dir: path.Dir(filePath), OpType: r.OpType, OpVersion: r.OpVersion, Field: r.Field}
			if seen[key] {
				return fmt.Errorf("spec: %s has duplicate rule for (%s, %d, %s) under %s",
					filePath, r.OpType, r.OpVersion, r.Field, key.Dir)
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
