package writ_test

import (
	"context"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/resolve"
)

func TestReviewsDomainGuards(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	headHash := strings.TrimSpace(runGitCmd(t, repoDir, "rev-parse", "HEAD"))
	blobHash := strings.TrimSpace(runGitCmd(t, repoDir, "rev-parse", "HEAD:README.md"))

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Domain guard testing",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	t.Run("SetStatus invalid status enum", func(t *testing.T) {
		err := s.Reviews.SetStatus(ctx, reviewID, writ.ReviewStatus{Status: "Open"})
		if err == nil {
			t.Fatal("expected error for invalid status 'Open', got nil")
		}
		want := `writ: invalid status "Open" (must be draft, open, closed, or merged)`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("SetStatus invalid merge commit abbreviated SHA", func(t *testing.T) {
		err := s.Reviews.SetStatus(ctx, reviewID, writ.ReviewStatus{
			Status:      "merged",
			MergeCommit: "abc1234",
		})
		if err == nil {
			t.Fatal("expected error for abbreviated merge commit SHA, got nil")
		}
		want := `writ: merge commit must be a commit OID, not a ref name: "abc1234"`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Approve invalid verdict enum", func(t *testing.T) {
		err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
			Verdict: "request_changes",
		})
		if err == nil {
			t.Fatal("expected error for verdict 'request_changes', got nil")
		}
		want := `writ: invalid verdict "request_changes" (must be approve, request-changes, or none)`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Approve invalid revision ref name", func(t *testing.T) {
		err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
			Verdict:  "approve",
			Revision: "HEAD",
		})
		if err == nil {
			t.Fatal("expected error for revision 'HEAD', got nil")
		}
		want := `writ: revision must be a commit OID, not a ref name: "HEAD"`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Link invalid relation enum", func(t *testing.T) {
		err := s.Reviews.Link(ctx, reviewID, writ.Link{
			Target:   reviewID,
			Relation: "blocks",
		})
		if err == nil {
			t.Fatal("expected error for relation 'blocks', got nil")
		}
		want := `writ: invalid relation "blocks" (must be fixes, relates, or none)`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Label empty string in add", func(t *testing.T) {
		err := s.Reviews.Label(ctx, reviewID, []string{""}, nil)
		if err == nil {
			t.Fatal("expected error for empty label in add, got nil")
		}
		want := "writ: label cannot be empty"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Label empty string in remove", func(t *testing.T) {
		err := s.Reviews.Label(ctx, reviewID, nil, []string{""})
		if err == nil {
			t.Fatal("expected error for empty label in remove, got nil")
		}
		want := "writ: label cannot be empty"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment empty zero-value Anchor", func(t *testing.T) {
		_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text:   "comment with zero anchor",
			Anchor: &writ.Anchor{},
		})
		if err == nil {
			t.Fatal("expected error for zero-value anchor, got nil")
		}
		want := "writ: invalid anchor version 0 (must be 1)"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment anchor missing sides", func(t *testing.T) {
		_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text:   "comment with no sides",
			Anchor: &writ.Anchor{Version: 1},
		})
		if err == nil {
			t.Fatal("expected error for anchor with no sides, got nil")
		}
		want := "writ: anchor must specify at least one of old or new side"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment anchor invalid commit ref", func(t *testing.T) {
		_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text: "comment with ref commit in anchor",
			Anchor: &writ.Anchor{
				Version: 1,
				New: &resolve.SideAnchor{
					Commit: "HEAD",
					Path:   "README.md",
					Blob:   blobHash,
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for ref in anchor commit, got nil")
		}
		want := `writ: new side anchor commit must be a commit OID, not a ref name: "HEAD"`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment anchor non-relative path", func(t *testing.T) {
		_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text: "comment with absolute path in anchor",
			Anchor: &writ.Anchor{
				Version: 1,
				New: &resolve.SideAnchor{
					Commit: headHash,
					Path:   "/README.md",
					Blob:   blobHash,
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for absolute path in anchor, got nil")
		}
		want := `writ: new side anchor path must be relative, got "/README.md"`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment anchor range missing context", func(t *testing.T) {
		_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text: "comment with range but missing context",
			Anchor: &writ.Anchor{
				Version: 1,
				New: &resolve.SideAnchor{
					Commit: headHash,
					Path:   "README.md",
					Blob:   blobHash,
					Range:  &resolve.Range{Start: 1, End: 2},
				},
			},
		})
		if err == nil {
			t.Fatal("expected error for range without context, got nil")
		}
		want := "writ: new side anchor has range but missing context"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})
}

func TestIssuesDomainGuards(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	issueID, err := s.Issues.Create(ctx, writ.NewIssue{
		Title: "Domain guard testing for issues",
	})
	if err != nil {
		t.Fatalf("Create issue failed: %v", err)
	}

	t.Run("SetState invalid state enum", func(t *testing.T) {
		err := s.Issues.SetState(ctx, issueID, writ.IssueState{State: "resolved"})
		if err == nil {
			t.Fatal("expected error for invalid state 'resolved', got nil")
		}
		want := `writ: invalid state "resolved" (must be open or closed)`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Update empty title rejected", func(t *testing.T) {
		empty := ""
		err := s.Issues.Update(ctx, issueID, writ.IssueEdit{Title: &empty})
		if err == nil {
			t.Fatal("expected error for empty issue title, got nil")
		}
		want := "writ: issue title cannot be empty: pass a nil title to leave it unchanged"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Link invalid relation enum", func(t *testing.T) {
		err := s.Issues.Link(ctx, issueID, writ.Link{
			Target:   issueID,
			Relation: "blocks",
		})
		if err == nil {
			t.Fatal("expected error for relation 'blocks', got nil")
		}
		want := `writ: invalid relation "blocks" (must be fixes, relates, or none)`
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Label empty string in add", func(t *testing.T) {
		err := s.Issues.Label(ctx, issueID, []string{""}, nil)
		if err == nil {
			t.Fatal("expected error for empty label in add, got nil")
		}
		want := "writ: label cannot be empty"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Label empty string in remove", func(t *testing.T) {
		err := s.Issues.Label(ctx, issueID, nil, []string{""})
		if err == nil {
			t.Fatal("expected error for empty label in remove, got nil")
		}
		want := "writ: label cannot be empty"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("Comment zero-value Anchor", func(t *testing.T) {
		_, err := s.Issues.Comment(ctx, issueID, writ.NewComment{
			Text:   "issue comment with zero anchor",
			Anchor: &writ.Anchor{},
		})
		if err == nil {
			t.Fatal("expected error for zero anchor on issue comment, got nil")
		}
		want := "writ: invalid anchor version 0 (must be 1)"
		if err.Error() != want {
			t.Errorf("got error %q, want %q", err.Error(), want)
		}
	})
}
