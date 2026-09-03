package writ

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/order"
)

// NewDocument specifies fields for creating a new document.
type NewDocument struct {
	Title  string   `json:"title"`
	Labels []string `json:"labels,omitempty"`
	Links  []Link   `json:"links,omitempty"`
}

// DocumentLabelEdit specifies additions and removals of labels on a document.
type DocumentLabelEdit struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// DocumentEdit specifies fields for updating a document.
type DocumentEdit struct {
	Title  *string            `json:"title,omitempty"`
	Labels *DocumentLabelEdit `json:"labels,omitempty"`
}

// NewSection specifies fields for adding a section to a document.
type NewSection struct {
	Title    string `json:"title,omitempty"`
	Body     string `json:"body"`
	Position string `json:"position,omitempty"`
	After    string `json:"after,omitempty"`
	Before   string `json:"before,omitempty"`
}

// SectionEdit specifies fields for updating a section.
type SectionEdit struct {
	Title    *string `json:"title,omitempty"`
	Position *string `json:"position,omitempty"`
}

// Documents provides document and section operations.
type Documents struct {
	store *Store
}

func (d *Documents) targetStore(ctx context.Context) (*Store, bool, error) {
	if d == nil || d.store == nil {
		return nil, false, fmt.Errorf("writ: store is nil")
	}
	if d.store.Workspace != nil && d.store.Workspace.IsConfigured() {
		wsStore, err := d.store.Workspace.getStore(ctx)
		if err != nil {
			return nil, false, err
		}
		return wsStore, true, nil
	}
	return d.store, false, nil
}

// Create initializes a new document collaborative object, minting an object ID.
func (d *Documents) Create(ctx context.Context, nd NewDocument) (string, error) {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return "", err
	}
	if err := target.ensureWritable(); err != nil {
		return "", err
	}

	if nd.Title == "" {
		return "", fmt.Errorf("writ: document title cannot be empty")
	}

	id := newObjectID()

	body := map[string]any{
		"title": nd.Title,
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal document body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "document",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	tip, err := target.dagStore.Append(ctx, env, nil)
	if err != nil {
		return "", fmt.Errorf("writ: append document: %w", err)
	}

	frontier := []string{tip.ID}

	for _, link := range nd.Links {
		if link.Target == "" || link.Relation == "" {
			continue
		}
		lBody := map[string]any{
			"target":   link.Target,
			"relation": link.Relation,
		}
		if link.TargetType != "" {
			lBody["target_type"] = link.TargetType
		}
		lBytes, err := json.Marshal(lBody)
		if err != nil {
			return "", fmt.Errorf("writ: marshal link body: %w", err)
		}
		linkEnv := codec.Envelope{
			ObjectID:   id,
			ObjectType: "document",
			OpType:     "link",
			OpVersion:  1,
			Body:       lBytes,
		}
		newTip, err := target.dagStore.Append(ctx, linkEnv, frontier)
		if err != nil {
			return "", fmt.Errorf("writ: append document link: %w", err)
		}
		frontier = []string{newTip.ID}
	}

	if len(nd.Labels) > 0 {
		lblBody := map[string]any{
			"add": nd.Labels,
		}
		lblBytes, err := json.Marshal(lblBody)
		if err != nil {
			return "", fmt.Errorf("writ: marshal label body: %w", err)
		}
		lblEnv := codec.Envelope{
			ObjectID:   id,
			ObjectType: "document",
			OpType:     "label",
			OpVersion:  1,
			Body:       lblBytes,
		}
		if _, err := target.dagStore.Append(ctx, lblEnv, frontier); err != nil {
			return "", fmt.Errorf("writ: append document labels: %w", err)
		}
	}

	_ = target.maybeAutoRefresh(ctx)
	return id, nil
}

