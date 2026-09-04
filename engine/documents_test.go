package writ_test

import (
	"context"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
)

func TestDocumentsCRUDAndSections(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// 1. Create document
	docID, err := s.Documents.Create(ctx, writ.NewDocument{
		Title:  "Architecture RFC: Documents",
		Labels: []string{"design", "storage"},
		Links: []writ.Link{
			{Target: "issue-1", TargetType: "issue", Relation: "implementation-plan"},
		},
	})
	if err != nil {
		t.Fatalf("Create document: %v", err)
	}

	docRes, err := s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query.Document: %v", err)
	}
	if docRes.Document.Title != "Architecture RFC: Documents" {
		t.Errorf("Title = %q, want 'Architecture RFC: Documents'", docRes.Document.Title)
	}
	if len(docRes.Document.Labels) != 2 || docRes.Document.Labels[0] != "design" || docRes.Document.Labels[1] != "storage" {
		t.Errorf("Labels = %v, want ['design', 'storage']", docRes.Document.Labels)
	}
	if len(docRes.Document.Links) != 1 || docRes.Document.Links[0].Target != "issue-1" {
		t.Errorf("Links = %+v, want 1 link to issue-1", docRes.Document.Links)
	}

	// 2. Update document
	newTitle := "Architecture RFC: Documents (Revised)"
	if err := s.Documents.Update(ctx, docID, writ.DocumentEdit{
		Title: &newTitle,
		Labels: &writ.DocumentLabelEdit{
			Remove: []string{"storage"},
			Add:    []string{"rfc"},
		},
	}); err != nil {
		t.Fatalf("Update document: %v", err)
	}

	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query.Document: %v", err)
	}
	if docRes.Document.Title != newTitle {
		t.Errorf("Title = %q, want %q", docRes.Document.Title, newTitle)
	}
	if len(docRes.Document.Labels) != 2 || docRes.Document.Labels[0] != "design" || docRes.Document.Labels[1] != "rfc" {
		t.Errorf("Labels = %v, want ['design', 'rfc']", docRes.Document.Labels)
	}

	// 3. Add section 1
	sec1ID, err := s.Documents.AddSection(ctx, docID, writ.NewSection{
		Title: "Introduction",
		Body:  "Introductory paragraph.",
	})
	if err != nil {
		t.Fatalf("AddSection 1: %v", err)
	}

	// 4. Add section 2
	sec2ID, err := s.Documents.AddSection(ctx, docID, writ.NewSection{
		Title: "Design",
		Body:  "Design details.",
	})
	if err != nil {
		t.Fatalf("AddSection 2: %v", err)
	}

	// Verify sections in document order
	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query.Document: %v", err)
	}
	if len(docRes.Sections) != 2 {
		t.Fatalf("len(Sections) = %d, want 2", len(docRes.Sections))
	}
	if docRes.Sections[0].ObjectID != sec1ID || docRes.Sections[1].ObjectID != sec2ID {
		t.Errorf("Sections = [%s, %s], want [%s, %s]", docRes.Sections[0].ObjectID, docRes.Sections[1].ObjectID, sec1ID, sec2ID)
	}

	// 5. Move section 2 before section 1
	if err := s.Documents.MoveSection(ctx, sec2ID, "", sec1ID); err != nil {
		t.Fatalf("MoveSection: %v", err)
	}

	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query.Document: %v", err)
	}
	if docRes.Sections[0].ObjectID != sec2ID || docRes.Sections[1].ObjectID != sec1ID {
		t.Errorf("After move, Sections = [%s, %s], want [%s, %s]", docRes.Sections[0].ObjectID, docRes.Sections[1].ObjectID, sec2ID, sec1ID)
	}

	// 6. Edit section body
	if err := s.Documents.EditSection(ctx, sec1ID, "Updated intro body."); err != nil {
		t.Fatalf("EditSection: %v", err)
	}
	secRes, err := s.Query.Section(sec1ID)
	if err != nil {
		t.Fatalf("Query.Section: %v", err)
	}
	if secRes.Section.SettledBody() != "Updated intro body." {
		t.Errorf("Section body = %q, want 'Updated intro body.'", secRes.Section.SettledBody())
	}

	// 7. Update section title
	newSecTitle := "Background & Motivation"
	if err := s.Documents.UpdateSection(ctx, sec1ID, writ.SectionEdit{
		Title: &newSecTitle,
	}); err != nil {
		t.Fatalf("UpdateSection: %v", err)
	}
	secRes, err = s.Query.Section(sec1ID)
	if err != nil {
		t.Fatalf("Query.Section: %v", err)
	}
	if secRes.Section.Title != newSecTitle {
		t.Errorf("Section title = %q, want %q", secRes.Section.Title, newSecTitle)
	}

	// 8. Delete section 2
	if err := s.Documents.DeleteSection(ctx, sec2ID); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}
	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query.Document: %v", err)
	}
	if len(docRes.Sections) != 1 || docRes.Sections[0].ObjectID != sec1ID {
		t.Errorf("After delete, Sections = %v, want 1 section [%s]", docRes.Sections, sec1ID)
	}

	// 9. Query documents by label
	docs, err := s.Query.Documents(writ.DocumentFilter{
		Labels: []string{"design"},
	})
	if err != nil {
		t.Fatalf("Query.Documents: %v", err)
	}
	if len(docs) != 1 || docs[0].ObjectID != docID {
		t.Errorf("Query.Documents returned %v, want [%s]", docs, docID)
	}
}

