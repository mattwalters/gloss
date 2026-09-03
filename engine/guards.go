package writ

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/writtendev/writ/engine/resolve"
)

// commitOIDPattern is the oid grammar the vocabulary schemas declare: a full
// SHA-1 or SHA-256 object id, lowercase hex. Abbreviations and ref names are
// not object ids.
var commitOIDPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// requireCommitOID rejects a value the revision body's oid grammar would
// reject, before any op is built.
//
// This is the domain half of the producer contract: the codec would catch the
// same value, but only as a JSON Pointer into a canonicalized blob. "main" is
// the input a caller actually types, and the fix — resolve the ref first — is
// only obvious if the error says which one it was.
func requireCommitOID(field, value string) error {
	if commitOIDPattern.MatchString(value) {
		return nil
	}
	return fmt.Errorf("writ: %s must be a commit OID, not a ref name: %q", field, value)
}

func validateLabels(add, remove []string) error {
	if len(add) == 0 && len(remove) == 0 {
		return fmt.Errorf("writ: add or remove must be non-empty")
	}
	for _, l := range add {
		if l == "" {
			return fmt.Errorf("writ: label cannot be empty")
		}
	}
	for _, l := range remove {
		if l == "" {
			return fmt.Errorf("writ: label cannot be empty")
		}
	}
	return nil
}

func validateAnchor(a *Anchor) error {
	if a == nil {
		return nil
	}
	if a.Version != 1 {
		return fmt.Errorf("writ: invalid anchor version %d (must be 1)", a.Version)
	}
	if a.Old == nil && a.New == nil {
		return fmt.Errorf("writ: anchor must specify at least one of old or new side")
	}
	if a.Old != nil {
		if err := validateSideAnchor("old", a.Old); err != nil {
			return err
		}
	}
	if a.New != nil {
		if err := validateSideAnchor("new", a.New); err != nil {
			return err
		}
	}
	return nil
}

func validateSideAnchor(side string, s *resolve.SideAnchor) error {
	if s == nil {
		return fmt.Errorf("writ: %s side anchor cannot be nil", side)
	}
	if s.Commit == "" {
		return fmt.Errorf("writ: %s side anchor commit cannot be empty", side)
	}
	if err := requireCommitOID(side+" side anchor commit", s.Commit); err != nil {
		return err
	}
	if s.Path == "" {
		return fmt.Errorf("writ: %s side anchor path cannot be empty", side)
	}
	if strings.HasPrefix(s.Path, "/") {
		return fmt.Errorf("writ: %s side anchor path must be relative, got %q", side, s.Path)
	}
	if s.Blob == "" {
		return fmt.Errorf("writ: %s side anchor blob cannot be empty", side)
	}
	if err := requireCommitOID(side+" side anchor blob", s.Blob); err != nil {
		return err
	}
	if s.Range != nil && s.Context == nil {
		return fmt.Errorf("writ: %s side anchor has range but missing context", side)
	}
	if s.Context != nil && s.Range == nil {
		return fmt.Errorf("writ: %s side anchor has context but missing range", side)
	}
	if s.Range != nil {
		if s.Range.Start < 1 {
			return fmt.Errorf("writ: %s side anchor range start must be >= 1, got %d", side, s.Range.Start)
		}
		if s.Range.End < 1 {
			return fmt.Errorf("writ: %s side anchor range end must be >= 1, got %d", side, s.Range.End)
		}
		if s.Range.End < s.Range.Start {
			return fmt.Errorf("writ: %s side anchor range end (%d) cannot be less than start (%d)", side, s.Range.End, s.Range.Start)
		}
	}
	if s.Context != nil {
		if len(s.Context.Lines) == 0 {
			return fmt.Errorf("writ: %s side anchor context lines cannot be empty", side)
		}
		if len(s.Context.Lines) > 64 {
			return fmt.Errorf("writ: %s side anchor context lines count (%d) exceeds maximum of 64", side, len(s.Context.Lines))
		}
		if len(s.Context.Before) > 3 {
			return fmt.Errorf("writ: %s side anchor context before lines count (%d) exceeds maximum of 3", side, len(s.Context.Before))
		}
		if len(s.Context.After) > 3 {
			return fmt.Errorf("writ: %s side anchor context after lines count (%d) exceeds maximum of 3", side, len(s.Context.After))
		}
		if s.Context.Omitted < 0 {
			return fmt.Errorf("writ: %s side anchor context omitted count cannot be negative", side)
		}
		checkLines := func(label string, lines []string) error {
			for _, line := range lines {
				if strings.Contains(line, "\n") {
					return fmt.Errorf("writ: %s side anchor context %s line cannot contain newlines", side, label)
				}
				if utf8.RuneCountInString(line) > 1000 {
					return fmt.Errorf("writ: %s side anchor context %s line exceeds 1000 characters", side, label)
				}
			}
			return nil
		}
		if err := checkLines("before", s.Context.Before); err != nil {
			return err
		}
		if err := checkLines("lines", s.Context.Lines); err != nil {
			return err
		}
		if err := checkLines("after", s.Context.After); err != nil {
			return err
		}
	}
	return nil
}
