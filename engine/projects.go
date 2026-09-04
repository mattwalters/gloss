package writ

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/spec"
)

// NewProject specifies fields for creating a new project.
type NewProject struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// ProjectEdit specifies fields for updating a project. At least one field
// must be set.
type ProjectEdit struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

// Projects provides project creation, metadata updates, status transitions,
// and issue membership operations. Projects home in the repo the store was
// opened on; there is no routing to another repository.
type Projects struct {
	store *Store
}

// Create initializes a new project collaborative object, minting an object ID.
func (p *Projects) Create(ctx context.Context, np NewProject) (string, error) {
	if p == nil || p.store == nil {
		return "", fmt.Errorf("writ: store is nil")
	}
	target := p.store
	if err := target.ensureWritable(); err != nil {
		return "", err
	}

	if np.Title == "" {
		return "", fmt.Errorf("writ: project title cannot be empty")
	}

	id := newObjectID()

	body := map[string]any{
		"title": np.Title,
	}
	if np.Description != "" {
		body["description"] = np.Description
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal project body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "project",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, nil); err != nil {
		return "", fmt.Errorf("writ: append project: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return id, nil
}

// Update modifies title or description of an existing project. At least one
// of edit.Title or edit.Description must be set; both may be carried in the
// same op.
func (p *Projects) Update(ctx context.Context, id string, edit ProjectEdit) error {
	if p == nil || p.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := p.store
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: project id cannot be empty")
	}
	if edit.Title == nil && edit.Description == nil {
		return fmt.Errorf("writ: at least one property must be specified to update project")
	}
	if edit.Title != nil && *edit.Title == "" {
		return fmt.Errorf("writ: project title cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Project(id); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(id)
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
		return fmt.Errorf("writ: marshal project update body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "project",
		OpType:     "update",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append project update: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// SetStatus transitions the project lifecycle status ("planned", "active",
// "paused", "completed", or "canceled"). There is no Delete: v1 has no
// project tombstones (spec/project-cycle.md §6), and cancellation is
// SetStatus(ctx, id, "canceled", reason).
func (p *Projects) SetStatus(ctx context.Context, id, status, reason string) error {
	if p == nil || p.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := p.store
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: project id cannot be empty")
	}
	if status == "" {
		return fmt.Errorf("writ: project status cannot be empty")
	}
	if !slices.Contains(spec.ProjectStatuses(), status) {
		return fmt.Errorf("writ: invalid status %q (must be %s)", status, spec.FormatOptions(spec.ProjectStatuses()))
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Project(id); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := map[string]any{
		"status": status,
	}
	if reason != "" {
		body["reason"] = reason
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal project set-status body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "project",
		OpType:     "set-status",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append project set-status: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// AddIssue adds an issue to a project's member set. issueRef is canonicalized
// (bare for same-repo, qualified <repo-id>#<object-id> otherwise) before it
// reaches the op body, so a caller cannot alias an existing member by
// supplying an equivalent reference in the other form.
func (p *Projects) AddIssue(ctx context.Context, id, issueRef string) error {
	return p.editMembership(ctx, id, issueRef, "add-issue")
}

// RemoveIssue removes an issue from a project's member set. issueRef is
// canonicalized the same way AddIssue's is, so a remove targets the same
// member string an add would have written.
func (p *Projects) RemoveIssue(ctx context.Context, id, issueRef string) error {
	return p.editMembership(ctx, id, issueRef, "remove-issue")
}

func (p *Projects) editMembership(ctx context.Context, id, issueRef, opType string) error {
	if p == nil || p.store == nil {
		return fmt.Errorf("writ: store is nil")
	}
	target := p.store
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: project id cannot be empty")
	}
	if issueRef == "" {
		return fmt.Errorf("writ: issue reference cannot be empty")
	}

	canonical, err := canonicalizeReference(issueRef, target.localRepoID)
	if err != nil {
		return fmt.Errorf("writ: invalid issue reference %q: %w", issueRef, err)
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Project(id); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"issue": canonical})
	if err != nil {
		return fmt.Errorf("writ: marshal project %s body: %w", opType, err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "project",
		OpType:     opType,
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append project %s: %w", opType, err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}
