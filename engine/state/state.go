package state

import (
	"encoding/json"

	"github.com/writtendev/writ/engine/internal/person"
	"github.com/writtendev/writ/engine/resolve"
)

// NormalizePerson normalizes a person identifier string per spec/identifiers.md
// (trimmed leading/trailing whitespace, lowercase). The rule itself lives in
// engine/internal/person; this is the name the state package, its callers, and
// the projection use.
func NormalizePerson(s string) string {
	return person.NormalizePerson(s)
}

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
// per spec/fold.md §7 or whose body a declared rule found uninterpretable per §7.1.
type UnknownOp struct {
	Commit     string `json:"commit"`
	ObjectType string `json:"object_type"`
	OpType     string `json:"op_type"`
	OpVersion  int64  `json:"op_version"`
}

// ObjectState is the folded state produced by the fold driver for a collaborative object.
type ObjectState struct {
	ObjectID   string         `json:"object_id"`
	ObjectType string         `json:"object_type,omitempty"`
	TotalOrder []OpRef        `json:"total_order"`
	State      map[string]any `json:"state"`
	UnknownOps []UnknownOp    `json:"unknown_ops,omitempty"`
}

// Review represents the materialized state of a code review collaborative object (v1),
// produced by FoldReview.
type Review struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Status      string      `json:"status,omitempty"`
	MergeCommit string      `json:"merge_commit,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Assignees   []string    `json:"assignees,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Links       []Link      `json:"links,omitempty"`
	Revisions   []Revision  `json:"revisions,omitempty"`
	Approvals   []Approval  `json:"approvals,omitempty"`
	CIStatuses  []CIStatus  `json:"ci_statuses,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
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
type Comment struct {
	Subject    CommentSubject `json:"subject,omitzero"`
	Text       string         `json:"text,omitempty"`
	InReplyTo  string         `json:"in_reply_to,omitempty"`
	Anchor     *Anchor        `json:"anchor,omitempty"`
	Deleted    bool           `json:"deleted,omitempty"`
	Resolved   *bool          `json:"resolved,omitempty"`
	ResolvedBy string         `json:"resolved_by,omitempty"`
	UnknownOps []UnknownOp    `json:"unknown_ops,omitempty"`
}

// IsResolved reports whether the comment has been resolved.
func (c Comment) IsResolved() bool {
	return c.Resolved != nil && *c.Resolved
}

// CommentSubject identifies the collaborative object a comment is attached to.
type CommentSubject struct {
	ObjectType string                     `json:"object_type,omitempty"`
	ObjectID   string                     `json:"object_id,omitempty"`
	Unknown    map[string]json.RawMessage `json:"-"`
	Raw        []byte                     `json:"-"`
}

// MarshalJSON serializes CommentSubject. If Raw is populated, Raw is returned directly
// to preserve unknown fields and exact bytes.
func (s CommentSubject) MarshalJSON() ([]byte, error) {
	if len(s.Raw) > 0 {
		return s.Raw, nil
	}
	type Alias CommentSubject
	return json.Marshal((*Alias)(&s))
}

// IsZero reports whether the CommentSubject is empty.
func (s CommentSubject) IsZero() bool {
	return s.ObjectType == "" && s.ObjectID == "" && len(s.Unknown) == 0 && len(s.Raw) == 0
}

// ParseCommentSubject parses raw JSON bytes into a CommentSubject, retaining the original bytes
// in Raw and preserving unknown fields.
func ParseCommentSubject(raw []byte) (CommentSubject, error) {
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(raw, &topLevel); err != nil {
		return CommentSubject{}, err
	}

	var s CommentSubject
	s.Raw = raw

	if v, ok := topLevel["object_type"]; ok {
		if err := json.Unmarshal(v, &s.ObjectType); err != nil {
			return CommentSubject{}, err
		}
		delete(topLevel, "object_type")
	}
	if v, ok := topLevel["object_id"]; ok {
		if err := json.Unmarshal(v, &s.ObjectID); err != nil {
			return CommentSubject{}, err
		}
		delete(topLevel, "object_id")
	}

	if len(topLevel) > 0 {
		s.Unknown = topLevel
	}

	return s, nil
}

// Issue represents the materialized state of an issue collaborative object (v1),
// produced by FoldIssue.
type Issue struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	State       string      `json:"state,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Assignees   []string    `json:"assignees,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Links       []Link      `json:"links,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
}

// Link represents a cross-reference link attached to an issue.
type Link struct {
	Target     string `json:"target"`
	TargetType string `json:"target_type,omitempty"`
	Relation   string `json:"relation"`
}

// Project represents the materialized state of a project collaborative object (v1),
// produced by FoldProject.
type Project struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Status      string      `json:"status,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Issues      []string    `json:"issues,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
}

// Cycle represents the materialized state of a cycle collaborative object (v1),
// produced by FoldCycle.
type Cycle struct {
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	StartsAt    string      `json:"starts_at,omitempty"`
	EndsAt      string      `json:"ends_at,omitempty"`
	Issues      []string    `json:"issues,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
}

// Label represents the materialized state of a label collaborative object (v1),
// produced by FoldLabel.
type Label struct {
	Name        string      `json:"name,omitempty"`
	Color       string      `json:"color,omitempty"`
	Description string      `json:"description,omitempty"`
	UnknownOps  []UnknownOp `json:"unknown_ops,omitempty"`
}
