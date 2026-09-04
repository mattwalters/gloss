package writ_test

import (
	"context"
	"testing"

	"github.com/writtendev/writ/engine"
)

func TestLabelsCRUD(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// 1. Validation: empty name
	if _, err := s.Labels.Create(ctx, writ.NewLabel{}); err == nil {
		t.Errorf("expected error creating label with empty name, got nil")
	}

	// 2. Create label
	lblID, err := s.Labels.Create(ctx, writ.NewLabel{
		Name:        "bug",
		Color:       "#d73a4a",
		Description: "Something isn't working",
	})
	if err != nil {
		t.Fatalf("Create label failed: %v", err)
	}
	if lblID == "" {
		t.Fatalf("expected non-empty label ID")
	}

	// 3. Query label list
	labels, err := s.Query.Labels(writ.LabelFilter{})
	if err != nil {
		t.Fatalf("Query.Labels failed: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("expected 1 label, got %d", len(labels))
	}
	if labels[0].ObjectID != lblID || labels[0].Label.Name != "bug" || labels[0].Label.Color != "#d73a4a" {
		t.Errorf("unexpected label result: %+v", labels[0])
	}

	// 4. Query single label
	single, err := s.Query.Label(lblID)
	if err != nil {
		t.Fatalf("Query.Label failed: %v", err)
	}
	if single.Label.Description != "Something isn't working" {
		t.Errorf("unexpected description: %q", single.Label.Description)
	}

	// 5. Update validation
	if err := s.Labels.Update(ctx, lblID, writ.LabelEdit{}); err == nil {
		t.Errorf("expected error updating label with empty edit, got nil")
	}
	emptyName := ""
	if err := s.Labels.Update(ctx, lblID, writ.LabelEdit{Name: &emptyName}); err == nil {
		t.Errorf("expected error updating label with empty name, got nil")
	}

	// 6. Update label
	newName := "defect"
	newColor := "#e2b93c"
	if err := s.Labels.Update(ctx, lblID, writ.LabelEdit{
		Name:  &newName,
		Color: &newColor,
	}); err != nil {
		t.Fatalf("Update label failed: %v", err)
	}

	updated, err := s.Query.Label(lblID)
	if err != nil {
		t.Fatalf("Query.Label after update failed: %v", err)
	}
	if updated.Label.Name != "defect" || updated.Label.Color != "#e2b93c" || updated.Label.Description != "Something isn't working" {
		t.Errorf("unexpected updated label: %+v", updated.Label)
	}
}
