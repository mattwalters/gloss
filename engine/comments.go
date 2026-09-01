package writ

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/writtendev/writ/engine/codec"
)

// Comments provides operations on comment collaborative objects.
type Comments struct {
	store *Store
}

// Edit updates the text content of an existing comment.
func (c *Comments) Edit(ctx context.Context, id, text string) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := c.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: comment id cannot be empty")
	}
	if text == "" {
		return fmt.Errorf("writ: comment text cannot be empty")
	}

	if err := c.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	frontier, err := c.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}
	if len(frontier) == 0 {
		return ErrNotFound
	}

	bodyBytes, err := json.Marshal(map[string]string{
		"text": text,
	})
	if err != nil {
		return fmt.Errorf("writ: marshal comment edit body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "comment",
		OpType:     "edit",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := c.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: edit comment: %w", err)
	}

	_ = c.store.maybeAutoRefresh(ctx)
	return nil
}

// Delete marks a comment as deleted (tombstone).
func (c *Comments) Delete(ctx context.Context, id string) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := c.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: comment id cannot be empty")
	}

	if err := c.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	frontier, err := c.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}
	if len(frontier) == 0 {
		return ErrNotFound
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "comment",
		OpType:     "delete",
		OpVersion:  1,
		Body:       []byte("{}"),
	}

	if _, err := c.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: delete comment: %w", err)
	}

	_ = c.store.maybeAutoRefresh(ctx)
	return nil
}

// CommentResolve specifies parameters for resolving or unresolving a comment thread.
type CommentResolve struct {
	Resolved   bool   `json:"resolved"`
	ResolvedBy string `json:"resolved_by,omitempty"`
}

// Resolve updates the resolution status of a comment (or thread root).
// When res.Resolved is true, the comment is marked as resolved; when false, it is unresolved.
func (c *Comments) Resolve(ctx context.Context, id string, res CommentResolve) error {
	if c == nil || c.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	if err := c.store.ensureWritable(); err != nil {
		return err
	}
	if id == "" {
		return fmt.Errorf("writ: comment id cannot be empty")
	}

	if err := c.store.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	frontier, err := c.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}
	if len(frontier) == 0 {
		return ErrNotFound
	}

	body := map[string]any{
		"resolved": res.Resolved,
	}
	resolvedBy, err := normalizePersonBounded("resolved_by", res.ResolvedBy)
	if err != nil {
		return err
	}
	if resolvedBy != "" {
		body["resolved_by"] = resolvedBy
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal comment resolve body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "comment",
		OpType:     "resolve",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := c.store.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: resolve comment: %w", err)
	}

	_ = c.store.maybeAutoRefresh(ctx)
	return nil
}
