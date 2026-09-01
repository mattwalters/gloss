package state

import (
	"fmt"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/internal/fold"
	"github.com/writtendev/writ/engine/resolve"
)

// CommentThread represents a node in the comment reply forest.
type CommentThread struct {
	ObjectID   string          `json:"object_id"`
	Comment    Comment         `json:"comment"`
	Replies    []CommentThread `json:"replies,omitempty"`
	UnknownOps []UnknownOp     `json:"unknown_ops,omitempty"`
}

// FoldComment reduces operations for a single comment object into a typed Comment.
// Subject and anchor payloads are preserved byte-identically from the winning create op.
func FoldComment(ops []codec.Op) (Comment, error) {
	cf, err := fold.FoldComment(ops)
	if err != nil {
		return Comment{}, err
	}
	return convertCommentFold(cf)
}

// FoldComments groups operations across multiple comment objects, folds each comment,
// and constructs the reply forest as a slice of CommentThread roots.
func FoldComments(ops []codec.Op) ([]CommentThread, error) {
	nodes, err := fold.FoldComments(ops)
	if err != nil {
		return nil, err
	}

	var convertNode func(n fold.CommentNode) (CommentThread, error)
	convertNode = func(n fold.CommentNode) (CommentThread, error) {
		c, err := convertCommentFold(n.Comment)
		if err != nil {
			return CommentThread{}, err
		}
		replies := make([]CommentThread, 0, len(n.Replies))
		for _, r := range n.Replies {
			ct, err := convertNode(r)
			if err != nil {
				return CommentThread{}, err
			}
			replies = append(replies, ct)
		}
		return CommentThread{
			ObjectID:   n.ObjectID,
			Comment:    c,
			Replies:    replies,
			UnknownOps: c.UnknownOps,
		}, nil
	}

	threads := make([]CommentThread, 0, len(nodes))
	for _, n := range nodes {
		ct, err := convertNode(n)
		if err != nil {
			return nil, err
		}
		threads = append(threads, ct)
	}

	return threads, nil
}

func convertCommentFold(cf fold.CommentFold) (Comment, error) {
	var unknownOps []UnknownOp
	if len(cf.UnknownOps) > 0 {
		unknownOps = make([]UnknownOp, len(cf.UnknownOps))
		for i, u := range cf.UnknownOps {
			unknownOps[i] = UnknownOp{
				Commit:    u.Commit,
				OpType:    u.OpType,
				OpVersion: u.OpVersion,
			}
		}
	}

	// Actor is already normalized by the fold layer; normalizing again here is a
	// no-op kept for defence in depth, matching how the issue and review reducers
	// re-normalize their assignee and approval-subject person identifiers.
	c := Comment{
		Text:       cf.Text,
		InReplyTo:  cf.InReplyTo,
		Deleted:    cf.Deleted,
		Resolved:   cf.Resolved,
		Actor:      NormalizePerson(cf.Actor),
		UnknownOps: unknownOps,
	}

	if len(cf.SubjectRaw) > 0 {
		sub, err := ParseCommentSubject(cf.SubjectRaw)
		if err != nil {
			return Comment{}, fmt.Errorf("parsing comment subject: %w", err)
		}
		c.Subject = sub
	}

	if len(cf.AnchorRaw) > 0 {
		anc, err := resolve.ParseAnchor(cf.AnchorRaw)
		if err != nil {
			return Comment{}, fmt.Errorf("parsing comment anchor: %w", err)
		}
		c.Anchor = &anc
	}

	return c, nil
}
