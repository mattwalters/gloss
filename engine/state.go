package writ

import (
	"github.com/writtendev/writ/engine/resolve"
)

// Anchor is a content-based comment position object (v1).
// Fold carries anchors verbatim as data per spec/fold.md §6.
type Anchor = resolve.Anchor

// OpRef identifies an operation in an object's total order sequence L
// along with its causality-monotone effective timestamp t*.
type OpRef struct {
	Commit string `json:"commit"`
	TStar  int64  `json:"t_star"`
}

// UnknownOp records an operation that was preserved in the DAG and participated
// in ordering and ancestry, but whose (op_type, op_version) had no declared rules
// per spec/fold.md §7.
type UnknownOp struct {
	Commit    string `json:"commit"`
	OpType    string `json:"op_type"`
	OpVersion int64  `json:"op_version"`
}

// ObjectState is the folded state produced by the fold driver for a collaborative object.
type ObjectState struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type,omitempty"`
	TotalOrder []OpRef        `json:"total_order"`
	State      map[string]any `json:"state"`
	UnknownOps []UnknownOp    `json:"unknown_ops,omitempty"`
}

// Review represents the materialized state of a code review collaborative object (v1).
// Reducer implementation is in WRIT-26.
type Review struct {
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status,omitempty"`
	MergeCommit string     `json:"merge_commit,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	Revisions   []Revision `json:"revisions,omitempty"`
	Approvals   []Approval `json:"approvals,omitempty"`
	CIStatuses  []CIStatus `json:"ci_statuses,omitempty"`
}

// Revision represents a code revision push (base and head commits) on a review.
type Revision struct {
	Base string `json:"base"`
	Head string `json:"head"`
}

// Approval represents a review verdict vote for a specific revision head.
type Approval struct {
	Subject  string `json:"subject"`
	Revision string `json:"revision"`
	Verdict  string `json:"verdict"`
	Message  string `json:"message,omitempty"`
}

// CIStatus represents an automated check result attached to a revision head.
type CIStatus struct {
	Revision    string `json:"revision"`
	Name        string `json:"name"`
	State       string `json:"state"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
}

// Comment represents the materialized state of a comment collaborative object (v1).
// Reducer implementation is in WRIT-27.
type Comment struct {
	Subject   CommentSubject `json:"subject"`
	Text      string         `json:"text"`
	InReplyTo string         `json:"in_reply_to,omitempty"`
	Anchor    *Anchor        `json:"anchor,omitempty"`
	Deleted   bool           `json:"deleted,omitempty"`
}

// CommentSubject identifies the collaborative object a comment is attached to.
type CommentSubject struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}

// Issue represents the materialized state of an issue collaborative object.
// Reducer implementation will be defined alongside the issue op vocabulary.
type Issue struct {
	Title    string `json:"title,omitempty"`
	State    string `json:"state,omitempty"`
	Assignee string `json:"assignee,omitempty"`
}