func TestDocumentsWorkspaceScoping(t *testing.T) {
	ctx := context.Background()
	wsDir, _ := setupTestRepoWithID(t, "ws-writer", "ws@writ.dev")
	repoDir, _ := setupTestRepoWithID(t, "code-writer", "code@writ.dev")

	wsStore, err := writ.Open(wsDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open wsDir: %v", err)
	}
	defer wsStore.Close()

	appStore, err := writ.Open(repoDir, writ.WithSigner(dummySigner()), writ.WithWorkspace(wsDir))
	if err != nil {
		t.Fatalf("Open appStore: %v", err)
	}
	defer appStore.Close()

	// Create document through appStore - should route to wsDir
	docID, err := appStore.Documents.Create(ctx, writ.NewDocument{
		Title: "Workspace Global Doc",
	})
	if err != nil {
		t.Fatalf("Create doc via appStore: %v", err)
	}

	// Add section through appStore
	secID, err := appStore.Documents.AddSection(ctx, docID, writ.NewSection{
		Title: "Sec 1",
		Body:  "Body 1",
	})
	if err != nil {
		t.Fatalf("AddSection via appStore: %v", err)
	}

	// Query from appStore
	docFromApp, err := appStore.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query from appStore: %v", err)
	}
	if docFromApp.Document.Title != "Workspace Global Doc" {
		t.Errorf("appStore Title = %q", docFromApp.Document.Title)
	}
	if len(docFromApp.Sections) != 1 || docFromApp.Sections[0].ObjectID != secID {
		t.Errorf("appStore sections = %+v", docFromApp.Sections)
	}

	// Query from wsStore directly
	docFromWS, err := wsStore.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query from wsStore: %v", err)
	}
	if docFromWS.Document.Title != "Workspace Global Doc" {
		t.Errorf("wsStore Title = %q", docFromWS.Document.Title)
	}
}

