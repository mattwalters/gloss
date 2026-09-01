package writ_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/state"
)

func TestIssuesLifecycleAndFoldAgreement(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create Issue
	issueID, err := s.Issues.Create(ctx, writ.NewIssue{
		Title:       "Fix parser crash on empty input",
		Description: "Parser panics with slice index out of range",
	})
	if err != nil {
		t.Fatalf("Issues.Create failed: %v", err)
	}
	if issueID == "" {
		t.Fatal("expected non-empty issue ID")
	}

	// 2. Update metadata
	newTitle := "Fix parser crash on empty and nil input"
	if err := s.Issues.Update(ctx, issueID, writ.IssueEdit{Title: &newTitle}); err != nil {
		t.Fatalf("Issues.Update failed: %v", err)
	}

	// 3. Assignees
	if err := s.Issues.Assign(ctx, issueID, []string{"alice", "bob"}, nil); err != nil {
		t.Fatalf("Issues.Assign add failed: %v", err)
	}
	if err := s.Issues.Assign(ctx, issueID, []string{"charlie"}, []string{"alice"}); err != nil {
		t.Fatalf("Issues.Assign remove failed: %v", err)
	}

	// 4. Labels
	if err := s.Issues.Label(ctx, issueID, []string{"bug", "parser", "v1"}, nil); err != nil {
		t.Fatalf("Issues.Label add failed: %v", err)
	}
	if err := s.Issues.Label(ctx, issueID, nil, []string{"v1"}); err != nil {
		t.Fatalf("Issues.Label remove failed: %v", err)
	}

	// 5. Link
	if err := s.Issues.Link(ctx, issueID, writ.Link{
		Target:     "r-12345",
		TargetType: "review",
		Relation:   "fixes",
	}); err != nil {
		t.Fatalf("Issues.Link failed: %v", err)
	}

	// 6. Comment
	commentID, err := s.Issues.Comment(ctx, issueID, writ.NewComment{
		Text: "Root cause found in lexer tokenizer.",
	})
	if err != nil {
		t.Fatalf("Issues.Comment failed: %v", err)
	}
	if commentID == "" {
		t.Fatal("expected non-empty comment ID")
	}

	// 7. Set state
	if err := s.Issues.SetState(ctx, issueID, writ.IssueState{
		State:  "closed",
		Reason: "completed",
	}); err != nil {
		t.Fatalf("Issues.SetState failed: %v", err)
	}

	// Assert Query result
	res, err := s.Query.Issue(issueID)
	if err != nil {
		t.Fatalf("Query.Issue failed: %v", err)
	}

	if res.Issue.Title != newTitle {
		t.Errorf("got Title %q, want %q", res.Issue.Title, newTitle)
	}
	if res.Issue.State != "closed" || res.Issue.Reason != "completed" {
		t.Errorf("got State %q Reason %q", res.Issue.State, res.Issue.Reason)
	}
	if !reflect.DeepEqual(res.Issue.Assignees, []string{"bob", "charlie"}) {
		t.Errorf("unexpected assignees: %+v", res.Issue.Assignees)
	}
	if !reflect.DeepEqual(res.Issue.Labels, []string{"bug", "parser"}) {
		t.Errorf("unexpected labels: %+v", res.Issue.Labels)
	}
	if len(res.Issue.Links) != 1 || res.Issue.Links[0].Target != "r-12345" {
		t.Errorf("unexpected links: %+v", res.Issue.Links)
	}

	// Verify agreement between Query.Issue and state.FoldIssue over dag.Enumerate
	ident, _ := identity.ParseWriterID("0123456789abcdef")
	dagStore, err := dag.Open(repoDir, identity.Identity{WriterID: ident})
	if err != nil {
		t.Fatalf("dag.Open failed: %v", err)
	}
	enumRes, err := dagStore.Enumerate()
	if err != nil {
		t.Fatalf("dagStore.Enumerate failed: %v", err)
	}
	foldedIssue, err := state.FoldIssue(enumRes.Ops[issueID])
	if err != nil {
		t.Fatalf("FoldIssue failed: %v", err)
	}

	if !reflect.DeepEqual(res.Issue, foldedIssue) {
		t.Errorf("Query.Issue and FoldIssue mismatch:\n Query:  %+v\n Folded: %+v", res.Issue, foldedIssue)
	}

	// Verify threads query on issue
	threads, err := s.Query.Threads("issue", issueID)
	if err != nil {
		t.Fatalf("Query.Threads failed: %v", err)
	}
	if len(threads) != 1 || threads[0].ObjectID != commentID {
		t.Errorf("unexpected issue threads: %+v", threads)
	}
}

