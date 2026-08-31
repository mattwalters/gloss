package state_test

import (
	"testing"

	s "github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

func TestReferenceVectorsValidCorpus(t *testing.T) {
	vectors, err := spec.ReferenceVectors()
	if err != nil {
		t.Fatalf("spec.ReferenceVectors failed: %v", err)
	}

	if len(vectors) == 0 {
		t.Fatal("expected non-empty reference vectors from spec corpus")
	}

	for _, vec := range vectors {
		t.Run(vec.Name, func(t *testing.T) {
			var localRepoID string
			if vec.Context != nil {
				localRepoID = vec.Context.LocalRepoID
			}

			var registry []s.RepoEntry
			for _, r := range vec.Registry {
				registry = append(registry, s.RepoEntry{
					RepoID:      r.RepoID,
					Slug:        r.Slug,
					Remotes:     r.Remotes,
					IsWorkspace: r.IsWorkspace,
				})
			}

			resolved, err := s.ResolveReference(vec.Reference, localRepoID, registry)
			if err != nil {
				t.Fatalf("ResolveReference(%q) error: %v", vec.Reference, err)
			}

			if vec.Expected != nil {
				if resolved.IsResolved() != vec.Expected.Resolved {
					t.Errorf("IsResolved = %v, want %v", resolved.IsResolved(), vec.Expected.Resolved)
				}
				if resolved.Scope != vec.Expected.Scope {
					t.Errorf("Scope = %q, want %q", resolved.Scope, vec.Expected.Scope)
				}
				if vec.Expected.RepoID != "" && resolved.RepoID != vec.Expected.RepoID {
					t.Errorf("RepoID = %q, want %q", resolved.RepoID, vec.Expected.RepoID)
				}
				if resolved.ObjectID != vec.Expected.ObjectID {
					t.Errorf("ObjectID = %q, want %q", resolved.ObjectID, vec.Expected.ObjectID)
				}
			}
		})
	}
}

func TestInvalidReferenceGrammarCases(t *testing.T) {
	invalidCases := []struct {
		name string
		ref  string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"leading whitespace", " 0123456789abcdef0123456789abcdef"},
		{"trailing whitespace", "0123456789abcdef0123456789abcdef "},
		{"embedded whitespace", "0123456789abcdef 0123456789abcdef"},
		{"newline in reference", "0123456789abcdef\n0123456789abcdef"},
		{"tab in reference", "0123456789abcdef\t0123456789abcdef"},
		{"two hashes", "a1b2c3d4e5f60718293a4b5c6d7e8f90#0123456789abcdef0123456789abcdef#extra"},
		{"empty designator", "#0123456789abcdef0123456789abcdef"},
		{"empty object id", "a1b2c3d4e5f60718293a4b5c6d7e8f90#"},
		{"short repo id", "a1b2c3d4e5f6#0123456789abcdef0123456789abcdef"},
		{"uppercase repo id", "A1B2C3D4E5F60718293A4B5C6D7E8F90#0123456789abcdef0123456789abcdef"},
		{"non-hex repo id", "g1h2i3j4k5l6m7n8o9p0q1r2s3t4u5v6#0123456789abcdef0123456789abcdef"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := s.ParseReference(tc.ref)
			if err == nil {
				t.Errorf("ParseReference(%q) expected error, got nil", tc.ref)
			}
			_, errRes := s.ResolveReference(tc.ref, "", nil)
			if errRes == nil {
				t.Errorf("ResolveReference(%q) expected error, got nil", tc.ref)
			}
		})
	}
}

func TestResolveReferenceUnresolvedPreservation(t *testing.T) {
	ref := "99999999999999999999999999999999#0123456789abcdef0123456789abcdef"
	localRepoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	registry := []s.RepoEntry{
		{
			RepoID:  localRepoID,
			Slug:    "acme/backend",
			Remotes: []string{"git@github.com:acme/backend.git"},
		},
	}

	res, err := s.ResolveReference(ref, localRepoID, registry)
	if err != nil {
		t.Fatalf("ResolveReference failed: %v", err)
	}

	if res.IsResolved() {
		t.Errorf("expected unresolved, got resolved")
	}
	if res.Scope != "unresolved" {
		t.Errorf("scope = %q, want 'unresolved'", res.Scope)
	}
	if res.Reference != ref {
		t.Errorf("reference = %q, want %q", res.Reference, ref)
	}
	if res.Designator != "99999999999999999999999999999999" {
		t.Errorf("designator = %q, want '99999999999999999999999999999999'", res.Designator)
	}
	if res.ObjectID != "0123456789abcdef0123456789abcdef" {
		t.Errorf("object_id = %q, want '0123456789abcdef0123456789abcdef'", res.ObjectID)
	}
	if res.Reason != "unknown_repo" {
		t.Errorf("reason = %q, want 'unknown_repo'", res.Reason)
	}
}

func TestResolveReferenceSameRepoShortCircuit(t *testing.T) {
	localRepoID := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	objID := "0123456789abcdef0123456789abcdef"

	// Bare reference
	res1, err := s.ResolveReference(objID, localRepoID, nil)
	if err != nil {
		t.Fatalf("ResolveReference bare failed: %v", err)
	}
	if !res1.IsResolved() || res1.Scope != "local" || res1.RepoID != localRepoID || res1.ObjectID != objID {
		t.Errorf("bare resolution mismatch: %+v", res1)
	}

	// Qualified reference matching localRepoID
	qualRef := localRepoID + "#" + objID
	res2, err := s.ResolveReference(qualRef, localRepoID, nil)
	if err != nil {
		t.Fatalf("ResolveReference qual matching failed: %v", err)
	}
	if !res2.IsResolved() || res2.Scope != "local" || res2.RepoID != localRepoID || res2.ObjectID != objID {
		t.Errorf("qual matching resolution mismatch: %+v", res2)
	}
}
