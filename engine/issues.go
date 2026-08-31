package writ

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/writtendev/writ/engine/codec"
)

// Issues provides operations on issue collaborative objects.
type Issues struct {
	store *Store
}

// NewIssue specifies parameters for creating an issue.
type NewIssue struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// IssueEdit specifies metadata edits for an issue.
type IssueEdit struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

// IssueState specifies state transitions for an issue.
type IssueState struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// Create initializes a new issue collaborative object, minting an object ID.
func (i *Issues) Create(ctx context.Context, n NewIssue) (string, error) {
	if i == nil || i.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return "", err
	}
	if n.Title == "" {
		return "", fmt.Errorf("writ: issue title cannot be empty")
	}

	id := newObjectID()

	body := map[string]any{
		"title": n.Title,
	}
	if n.Description != "" {
		body["description"] = n.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal issue create body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, nil); err != nil {
		return "", fmt.Errorf("writ: create issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return id, nil
}

// Update modifies title or description metadata for an existing issue.
func (i *Issues) Update(ctx context.Context, id string, edit IssueEdit) error {
	if i == nil || i.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: issue id cannot be empty")
	}
	if edit.Title == nil && edit.Description == nil {
		return fmt.Errorf("writ: at least one of title or description must be provided")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return err
	}

	frontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := make(map[string]any)
	if edit.Title != nil {
		body["title"] = *edit.Title
	}
	if edit.Description != nil {
		body["description"] = *edit.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal update body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "update",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: update issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return nil
}

// SetState transitions the issue state (e.g. "open", "closed") with an optional reason.
func (i *Issues) SetState(ctx context.Context, id string, state IssueState) error {
	if i == nil || i.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" || state.State == "" {
		return fmt.Errorf("writ: issue id and state must be non-empty")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return err
	}

	frontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := map[string]any{
		"state": state.State,
	}
	if state.Reason != "" {
		body["reason"] = state.Reason
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal set-state body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "set-state",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: set issue state: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return nil
}

// Assign adds and/or removes assignees on an issue.
func (i *Issues) Assign(ctx context.Context, id string, add, remove []string) error {
	if i == nil || i.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: issue id cannot be empty")
	}
	if len(add) == 0 && len(remove) == 0 {
		return fmt.Errorf("writ: add or remove must be non-empty")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return err
	}

	frontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := make(map[string]any)
	if len(add) > 0 {
		body["add"] = add
	}
	if len(remove) > 0 {
		body["remove"] = remove
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal assign body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "assign",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: assign issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return nil
}

// Label adds and/or removes labels on an issue.
func (i *Issues) Label(ctx context.Context, id string, add, remove []string) error {
	if i == nil || i.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: issue id cannot be empty")
	}
	if len(add) == 0 && len(remove) == 0 {
		return fmt.Errorf("writ: add or remove must be non-empty")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return err
	}

	frontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := make(map[string]any)
	if len(add) > 0 {
		body["add"] = add
	}
	if len(remove) > 0 {
		body["remove"] = remove
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal label body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "label",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: label issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return nil
}

// Link creates or modifies a cross-reference link on an issue.
func (i *Issues) Link(ctx context.Context, id string, l Link) error {
	if i == nil || i.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" || l.Target == "" || l.Relation == "" {
		return fmt.Errorf("writ: issue id, target, and relation must be non-empty")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return err
	}

	frontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := map[string]any{
		"target":   l.Target,
		"relation": l.Relation,
	}
	if l.TargetType != "" {
		body["target_type"] = l.TargetType
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal link body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "issue",
		OpType:     "link",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: link issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return nil
}

// Comment appends a new comment collaborative object attached to the issue.
func (i *Issues) Comment(ctx context.Context, id string, c NewComment) (string, error) {
	if i == nil || i.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	if err := i.store.ensureWritable(); err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("writ: issue id cannot be empty")
	}
	if c.Text == "" {
		return "", fmt.Errorf("writ: comment text cannot be empty")
	}

	if err := i.store.maybeAutoRefresh(ctx); err != nil {
		return "", fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := i.store.projection.Issue(id); err != nil {
		return "", err
	}

	issueFrontier, err := i.store.projection.Frontier(id)
	if err != nil {
		return "", fmt.Errorf("writ: get issue frontier: %w", err)
	}

	causalParents := make([]string, len(issueFrontier))
	copy(causalParents, issueFrontier)

	if c.InReplyTo != "" {
		replyFrontier, err := i.store.projection.Frontier(c.InReplyTo)
		if err != nil {
			return "", fmt.Errorf("writ: get reply frontier: %w", err)
		}
		if len(replyFrontier) == 0 {
			return "", fmt.Errorf("writ: in_reply_to comment %s not found: %w", c.InReplyTo, ErrNotFound)
		}
		causalParents = append(causalParents, replyFrontier...)
	}
	causalParents = dedupeAndSort(causalParents)

	commentID := newObjectID()

	body := map[string]any{
		"subject": CommentSubject{
			ObjectType: "issue",
			ObjectID:   id,
		},
		"text": c.Text,
	}
	if c.InReplyTo != "" {
		body["in_reply_to"] = c.InReplyTo
	}
	if c.Anchor != nil {
		body["anchor"] = c.Anchor
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal comment body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   commentID,
		ObjectType: "comment",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := i.store.dagStore.Append(ctx, env, causalParents); err != nil {
		return "", fmt.Errorf("writ: comment on issue: %w", err)
	}

	_ = i.store.maybeAutoRefresh(ctx)
	return commentID, nil
}
