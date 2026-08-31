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

	_ = c.store.maybeAutoRefresh(ctx)

	frontier, err := c.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
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

	_ = c.store.maybeAutoRefresh(ctx)

	frontier, err := c.store.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
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
