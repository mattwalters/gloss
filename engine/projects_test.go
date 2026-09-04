package writ_test

import (
	"context"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine"
)

func TestProjectsCRUDAndStatusLifecycle(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	// Validation: empty title.
	if _, err := s.Projects.Create(ctx, writ.NewProject{}); err == nil {
		t.Errorf("expected error creating project with empty title, got nil")
	}

	// Create.
	projectID, err := s.Projects.Create(ctx, writ.NewProject{
		Title:       "Authentication Redesign",
		Description: "Redesign auth flow to support SAML and OIDC",
	})
	if err != nil {
		t.Fatalf("Create project failed: %v", err)
	}
	if projectID == "" {
		t.Fatalf("expected non-empty project ID")
	}

	created, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	if created.Project.Title != "Authentication Redesign" {
		t.Errorf("Title = %q, want %q", created.Project.Title, "Authentication Redesign")
	}
	if created.Project.Description != "Redesign auth flow to support SAML and OIDC" {
		t.Errorf("Description = %q", created.Project.Description)
	}

	// Update validation.
	if err := s.Projects.Update(ctx, projectID, writ.ProjectEdit{}); err == nil {
		t.Errorf("expected error updating project with empty edit, got nil")
	}
	emptyTitle := ""
	if err := s.Projects.Update(ctx, projectID, writ.ProjectEdit{Title: &emptyTitle}); err == nil {
		t.Errorf("expected error updating project with empty title, got nil")
	}

	// Update.
	newTitle := "Authentication & SSO Redesign"
	newDescription := "Updated scope"
	if err := s.Projects.Update(ctx, projectID, writ.ProjectEdit{
		Title:       &newTitle,
		Description: &newDescription,
	}); err != nil {
		t.Fatalf("Update project failed: %v", err)
	}

	updated, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project after update failed: %v", err)
	}
	if updated.Project.Title != newTitle || updated.Project.Description != newDescription {
		t.Errorf("unexpected updated project: %+v", updated.Project)
	}

	// An explicitly-set empty description clears the field.
	clearedDescription := ""
	if err := s.Projects.Update(ctx, projectID, writ.ProjectEdit{Description: &clearedDescription}); err != nil {
		t.Fatalf("Update project (clear description) failed: %v", err)
	}
	cleared, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project after clearing description failed: %v", err)
	}
	if cleared.Project.Description != "" {
		t.Errorf("Description = %q, want empty after clearing", cleared.Project.Description)
	}

	// Invalid status is refused, naming the spec options.
	err = s.Projects.SetStatus(ctx, projectID, "bogus", "")
	if err == nil {
		t.Fatalf("expected error setting invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "planned") || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error %q does not name the spec status options", err.Error())
	}

	// SetStatus round-trips.
	if err := s.Projects.SetStatus(ctx, projectID, "paused", "waiting on upstream"); err != nil {
		t.Fatalf("SetStatus failed: %v", err)
	}
	paused, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project after SetStatus failed: %v", err)
	}
	if paused.Project.Status != "paused" || paused.Project.Reason != "waiting on upstream" {
		t.Errorf("unexpected status/reason: %+v", paused.Project)
	}

	// Cancellation is a status transition, not a delete: the project stays queryable.
	if err := s.Projects.SetStatus(ctx, projectID, "canceled", ""); err != nil {
		t.Fatalf("SetStatus(canceled) failed: %v", err)
	}
	canceled, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project after cancellation failed: %v", err)
	}
	if canceled.Project.Status != "canceled" {
		t.Errorf("Status = %q, want canceled", canceled.Project.Status)
	}
}

