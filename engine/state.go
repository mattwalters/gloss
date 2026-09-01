package writ

import (
	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/engine/state"
)

// Anchor is a content-based comment position object (v1).
// Fold carries anchors verbatim as data per spec/fold.md §6.
type Anchor = resolve.Anchor

// OpRef identifies an operation in an object's total order sequence L
// along with its causality-monotone effective timestamp t*.
type OpRef = state.OpRef

// UnknownOp records an operation that was preserved in the DAG and participated
// in ordering and ancestry, but whose (op_type, op_version) had no declared rules
// per spec/fold.md §7.
type UnknownOp = state.UnknownOp

// ObjectState is the folded state produced by the fold driver for a collaborative object.
type ObjectState = state.ObjectState

// Review represents the materialized state of a code review collaborative object (v1),
// produced by FoldReview.
type Review = state.Review

// Revision represents a code revision push (base and head commits) on a review.
type Revision = state.Revision

// Approval represents a review verdict vote for a specific revision head.
type Approval = state.Approval

// CIStatus represents an automated check result attached to a revision head.
type CIStatus = state.CIStatus

// Comment represents the materialized state of a comment collaborative object (v1).
type Comment = state.Comment

// CommentSubject identifies the collaborative object a comment is attached to.
type CommentSubject = state.CommentSubject

// ParseCommentSubject parses raw JSON bytes into a CommentSubject, retaining the original bytes
// in Raw and preserving unknown fields.
func ParseCommentSubject(raw []byte) (CommentSubject, error) {
	return state.ParseCommentSubject(raw)
}

// CommentThread represents a node in the comment reply forest.
type CommentThread = state.CommentThread

// Issue represents the materialized state of an issue collaborative object (v1),
// produced by FoldIssue.
type Issue = state.Issue

// Link represents a cross-reference link attached to an issue.
type Link = state.Link

// Project represents the materialized state of a project collaborative object (v1),
// produced by FoldProject.
type Project = state.Project

// Cycle represents the materialized state of a cycle collaborative object (v1),
// produced by FoldCycle.
type Cycle = state.Cycle

// RepoEntry represents a repository registry entry collaborative object (v1),
// produced by FoldRepo.
type RepoEntry = state.RepoEntry

// ResolvedReference represents the outcome of resolving a reference against a repository registry.
type ResolvedReference = state.ResolvedReference

// ParseReference parses a reference string into its repository designator and target object ID.
func ParseReference(ref string) (string, string, error) {
	return state.ParseReference(ref)
}

// ResolveReference executes the pure reference resolution algorithm from spec/identifiers.md §Reference resolution.
func ResolveReference(ref string, localRepoID string, registry []RepoEntry) (ResolvedReference, error) {
	return state.ResolveReference(ref, localRepoID, registry)
}

// NormalizePerson normalizes a person identifier string per spec/identifiers.md
// (trimmed leading/trailing whitespace, lowercase).
func NormalizePerson(s string) string {
	return state.NormalizePerson(s)
}
