package writ

import (
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/state"
)

// Rule specifies the merge strategy and parameters for an (op_type, op_version, field) tuple.
type Rule = state.Rule

// NormalizeRule specifies normalization attributes for an (op_type, field) merge rule.
type NormalizeRule = state.NormalizeRule

// Sentinels re-exported from internal/fold.
var (
	// ErrCycle is returned when the operation graph contains a directed cycle.
	ErrCycle = state.ErrCycle

	// ErrDuplicateOpID is returned when the input set contains duplicate operation IDs.
	ErrDuplicateOpID = state.ErrDuplicateOpID

	// ErrMixedObjects is returned when the input set spans multiple object IDs.
	ErrMixedObjects = state.ErrMixedObjects
)

// Fold executes deterministic fold reduction on an input set of operations
// against declared field merge rules, returning the resulting ObjectState.
func Fold(ops []codec.Op, rules []Rule) (ObjectState, error) {
	return state.Fold(ops, rules)
}

// FoldReview executes deterministic fold reduction on an input set of operations
// for a code review collaborative object, returning the materialized Review state.
func FoldReview(ops []codec.Op) (Review, error) {
	return state.FoldReview(ops)
}

// FoldComment reduces operations for a single comment object into a typed Comment.
// Subject and anchor payloads are preserved byte-identically from the winning create op.
func FoldComment(ops []codec.Op) (Comment, error) {
	return state.FoldComment(ops)
}

// FoldComments groups operations across multiple comment objects, folds each comment,
// and constructs the reply forest as a slice of CommentThread roots.
func FoldComments(ops []codec.Op) ([]CommentThread, error) {
	return state.FoldComments(ops)
}

// FoldIssue executes deterministic fold reduction on an input set of operations
// for an issue collaborative object, returning the materialized Issue state.
func FoldIssue(ops []codec.Op) (Issue, error) {
	return state.FoldIssue(ops)
}

// FoldProject executes deterministic fold reduction on an input set of operations
// for a project collaborative object, returning the materialized Project state.
func FoldProject(ops []codec.Op) (Project, error) {
	return state.FoldProject(ops)
}

// FoldCycle executes deterministic fold reduction on an input set of operations
// for a cycle collaborative object, returning the materialized Cycle state.
func FoldCycle(ops []codec.Op) (Cycle, error) {
	return state.FoldCycle(ops)
}

// ReviewRules returns the built-in field merge rules for the review-ops vocabulary (v1).
func ReviewRules() []Rule {
	return state.ReviewRules()
}

// IssueRules returns the built-in field merge rules for the issue-ops vocabulary (v1).
func IssueRules() []Rule {
	return state.IssueRules()
}

// ProjectRules returns the built-in field merge rules for the project vocabulary (v1).
func ProjectRules() []Rule {
	return state.ProjectRules()
}

// CycleRules returns the built-in field merge rules for the cycle vocabulary (v1).
func CycleRules() []Rule {
	return state.CycleRules()
}
