package writ

import (
	"regexp"
	"strings"

	"github.com/writtendev/writ/engine/state"
)

// canonicalObjectIDPattern matches a canonical 32-character hex object ID
// (spec/identifiers.md §Object identifiers), case-insensitively. Only a
// reference's object-id half in this shape is case-folded; a non-canonical
// object-id may be case-significant and must round-trip untouched.
var canonicalObjectIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{32}$`)

// canonicalizeReference normalizes a caller-supplied reference to the
// producer form spec/project-cycle.md §3 and spec/identifiers.md §Reference
// grammar (Rules 1-4) require: a lowercase designator, a lowercase object-id
// when it is canonical, bare form when the reference is local, and qualified
// form (<repo-id>#<object-id>) otherwise.
//
// Fold compares set members as exact strings and never resolves references
// (spec/fold.md §6), so a bare <object-id> and a qualified
// <local-repo-id>#<object-id> naming the same object fold as two distinct
// members unless every producer converges on one form. This function is that
// convergence point, not a resolver: it never looks the referenced object up
// and never consults a repository registry. Shared by every service that
// emits a membership reference (engine/projects.go; engine/cycles.go
// follows the identical rule and reuses this helper rather than restating
// it).
//
// localRepoID is the caller's own repo-id (Store.localRepoID), or "" when
// unset (writ.repoId is optional — identity.LoadRepoID). When it is unset, a
// caller-supplied qualified reference that happens to name this repo cannot
// be recognized as local and is emitted qualified; that is a consequence of
// the information available, not a bug to paper over here.
func canonicalizeReference(ref, localRepoID string) (string, error) {
	designator, objectID, err := state.ParseReference(ref)
	if err != nil {
		return "", err
	}

	designator = strings.ToLower(designator)
	if canonicalObjectIDPattern.MatchString(objectID) {
		objectID = strings.ToLower(objectID)
	}

	if designator == "" || designator == localRepoID {
		return objectID, nil
	}
	return designator + "#" + objectID, nil
}
