package writ

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/writtendev/writ/engine/codec"
)

// NewLabel specifies fields for creating a new label.
type NewLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// LabelEdit specifies fields for updating a label.
type LabelEdit struct {
	Name        *string `json:"name,omitempty"`
	Color       *string `json:"color,omitempty"`
	Description *string `json:"description,omitempty"`
}

// Labels provides label creation and updates.
type Labels struct {
	store *Store
}

// Create initializes a new label collaborative object, minting an object ID.
func (l *Labels) Create(ctx context.Context, nl NewLabel) (string, error) {
	if l == nil || l.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	target := l.store
	if err := target.ensureWritable(); err != nil {
		return "", err
	}

	if nl.Name == "" {
		return "", fmt.Errorf("writ: label name cannot be empty")
	}

	id := newObjectID()

	body := map[string]any{
		"name": nl.Name,
	}
	if nl.Color != "" {
		body["color"] = nl.Color
	}
	if nl.Description != "" {
		body["description"] = nl.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal label body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "label",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, nil); err != nil {
		return "", fmt.Errorf("writ: append label: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return id, nil
}

// Update modifies one or more properties of an existing label.
func (l *Labels) Update(ctx context.Context, id string, edit LabelEdit) error {
	if l == nil || l.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := l.store
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: label id cannot be empty")
	}

	if edit.Name == nil && edit.Color == nil && edit.Description == nil {
		return fmt.Errorf("writ: at least one property must be specified to update label")
	}

	if edit.Name != nil && *edit.Name == "" {
		return fmt.Errorf("writ: label name cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Label(id); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := make(map[string]any)
	if edit.Name != nil {
		body["name"] = *edit.Name
	}
	if edit.Color != nil {
		body["color"] = *edit.Color
	}
	if edit.Description != nil {
		body["description"] = *edit.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal label update body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "label",
		OpType:     "update",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append label update: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}
