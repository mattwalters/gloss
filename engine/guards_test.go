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

	t.Run("Comment anchor valid multibyte context lines exceeding 1000 bytes", func(t *testing.T) {
		line2000Bytes := strings.Repeat("é", 1000) // 1000 runes, 2000 bytes
		line1500Bytes := strings.Repeat("世", 500)  // 500 runes, 1500 bytes
		line4000Bytes := strings.Repeat("𠀀", 1000) // 1000 runes, 4000 bytes

		for _, tc := range []struct {
			name string
			side string
			ctx  *resolve.Context
		}{
			{
				name: "new side with 1000 2-byte runes in lines, 500 3-byte runes in before, 1000 4-byte runes in after",
				side: "new",
				ctx: &resolve.Context{
					Before: []string{line1500Bytes},
					Lines:  []string{line2000Bytes},
					After:  []string{line4000Bytes},
				},
			},
			{
				name: "old side with multibyte lines",
				side: "old",
				ctx: &resolve.Context{
					Before: []string{line4000Bytes},
					Lines:  []string{line1500Bytes},
					After:  []string{line2000Bytes},
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				anchor := &writ.Anchor{Version: 1}
				sideAnchor := &resolve.SideAnchor{
					Commit:  headHash,
					Path:    "README.md",
					Blob:    blobHash,
					Range:   &resolve.Range{Start: 1, End: 1},
					Context: tc.ctx,
				}
				if tc.side == "new" {
					anchor.New = sideAnchor
				} else {
					anchor.Old = sideAnchor
				}
				_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
					Text:   "valid multibyte anchor comment",
					Anchor: anchor,
				})
				if err != nil {
					t.Fatalf("expected success for valid multibyte context, got error: %v", err)
				}
			})
		}
	})

	t.Run("Comment anchor context line exceeds 1000 characters", func(t *testing.T) {
		cases := []struct {
			name  string
			side  string
			label string
			ctx   *resolve.Context
			want  string
		}{
			{
				name:  "new side lines exceeds 1000 ascii characters",
				side:  "new",
				label: "lines",
				ctx: &resolve.Context{
					Lines: []string{strings.Repeat("a", 1001)},
				},
				want: "writ: new side anchor context lines line exceeds 1000 characters",
			},
			{
				name:  "new side lines exceeds 1000 multibyte 2-byte characters",
				side:  "new",
				label: "lines",
				ctx: &resolve.Context{
					Lines: []string{strings.Repeat("é", 1001)},
				},
				want: "writ: new side anchor context lines line exceeds 1000 characters",
			},
			{
				name:  "new side lines exceeds 1000 supplementary 4-byte characters",
				side:  "new",
				label: "lines",
				ctx: &resolve.Context{
					Lines: []string{strings.Repeat("𠀀", 1001)},
				},
				want: "writ: new side anchor context lines line exceeds 1000 characters",
			},
			{
				name:  "new side before line exceeds 1000 characters",
				side:  "new",
				label: "before",
				ctx: &resolve.Context{
					Before: []string{strings.Repeat("é", 1001)},
					Lines:  []string{"valid"},
				},
				want: "writ: new side anchor context before line exceeds 1000 characters",
			},
			{
				name:  "new side after line exceeds 1000 characters",
				side:  "new",
				label: "after",
				ctx: &resolve.Context{
					Lines: []string{"valid"},
					After: []string{strings.Repeat("世", 1001)},
				},
				want: "writ: new side anchor context after line exceeds 1000 characters",
			},
			{
				name:  "old side lines exceeds 1000 characters",
				side:  "old",
				label: "lines",
				ctx: &resolve.Context{
					Lines: []string{strings.Repeat("é", 1001)},
				},
				want: "writ: old side anchor context lines line exceeds 1000 characters",
			},
			{
				name:  "old side before line exceeds 1000 characters",
				side:  "old",
				label: "before",
				ctx: &resolve.Context{
					Before: []string{strings.Repeat("é", 1001)},
					Lines:  []string{"valid"},
				},
				want: "writ: old side anchor context before line exceeds 1000 characters",
			},
			{
				name:  "old side after line exceeds 1000 characters",
				side:  "old",
				label: "after",
				ctx: &resolve.Context{
					Lines: []string{"valid"},
					After: []string{strings.Repeat("é", 1001)},
				},
				want: "writ: old side anchor context after line exceeds 1000 characters",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				anchor := &writ.Anchor{Version: 1}
				sideAnchor := &resolve.SideAnchor{
					Commit:  headHash,
					Path:    "README.md",
					Blob:    blobHash,
					Range:   &resolve.Range{Start: 1, End: 1},
					Context: tc.ctx,
				}
				if tc.side == "new" {
					anchor.New = sideAnchor
				} else {
					anchor.Old = sideAnchor
				}
				_, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
					Text:   "anchor line length test",
					Anchor: anchor,
				})
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.want)
				}
				if err.Error() != tc.want {
					t.Errorf("got error %q, want %q", err.Error(), tc.want)
				}
			})
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

	t.Run("SetState invalid state reference", func(t *testing.T) {
		err := s.Issues.SetState(ctx, issueID, writ.IssueState{State: "invalid state with spaces"})
		if err == nil {
			t.Fatal("expected error for invalid state reference, got nil")
		}
		if !strings.Contains(err.Error(), "invalid issue state reference") {
			t.Errorf("got error %q, want reference validation error", err.Error())
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
