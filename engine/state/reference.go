package state

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var repoDesignatorRegexp = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ErrInvalidReference is returned when a reference string violates the reference grammar.
var ErrInvalidReference = errors.New("writ: invalid reference format")

// ResolvedReference represents the outcome of resolving a reference against a repository registry.
type ResolvedReference struct {
	Scope      string   `json:"scope"` // "local", "cross-repo", or "unresolved"
	RepoID     string   `json:"repo_id,omitempty"`
	Slug       string   `json:"slug,omitempty"`
	Remotes    []string `json:"remotes,omitempty"`
	ObjectID   string   `json:"object_id"`
	Reference  string   `json:"reference,omitempty"`
	Designator string   `json:"designator,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// IsResolved reports whether the reference was successfully resolved (local or cross-repo).
func (r ResolvedReference) IsResolved() bool {
	return r.Scope == "local" || r.Scope == "cross-repo"
}

// ParseReference parses a reference string into its repository designator (empty for bare local references)
// and target object ID, validating syntax against spec/identifiers.md §Reference grammar.
func ParseReference(ref string) (designator string, objectID string, err error) {
	if ref == "" {
		return "", "", fmt.Errorf("%w: reference cannot be empty", ErrInvalidReference)
	}

	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", "", fmt.Errorf("%w: reference cannot contain whitespace or control characters", ErrInvalidReference)
		}
	}

	parts := strings.Split(ref, "#")
	switch len(parts) {
	case 1:
		// Bare reference
		if parts[0] == "" {
			return "", "", fmt.Errorf("%w: object id cannot be empty", ErrInvalidReference)
		}
		return "", parts[0], nil

	case 2:
		// Fully-qualified reference: <repo-id>#<object-id>
		des := parts[0]
		objID := parts[1]

		if des == "" {
			return "", "", fmt.Errorf("%w: repo designator cannot be empty when '#' is present", ErrInvalidReference)
		}
		if !repoDesignatorRegexp.MatchString(des) {
			return "", "", fmt.Errorf("%w: repo designator must be 32 lowercase hex characters", ErrInvalidReference)
		}
		if objID == "" {
			return "", "", fmt.Errorf("%w: object id cannot be empty", ErrInvalidReference)
		}

		return des, objID, nil

	default:
		return "", "", fmt.Errorf("%w: reference cannot contain multiple '#' separators", ErrInvalidReference)
	}
}

// ResolveReference executes the reference resolution algorithm from spec/identifiers.md §Reference resolution:
//
// 1. Parse reference into designator and target_object_id.
// 2. Same-repo short circuit: if designator == "" or designator == localRepoID, return local scope.
// 3. Cross-repo registry lookup: search registry for entry.repo_id == designator. If found, return cross-repo scope.
// 4. If not found or registry unavailable, return unresolved scope preserving original reference.
func ResolveReference(ref string, localRepoID string, registry []RepoEntry) (ResolvedReference, error) {
	designator, objectID, err := ParseReference(ref)
	if err != nil {
		return ResolvedReference{}, err
	}

	// Same-repo short-circuit
	if designator == "" || (localRepoID != "" && designator == localRepoID) {
		targetRepoID := localRepoID
		if targetRepoID == "" && designator != "" {
			targetRepoID = designator
		}
		return ResolvedReference{
			Scope:    "local",
			RepoID:   targetRepoID,
			ObjectID: objectID,
		}, nil
	}

	// Cross-repo registry lookup
	for _, entry := range registry {
		if entry.RepoID == designator {
			return ResolvedReference{
				Scope:    "cross-repo",
				RepoID:   entry.RepoID,
				Slug:     entry.Slug,
				Remotes:  entry.Remotes,
				ObjectID: objectID,
			}, nil
		}
	}

	// Unresolved-but-preserved
	return ResolvedReference{
		Scope:      "unresolved",
		Reference:  ref,
		Designator: designator,
		ObjectID:   objectID,
		Reason:     "unknown_repo",
	}, nil
}