// Update modifies title or labels of an existing document.
func (d *Documents) Update(ctx context.Context, id string, edit DocumentEdit) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: document id cannot be empty")
	}

	if edit.Title == nil && edit.Labels == nil {
		return fmt.Errorf("writ: at least one property must be specified to update document")
	}

	if edit.Title != nil && *edit.Title == "" {
		return fmt.Errorf("writ: document title cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Document(id); err != nil {
		return err
	}

	if edit.Title != nil {
		frontier, err := target.projection.Frontier(id)
		if err != nil {
			return fmt.Errorf("writ: get frontier: %w", err)
		}
		bodyBytes, err := json.Marshal(map[string]any{"title": *edit.Title})
		if err != nil {
			return fmt.Errorf("writ: marshal update body: %w", err)
		}
		env := codec.Envelope{
			ObjectID:   id,
			ObjectType: "document",
			OpType:     "update",
			OpVersion:  1,
			Body:       bodyBytes,
		}
		if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
			return fmt.Errorf("writ: append document update: %w", err)
		}
	}

	if edit.Labels != nil {
		frontier, err := target.projection.Frontier(id)
		if err != nil {
			return fmt.Errorf("writ: get frontier: %w", err)
		}
		body := make(map[string]any)
		if len(edit.Labels.Add) > 0 {
			body["add"] = edit.Labels.Add
		}
		if len(edit.Labels.Remove) > 0 {
			body["remove"] = edit.Labels.Remove
		}
		if len(body) > 0 {
			bodyBytes, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("writ: marshal label body: %w", err)
			}
			env := codec.Envelope{
				ObjectID:   id,
				ObjectType: "document",
				OpType:     "label",
				OpVersion:  1,
				Body:       bodyBytes,
			}
			if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
				return fmt.Errorf("writ: append document labels: %w", err)
			}
		}
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// Link adds or updates a cross-reference link on a document.
func (d *Documents) Link(ctx context.Context, id string, link Link) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if id == "" {
		return fmt.Errorf("writ: document id cannot be empty")
	}
	if link.Target == "" {
		return fmt.Errorf("writ: link target cannot be empty")
	}
	if link.Relation == "" {
		return fmt.Errorf("writ: link relation cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Document(id); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(id)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := map[string]any{
		"target":   link.Target,
		"relation": link.Relation,
	}
	if link.TargetType != "" {
		body["target_type"] = link.TargetType
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal link body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   id,
		ObjectType: "document",
		OpType:     "link",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append document link: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// AddSection creates a new section in a document.
func (d *Documents) AddSection(ctx context.Context, docID string, ns NewSection) (string, error) {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return "", err
	}
	if err := target.ensureWritable(); err != nil {
		return "", err
	}

	if docID == "" {
		return "", fmt.Errorf("writ: document id cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return "", fmt.Errorf("writ: auto refresh: %w", err)
	}

	docRes, err := target.projection.Document(docID)
	if err != nil {
		return "", err
	}

	var pos string
	if ns.Position != "" {
		if err := order.Validate(ns.Position); err != nil {
			return "", fmt.Errorf("writ: invalid position %q: %w", ns.Position, err)
		}
		pos = ns.Position
	} else if ns.After != "" || ns.Before != "" {
		var afterPos, beforePos string
		if ns.After != "" {
			found := false
			for _, s := range docRes.Sections {
				if s.ObjectID == ns.After {
					afterPos = s.Section.Position
					found = true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("writ: section %s not found in document", ns.After)
			}
		}
		if ns.Before != "" {
			found := false
			for _, s := range docRes.Sections {
				if s.ObjectID == ns.Before {
					beforePos = s.Section.Position
					found = true
					break
				}
			}
			if !found {
				return "", fmt.Errorf("writ: section %s not found in document", ns.Before)
			}
		}
		var bErr error
		pos, bErr = order.Between(afterPos, beforePos)
		if bErr != nil {
			return "", fmt.Errorf("writ: generate position: %w", bErr)
		}
	} else {
		lastPos := ""
		if len(docRes.Sections) > 0 {
			lastPos = docRes.Sections[len(docRes.Sections)-1].Section.Position
		}
		var bErr error
		pos, bErr = order.Between(lastPos, "")
		if bErr != nil {
			return "", fmt.Errorf("writ: generate position: %w", bErr)
		}
	}

	secID := newObjectID()

	body := map[string]any{
		"document_id": docID,
		"position":    pos,
		"body":        ns.Body,
	}
	if ns.Title != "" {
		body["title"] = ns.Title
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("writ: marshal section body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   secID,
		ObjectType: "section",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, nil); err != nil {
		return "", fmt.Errorf("writ: append section: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return secID, nil
}

// EditSection records a new body version for a section, resolving any existing conflicts upon commit.
func (d *Documents) EditSection(ctx context.Context, sectionID string, body string) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if sectionID == "" {
		return fmt.Errorf("writ: section id cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Section(sectionID); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(sectionID)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"body": body})
	if err != nil {
		return fmt.Errorf("writ: marshal edit body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   sectionID,
		ObjectType: "section",
		OpType:     "edit",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append section edit: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// MoveSection reorders a section to between afterID and beforeID.
func (d *Documents) MoveSection(ctx context.Context, sectionID string, afterID, beforeID string) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if sectionID == "" {
		return fmt.Errorf("writ: section id cannot be empty")
	}
	if afterID == "" && beforeID == "" {
		return fmt.Errorf("writ: at least one of after or before must be specified to move section")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	sec, err := target.projection.Section(sectionID)
	if err != nil {
		return err
	}

	doc, err := target.projection.Document(sec.Section.DocumentID)
	if err != nil {
		return err
	}

	var afterPos, beforePos string
	if afterID != "" {
		found := false
		for _, s := range doc.Sections {
			if s.ObjectID == afterID {
				afterPos = s.Section.Position
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("writ: section %s not found in document", afterID)
		}
	}
	if beforeID != "" {
		found := false
		for _, s := range doc.Sections {
			if s.ObjectID == beforeID {
				beforePos = s.Section.Position
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("writ: section %s not found in document", beforeID)
		}
	}

	newPos, err := order.Between(afterPos, beforePos)
	if err != nil {
		return fmt.Errorf("writ: generate position: %w", err)
	}

	frontier, err := target.projection.Frontier(sectionID)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"position": newPos})
	if err != nil {
		return fmt.Errorf("writ: marshal move body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   sectionID,
		ObjectType: "section",
		OpType:     "move",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append section move: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// UpdateSection modifies title or position of an existing section.
func (d *Documents) UpdateSection(ctx context.Context, sectionID string, edit SectionEdit) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if sectionID == "" {
		return fmt.Errorf("writ: section id cannot be empty")
	}
	if edit.Title == nil && edit.Position == nil {
		return fmt.Errorf("writ: at least one property must be specified to update section")
	}

	if edit.Position != nil {
		if err := order.Validate(*edit.Position); err != nil {
			return fmt.Errorf("writ: invalid position %q: %w", *edit.Position, err)
		}
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Section(sectionID); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(sectionID)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	body := make(map[string]any)
	if edit.Title != nil {
		body["title"] = *edit.Title
	}
	if edit.Position != nil {
		body["position"] = *edit.Position
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("writ: marshal update body: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   sectionID,
		ObjectType: "section",
		OpType:     "update",
		OpVersion:  1,
		Body:       bodyBytes,
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append section update: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}

// DeleteSection tombstones a section.
func (d *Documents) DeleteSection(ctx context.Context, sectionID string) error {
	target, _, err := d.targetStore(ctx)
	if err != nil {
		return err
	}
	if err := target.ensureWritable(); err != nil {
		return err
	}

	if sectionID == "" {
		return fmt.Errorf("writ: section id cannot be empty")
	}

	if err := target.maybeAutoRefresh(ctx); err != nil {
		return fmt.Errorf("writ: auto refresh: %w", err)
	}

	if _, err := target.projection.Section(sectionID); err != nil {
		return err
	}

	frontier, err := target.projection.Frontier(sectionID)
	if err != nil {
		return fmt.Errorf("writ: get frontier: %w", err)
	}

	env := codec.Envelope{
		ObjectID:   sectionID,
		ObjectType: "section",
		OpType:     "delete",
		OpVersion:  1,
		Body:       []byte("{}"),
	}

	if _, err := target.dagStore.Append(ctx, env, frontier); err != nil {
		return fmt.Errorf("writ: append section delete: %w", err)
	}

	_ = target.maybeAutoRefresh(ctx)
	return nil
}