func TestDocumentsSectionConcurrencyAndResolution(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	docID, err := s.Documents.Create(ctx, writ.NewDocument{Title: "Concurrency Doc"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	secID, err := s.Documents.AddSection(ctx, docID, writ.NewSection{
		Title: "Conflict Section",
		Body:  "Original body",
	})
	if err != nil {
		t.Fatalf("AddSection: %v", err)
	}

	// Get tip of section
	frontier, err := writ.StoreProjection(s).Frontier(secID)
	if err != nil {
		t.Fatalf("Frontier: %v", err)
	}

	// Construct two concurrent ops sharing the same causal parent
	aliceEnv := codec.Envelope{
		ObjectID:   secID,
		ObjectType: "section",
		OpType:     "edit",
		OpVersion:  1,
		Body:       []byte(`{"body":"Alice content"}`),
	}
	bobEnv := codec.Envelope{
		ObjectID:   secID,
		ObjectType: "section",
		OpType:     "edit",
		OpVersion:  1,
		Body:       []byte(`{"body":"Bob content"}`),
	}

	identBob := identity.Identity{
		WriterID: identity.WriterID("fedcba9876543210"),
		Author: identity.Author{
			Name:  "Bob",
			Email: "bob@example.com",
		},
	}
	storeBob, err := dag.Open(repoDir, identBob)
	if err != nil {
		t.Fatalf("Open Bob: %v", err)
	}

	aliceOp, err := writ.StoreDAGStore(s).Append(ctx, aliceEnv, frontier)
	if err != nil {
		t.Fatalf("Append Alice: %v", err)
	}
	bobOp, err := storeBob.Append(ctx, bobEnv, frontier)
	if err != nil {
		t.Fatalf("Append Bob: %v", err)
	}

	if _, err := s.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Query section - should be conflicted
	secRes, err := s.Query.Section(secID)
	if err != nil {
		t.Fatalf("Query.Section: %v", err)
	}
	if !secRes.Section.IsConflicted() {
		t.Fatalf("Expected section to be conflicted, got: %v", secRes.Section.Body)
	}
	bodies := secRes.Section.ConflictBodies()
	if len(bodies) != 2 || bodies[0] != "Alice content" || bodies[1] != "Bob content" {
		t.Errorf("Conflict bodies = %v, want ['Alice content', 'Bob content']", bodies)
	}

	// Now resolve the conflict by observing both tips
	resolveEnv := codec.Envelope{
		ObjectID:   secID,
		ObjectType: "section",
		OpType:     "edit",
		OpVersion:  1,
		Body:       []byte(`{"body":"Resolved content"}`),
	}
	if _, err := writ.StoreDAGStore(s).Append(ctx, resolveEnv, []string{aliceOp.ID, bobOp.ID}); err != nil {
		t.Fatalf("Append resolve: %v", err)
	}

	if _, err := s.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	secRes, err = s.Query.Section(secID)
	if err != nil {
		t.Fatalf("Query.Section resolved: %v", err)
	}
	if secRes.Section.IsConflicted() {
		t.Fatalf("Expected section to be resolved, but still conflicted: %v", secRes.Section.Body)
	}
	if secRes.Section.SettledBody() != "Resolved content" {
		t.Errorf("SettledBody = %q, want 'Resolved content'", secRes.Section.SettledBody())
	}
}

func TestAddSectionAndMoveSectionBounding(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	docID, err := s.Documents.Create(ctx, writ.NewDocument{
		Title: "Ordering Test Document",
	})
	if err != nil {
		t.Fatalf("Create doc: %v", err)
	}

	secA, err := s.Documents.AddSection(ctx, docID, writ.NewSection{Title: "A", Body: "Body A"})
	if err != nil {
		t.Fatalf("Add A: %v", err)
	}
	secB, err := s.Documents.AddSection(ctx, docID, writ.NewSection{Title: "B", Body: "Body B"})
	if err != nil {
		t.Fatalf("Add B: %v", err)
	}
	secC, err := s.Documents.AddSection(ctx, docID, writ.NewSection{Title: "C", Body: "Body C"})
	if err != nil {
		t.Fatalf("Add C: %v", err)
	}

	// 1. Add section D after A (without before). It must be placed between A and B, not after C.
	secD, err := s.Documents.AddSection(ctx, docID, writ.NewSection{Title: "D", Body: "Body D", After: secA})
	if err != nil {
		t.Fatalf("Add D after A: %v", err)
	}

	docRes, err := s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query doc: %v", err)
	}
	wantOrder := []string{secA, secD, secB, secC}
	if len(docRes.Sections) != len(wantOrder) {
		t.Fatalf("Sections count = %d, want %d", len(docRes.Sections), len(wantOrder))
	}
	for i, wantID := range wantOrder {
		if docRes.Sections[i].ObjectID != wantID {
			t.Errorf("Section[%d] = %s (%s), want %s", i, docRes.Sections[i].ObjectID, docRes.Sections[i].Section.Title, wantID)
		}
	}

	// 2. Add section E before B (without after). In [A, D, B, C], it must be placed between D and B.
	secE, err := s.Documents.AddSection(ctx, docID, writ.NewSection{Title: "E", Body: "Body E", Before: secB})
	if err != nil {
		t.Fatalf("Add E before B: %v", err)
	}

	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query doc: %v", err)
	}
	wantOrder = []string{secA, secD, secE, secB, secC}
	for i, wantID := range wantOrder {
		if docRes.Sections[i].ObjectID != wantID {
			t.Errorf("Section[%d] = %s (%s), want %s", i, docRes.Sections[i].ObjectID, docRes.Sections[i].Section.Title, wantID)
		}
	}

	// 3. Move C after D (without before). Current order: [A, D, E, B, C].
	// Moving C after D should place C between D and E: [A, D, C, E, B].
	if err := s.Documents.MoveSection(ctx, secC, secD, ""); err != nil {
		t.Fatalf("Move C after D: %v", err)
	}

	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query doc: %v", err)
	}
	wantOrder = []string{secA, secD, secC, secE, secB}
	for i, wantID := range wantOrder {
		if docRes.Sections[i].ObjectID != wantID {
			t.Errorf("After move C, Section[%d] = %s (%s), want %s", i, docRes.Sections[i].ObjectID, docRes.Sections[i].Section.Title, wantID)
		}
	}

	// 4. Move B before C (without after). Current order: [A, D, C, E, B].
	// Moving B before C should place B between D and C: [A, D, B, C, E].
	if err := s.Documents.MoveSection(ctx, secB, "", secC); err != nil {
		t.Fatalf("Move B before C: %v", err)
	}

	docRes, err = s.Query.Document(docID)
	if err != nil {
		t.Fatalf("Query doc: %v", err)
	}
	wantOrder = []string{secA, secD, secB, secC, secE}
	for i, wantID := range wantOrder {
		if docRes.Sections[i].ObjectID != wantID {
			t.Errorf("After move B, Section[%d] = %s (%s), want %s", i, docRes.Sections[i].ObjectID, docRes.Sections[i].Section.Title, wantID)
		}
	}

	// 5. Move relative to self should error
	if err := s.Documents.MoveSection(ctx, secB, secB, ""); err == nil {
		t.Errorf("expected error moving section relative to itself, got nil")
	}
	if err := s.Documents.MoveSection(ctx, secB, "", secB); err == nil {
		t.Errorf("expected error moving section relative to itself, got nil")
	}
}

func TestDocumentUpdateLinearFrontier(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	docID, err := s.Documents.Create(ctx, writ.NewDocument{
		Title: "Initial Title",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newTitle := "Updated Title"
	if err := s.Documents.Update(ctx, docID, writ.DocumentEdit{
		Title:  &newTitle,
		Labels: &writ.DocumentLabelEdit{Add: []string{"rfc"}},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	enumRes, err := writ.StoreDAGStore(s).Enumerate()
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}

	var updateOp, labelOp codec.Op
	for _, op := range enumRes.Ops[docID] {
		if op.OpType == "update" {
			updateOp = op
		} else if op.OpType == "label" {
			labelOp = op
		}
	}

	if updateOp.ID == "" {
		t.Fatalf("expected update op in dagStore, found none")
	}
	if labelOp.ID == "" {
		t.Fatalf("expected label op in dagStore, found none")
	}

	foundParent := false
	for _, p := range labelOp.Parents {
		if p == updateOp.ID {
			foundParent = true
			break
		}
	}
	if !foundParent {
		t.Errorf("labelOp.Parents = %v, want to contain updateOp.ID %s", labelOp.Parents, updateOp.ID)
	}
}