func TestProjectsMembershipReferenceAliasing(t *testing.T) {
	ctx := context.Background()
	repoDir, localRepoID := setupTestRepoWithID(t, "alice", "alice@writ.dev")

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	projectID, err := s.Projects.Create(ctx, writ.NewProject{Title: "Board"})
	if err != nil {
		t.Fatalf("Create project failed: %v", err)
	}

	issueID := "0123456789abcdef0123456789abcdef"

	// A bare reference and a qualified <local-repo-id>#<same-object-id> reference
	// must canonicalize to the same member string, or fold's byte-for-byte
	// comparison would treat them as two distinct members.
	if err := s.Projects.AddIssue(ctx, projectID, issueID); err != nil {
		t.Fatalf("AddIssue (bare) failed: %v", err)
	}
	qualifiedLocal := string(localRepoID) + "#" + issueID
	if err := s.Projects.AddIssue(ctx, projectID, qualifiedLocal); err != nil {
		t.Fatalf("AddIssue (qualified local) failed: %v", err)
	}

	res, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	if len(res.Project.Issues) != 1 {
		t.Fatalf("Issues = %v, want exactly one member (aliasing not collapsed)", res.Project.Issues)
	}
	if res.Project.Issues[0] != issueID {
		t.Errorf("Issues[0] = %q, want bare form %q", res.Project.Issues[0], issueID)
	}

	// A foreign-repo qualified reference stays qualified: it names a different
	// object than the local one and must not collapse into it.
	foreignRepoID := "fedcba9876543210fedcba9876543210"
	foreignIssue := "abcdef0123456789abcdef0123456789"
	qualifiedForeign := foreignRepoID + "#" + foreignIssue
	if err := s.Projects.AddIssue(ctx, projectID, qualifiedForeign); err != nil {
		t.Fatalf("AddIssue (qualified foreign) failed: %v", err)
	}

	res, err = s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	if len(res.Project.Issues) != 2 {
		t.Fatalf("Issues = %v, want 2 members", res.Project.Issues)
	}
	foundForeign := false
	for _, iss := range res.Project.Issues {
		if iss == qualifiedForeign {
			foundForeign = true
		}
	}
	if !foundForeign {
		t.Errorf("Issues = %v, want %q to stay qualified", res.Project.Issues, qualifiedForeign)
	}

	// RemoveIssue, canonicalized the same way, drops the member the bare/local
	// add wrote.
	if err := s.Projects.RemoveIssue(ctx, projectID, issueID); err != nil {
		t.Fatalf("RemoveIssue failed: %v", err)
	}
	res, err = s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project after RemoveIssue failed: %v", err)
	}
	for _, iss := range res.Project.Issues {
		if iss == issueID {
			t.Errorf("Issues = %v, want %q removed", res.Project.Issues, issueID)
		}
	}

	// A malformed reference is refused and appends nothing.
	before, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	malformed := []string{
		"has whitespace",
		"too#many#hashes",
		"not-hex#" + issueID,
	}
	for _, m := range malformed {
		if err := s.Projects.AddIssue(ctx, projectID, m); err == nil {
			t.Errorf("AddIssue(%q) expected error, got nil", m)
		}
	}
	after, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	if len(after.Project.Issues) != len(before.Project.Issues) {
		t.Errorf("Issues changed after malformed AddIssue calls: before=%v after=%v", before.Project.Issues, after.Project.Issues)
	}
}

// TestProjectsMembershipQualifiedReferenceStaysQualifiedWhenLocalRepoIDUnset
// pins the documented consequence of writ.repoId being optional: when it is
// unset, a caller-supplied qualified reference that happens to name this repo
// cannot be recognized as local, and is written qualified rather than bare.
func TestProjectsMembershipQualifiedReferenceStaysQualifiedWhenLocalRepoIDUnset(t *testing.T) {
	ctx := context.Background()
	repoDir, _ := setupConfiguredRepo(t) // no writ.repoId configured

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	projectID, err := s.Projects.Create(ctx, writ.NewProject{Title: "Board"})
	if err != nil {
		t.Fatalf("Create project failed: %v", err)
	}

	issueID := "0123456789abcdef0123456789abcdef"
	someRepoID := "fedcba9876543210fedcba9876543210"
	qualified := someRepoID + "#" + issueID

	if err := s.Projects.AddIssue(ctx, projectID, qualified); err != nil {
		t.Fatalf("AddIssue failed: %v", err)
	}

	res, err := s.Query.Project(projectID)
	if err != nil {
		t.Fatalf("Query.Project failed: %v", err)
	}
	if len(res.Project.Issues) != 1 || res.Project.Issues[0] != qualified {
		t.Errorf("Issues = %v, want exactly [%q] (stays qualified)", res.Project.Issues, qualified)
	}
}
