package writ

import (
	"context"
	"fmt"
	"time"

	"github.com/writtendev/writ/engine/projection"
	"github.com/writtendev/writ/engine/resolve"
)

// Draft represents an unpublished local comment draft.
type Draft struct {
	ID          string          `json:"id"`
	SubjectType string          `json:"subject_type"`
	SubjectID   string          `json:"subject_id"`
	InReplyTo   string          `json:"in_reply_to,omitempty"`
	Anchor      *resolve.Anchor `json:"anchor,omitempty"`
	Text        string          `json:"text"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// DraftFilter specifies filtering criteria when querying drafts.
type DraftFilter struct {
	SubjectID   string `json:"subject_id,omitempty"`
	SubjectType string `json:"subject_type,omitempty"`
}

// Drafts provides operations on local-only comment drafts.
type Drafts struct {
	store *Store
}

// Save creates or updates a comment draft. If draft.ID is empty, a new draft ID is minted.
func (d *Drafts) Save(ctx context.Context, draft Draft) (string, error) {
	if d == nil || d.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	if draft.Text == "" {
		return "", fmt.Errorf("writ: draft text cannot be empty")
	}
	if draft.SubjectID == "" {
		return "", fmt.Errorf("writ: draft subject_id cannot be empty")
	}

	pDraft := projection.Draft{
		DraftID:     draft.ID,
		SubjectType: draft.SubjectType,
		SubjectID:   draft.SubjectID,
		InReplyTo:   draft.InReplyTo,
		Anchor:      draft.Anchor,
		Text:        draft.Text,
		CreatedAt:   draft.CreatedAt,
		UpdatedAt:   draft.UpdatedAt,
	}

	id, err := d.store.projection.SaveDraft(pDraft)
	if err != nil {
		return "", fmt.Errorf("writ: save draft: %w", err)
	}
	return id, nil
}

// Get retrieves a comment draft by its draft ID.
func (d *Drafts) Get(ctx context.Context, id string) (Draft, error) {
	if d == nil || d.store == nil {
		return Draft{}, fmt.Errorf("writ: store is nil")
	}
	if id == "" {
		return Draft{}, fmt.Errorf("writ: draft id cannot be empty")
	}

	pd, err := d.store.projection.Draft(id)
	if err != nil {
		return Draft{}, err
	}

	return Draft{
		ID:          pd.DraftID,
		SubjectType: pd.SubjectType,
		SubjectID:   pd.SubjectID,
		InReplyTo:   pd.InReplyTo,
		Anchor:      pd.Anchor,
		Text:        pd.Text,
		CreatedAt:   pd.CreatedAt,
		UpdatedAt:   pd.UpdatedAt,
	}, nil
}

// List returns all comment drafts matching the specified filter.
func (d *Drafts) List(ctx context.Context, filter DraftFilter) ([]Draft, error) {
	if d == nil || d.store == nil {
		return nil, fmt.Errorf("writ: store is nil")
	}

	pList, err := d.store.projection.ListDrafts(projection.DraftFilter{
		SubjectID:   filter.SubjectID,
		SubjectType: filter.SubjectType,
	})
	if err != nil {
		return nil, fmt.Errorf("writ: list drafts: %w", err)
	}

	drafts := make([]Draft, len(pList))
	for i, pd := range pList {
		drafts[i] = Draft{
			ID:          pd.DraftID,
			SubjectType: pd.SubjectType,
			SubjectID:   pd.SubjectID,
			InReplyTo:   pd.InReplyTo,
			Anchor:      pd.Anchor,
			Text:        pd.Text,
			CreatedAt:   pd.CreatedAt,
			UpdatedAt:   pd.UpdatedAt,
		}
	}
	return drafts, nil
}

// Discard deletes a comment draft by its draft ID.
func (d *Drafts) Discard(ctx context.Context, id string) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if id == "" {
		return fmt.Errorf("writ: draft id cannot be empty")
	}

	if err := d.store.projection.DeleteDraft(id); err != nil {
		return err
	}
	return nil
}

// Publish converts a local draft into a committed comment operation on its subject,
// deleting the draft upon success.
func (d *Drafts) Publish(ctx context.Context, id string) (string, error) {
	if d == nil || d.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}

	draft, err := d.Get(ctx, id)
	if err != nil {
		return "", err
	}

	var commentID string
	switch draft.SubjectType {
	case "review", "":
		cid, err := d.store.Reviews.Comment(ctx, draft.SubjectID, NewComment{
			Text:      draft.Text,
			Anchor:    draft.Anchor,
			InReplyTo: draft.InReplyTo,
		})
		if err != nil {
			if draft.SubjectType == "" {
				cidIssue, errIssue := d.store.Issues.Comment(ctx, draft.SubjectID, NewComment{
					Text:      draft.Text,
					Anchor:    draft.Anchor,
					InReplyTo: draft.InReplyTo,
				})
				if errIssue == nil {
					commentID = cidIssue
					break
				}
			}
			return "", err
		}
		commentID = cid
	case "issue":
		cid, err := d.store.Issues.Comment(ctx, draft.SubjectID, NewComment{
			Text:      draft.Text,
			Anchor:    draft.Anchor,
			InReplyTo: draft.InReplyTo,
		})
		if err != nil {
			return "", err
		}
		commentID = cid
	default:
		return "", fmt.Errorf("writ: unsupported draft subject type %q", draft.SubjectType)
	}

	// Delete draft after successful comment operation
	_ = d.Discard(ctx, id)

	return commentID, nil
}
