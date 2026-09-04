package writ

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/order"
	"github.com/writtendev/writ/spec"
)

// NewWorkflowState specifies fields for creating a new workflow state.
type NewWorkflowState struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Position    string `json:"position,omitempty"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// WorkflowStateEdit specifies fields for updating a workflow state.
type WorkflowStateEdit struct {
	Name        *string `json:"name,omitempty"`
	Type        *string `json:"type,omitempty"`
	Position    *string `json:"position,omitempty"`
	Color       *string `json:"color,omitempty"`
	Description *string `json:"description,omitempty"`
}

// WorkflowStates provides workflow state creation, updates, and default seeding.
type WorkflowStates struct {
	store *Store
}

// Create initializes a new workflow state collaborative object, minting an object ID.
func (ws *WorkflowStates) Create(ctx context.Context, nws NewWorkflowState) (string, error) {
	if ws == nil || ws.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	target := ws.store
	if err := target.ensureWritable(); err != nil {
		return "", err
	}

	if nws.Name == "" {
		return "", fmt.Errorf("writ: workflow state name cannot be empty")
	}
	if !slices.Contains(spec.WorkflowStateTypes(), nws.Type) {
		return "", fmt.Errorf("writ: invalid workflow state type %q (must be %s)", nws.Type, spec.FormatOptions(spec.WorkflowStateTypes()))
	}

	pos := nws.Position
	if pos == "" {
		if err := target.maybeAutoRefresh(ctx); err != nil {
			return "", fmt.Errorf("writ: auto refresh: %w", err)
		}
		existing, err := target.projection.WorkflowStates(WorkflowStateFilter{})
		if err != nil {
			return "", fmt.Errorf("writ: query workflow states for position: %w", err)
		}
		lastPos := ""
		if len(existing) > 0 {
			lastPos = existing[len(existing)-1].WorkflowState.Position
		}
		var bErr error
		pos, bErr = order.Between(lastPos, "")
		if bErr != nil {
			return "", fmt.Errorf("writ: generate position: %w", bErr)
		}
	} else {
		if err := order.Validate(pos); err != nil {
			return "", fmt.Errorf("writ: invalid position %q: %w", pos, err)
		}
	}

	id := newObjectID()

	body := map[string]any{
		"name":     nws.Name,
		"type":     nws.Type,
		"position": pos,
	}
	if nws.Color != "" {
		body["color"] = nws.Color
	}
	if nws.Description != "" {
		body["description"] = nws.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal workflow state body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "workflow-state",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, nil); err != nil {
		return "", fmt.Errorf("writ: append workflow state: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return id, nil
}

// Update modifies one or more properties of an existing workflow state.
func (ws *WorkflowStates) Update(ctx context.Context, id string, edit WorkflowStateEdit) error {
	if ws == nil || ws.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := ws.store
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: workflow state id cannot be empty")
	}

	if edit.Name == nil && edit.Type == nil && edit.Position == nil && edit.Color == nil && edit.Description == nil {
		return fmt.Errorf("writ: at least one property must be specified to update workflow state")
	}

	if edit.Name != nil && *edit.Name == "" {
		return fmt.Errorf("writ: workflow state name cannot be empty")
	}
	if edit.Type != nil && !slices.Contains(spec.WorkflowStateTypes(), *edit.Type) {
		return fmt.Errorf("writ: invalid workflow state type %q (must be %s)", *edit.Type, spec.FormatOptions(spec.WorkflowStateTypes()))
	}
	if edit.Position != nil {
		if err := order.Validate(*edit.Position); err != nil {
			return fmt.Errorf("writ: invalid position %q: %w", *edit.Position, err)
		}
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.WorkflowState(id); err != nil {
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
	if edit.Type != nil {
		body["type"] = *edit.Type
	}
	if edit.Position != nil {
		body["position"] = *edit.Position
	}
	if edit.Color != nil {
		body["color"] = *edit.Color
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
		ObjectType: "workflow-state",
		OpType:     "update",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: update workflow state: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// DefaultWorkflowStates returns the canonical 5 default starter workflow states.
var DefaultWorkflowStates = []NewWorkflowState{
	{Name: "Backlog", Type: "backlog", Position: "1"},
	{Name: "Todo", Type: "unstarted", Position: "V"},
	{Name: "In Progress", Type: "started", Position: "k"},
	{Name: "Done", Type: "completed", Position: "s"},
	{Name: "Canceled", Type: "canceled", Position: "zV"},
}

// SeedDefaults creates the five default starter workflow states if no workflow states exist.
func (ws *WorkflowStates) SeedDefaults(ctx context.Context) error {
	if ws == nil || ws.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := ws.store
	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	existing, err := target.projection.WorkflowStates(WorkflowStateFilter{})
	if err != nil {
		return fmt.Errorf("writ: query existing workflow states: %w", err)
	}
	if len(existing) > 0 {
		return nil
	}

	for _, def := range DefaultWorkflowStates {
		if _, err := ws.Create(ctx, def); err != nil {
			return fmt.Errorf("writ: seed workflow state %s: %w", def.Name, err)
		}
	}
	return nil
}