func TestIssuesWritePathEnvelopes(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	issueID, err := s.Issues.Create(ctx, writ.NewIssue{
		Title:       "Issue Spec Envelope Test",
		Description: "Verifies envelope binding",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}

	ref, err := repo.Reference(plumbing.ReferenceName("refs/writ/0123456789abcdef/issue"), true)
	if err != nil {
		t.Fatalf("Reference lookup: %v", err)
	}

	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		t.Fatalf("commit Tree: %v", err)
	}

	opEntry, err := tree.FindEntry("op.json")
	if err != nil {
		t.Fatalf("op.json not found: %v", err)
	}

	blob, err := repo.BlobObject(opEntry.Hash)
	if err != nil {
		t.Fatalf("BlobObject: %v", err)
	}

	r, err := blob.Reader()
	if err != nil {
		t.Fatalf("blob Reader: %v", err)
	}
	defer r.Close()

	var payload map[string]any
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		t.Fatalf("json decode op.json: %v", err)
	}

	if payload["object_id"] != issueID {
		t.Errorf("got object_id %v, want %v", payload["object_id"], issueID)
	}
	if payload["object_type"] != "issue" {
		t.Errorf("got object_type %v, want 'issue'", payload["object_type"])
	}
	if payload["op_type"] != "create" {
		t.Errorf("got op_type %v, want 'create'", payload["op_type"])
	}
	if payload["op_version"] != float64(1) {
		t.Errorf("got op_version %v, want 1", payload["op_version"])
	}
}

func TestIssuesValidationAndNotFound(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	missingID := "00000000000000000000000000000000"
	newTitle := "New Title"

	if err := s.Issues.Update(ctx, missingID, writ.IssueEdit{Title: &newTitle}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Update on missing issue, got %v", err)
	}
	if err := s.Issues.SetState(ctx, missingID, writ.IssueState{State: "closed"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for SetState on missing issue, got %v", err)
	}
	if err := s.Issues.Assign(ctx, missingID, []string{"alice"}, nil); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Assign on missing issue, got %v", err)
	}
	if err := s.Issues.Label(ctx, missingID, []string{"bug"}, nil); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Label on missing issue, got %v", err)
	}
	if err := s.Issues.Link(ctx, missingID, writ.Link{Target: "r-1", Relation: "fixes"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Link on missing issue, got %v", err)
	}
	if _, err := s.Issues.Comment(ctx, missingID, writ.NewComment{Text: "text"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Comment on missing issue, got %v", err)
	}
}

// TestIssuesAssignPersonIDLengthBound checks that the person-id length bound
// (spec/schemas/identifiers.schema.json, maxLength: 320) is enforced on issue
// assignment too. Over-length input is refused rather than truncated.
func TestIssuesAssignPersonIDLengthBound(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	issueID, err := s.Issues.Create(ctx, writ.NewIssue{Title: "Person ID Bound Issue"})
	if err != nil {
		t.Fatalf("Issues.Create failed: %v", err)
	}

	atLimit := personIDAtLimit(t)
	overLimit := personIDOverLimit(t)

	if err := s.Issues.Assign(ctx, issueID, []string{atLimit}, nil); err != nil {
		t.Fatalf("Assign with a 320-character assignee failed: %v", err)
	}
	err = s.Issues.Assign(ctx, issueID, []string{overLimit}, nil)
	if err == nil {
		t.Fatal("expected Assign to reject a 321-character assignee, got nil error")
	}
	if !strings.Contains(err.Error(), "320") {
		t.Errorf("expected an error naming the 320-character limit, got %q", err)
	}
	if err := s.Issues.Assign(ctx, issueID, nil, []string{overLimit}); err == nil {
		t.Error("expected Assign to reject a 321-character removal, got nil error")
	}

	res, err := s.Query.Issue(issueID)
	if err != nil {
		t.Fatalf("Query.Issue failed: %v", err)
	}
	if !reflect.DeepEqual(res.Issue.Assignees, []string{atLimit}) {
		t.Errorf("expected the 320-character assignee to round-trip unchanged, got %v", res.Issue.Assignees)
	}
}
