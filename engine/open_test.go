package writ_test

import (
	"context"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	writsync "github.com/writtendev/writ/engine/sync"
)

func TestOpenWithRepositoryExtensions(t *testing.T) {
	extensionScenarios := []struct {
		name       string
		configArgs [][]string
	}{
		{
			name: "worktreeConfig (sparse-checkout)",
			configArgs: [][]string{
				{"core.repositoryformatversion", "0"},
				{"extensions.worktreeConfig", "true"},
			},
		},
		{
			name: "partialClone",
			configArgs: [][]string{
				{"core.repositoryformatversion", "0"},
				{"extensions.partialClone", "origin"},
			},
		},
		{
			name: "preciousObjects",
			configArgs: [][]string{
				{"core.repositoryformatversion", "0"},
				{"extensions.preciousObjects", "true"},
			},
		},
		{
			name: "all standard extensions combined",
			configArgs: [][]string{
				{"core.repositoryformatversion", "0"},
				{"extensions.worktreeConfig", "true"},
				{"extensions.partialClone", "origin"},
				{"extensions.preciousObjects", "true"},
			},
		},
	}

	for _, tc := range extensionScenarios {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, _ := setupConfiguredRepo(t)

			// Apply git config extensions
			for _, cfg := range tc.configArgs {
				runGitCmd(t, repoDir, "config", cfg[0], cfg[1])
			}

			ctx := context.Background()

			// 1. writ.Open
			s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
			if err != nil {
				t.Fatalf("writ.Open failed on repo with %s: %v", tc.name, err)
			}
			defer s.Close()

			// 2. Create Issue
			issueID, err := s.Issues.Create(ctx, writ.NewIssue{
				Title:       "Test issue with extensions",
				Description: "Testing repository extensions compatibility",
			})
			if err != nil {
				t.Fatalf("Issues.Create failed: %v", err)
			}
			if issueID == "" {
				t.Fatal("expected non-empty issue ID")
			}

			// 3. Create Review
			revID, err := s.Reviews.Create(ctx, writ.NewReview{
				Title: "Test review with extensions",
			})
			if err != nil {
				t.Fatalf("Reviews.Create failed: %v", err)
			}
			if revID == "" {
				t.Fatal("expected non-empty review ID")
			}

			// 4. Create Comment
			commentID, err := s.Reviews.Comment(ctx, revID, writ.NewComment{
				Text: "Looks good to me!",
			})
			if err != nil {
				t.Fatalf("Reviews.Comment failed: %v", err)
			}
			if commentID == "" {
				t.Fatal("expected non-empty comment ID")
			}

			// 5. Query
			issues, err := s.Query.Issues(writ.IssueFilter{})
			if err != nil {
				t.Fatalf("Query.Issues failed: %v", err)
			}
			if len(issues) != 1 || issues[0].ObjectID != issueID {
				t.Fatalf("unexpected issues query result: %+v", issues)
			}

			reviews, err := s.Query.Reviews(writ.ReviewFilter{})
			if err != nil {
				t.Fatalf("Query.Reviews failed: %v", err)
			}
			if len(reviews) != 1 || reviews[0].ObjectID != revID {
				t.Fatalf("unexpected reviews query result: %+v", reviews)
			}

			comments, err := s.Query.Comments(writ.CommentFilter{SubjectID: revID})
			if err != nil {
				t.Fatalf("Query.Comments failed: %v", err)
			}
			if len(comments) != 1 || comments[0].ObjectID != commentID {
				t.Fatalf("unexpected comments query result: %+v", comments)
			}

			// 6. Refresh projection
			stats, err := s.Refresh(ctx)
			if err != nil {
				t.Fatalf("Refresh failed: %v", err)
			}
			if stats.ObjectsTouched < 0 {
				t.Fatalf("unexpected stats: %+v", stats)
			}

			// 7. Verify dag.Open directly
			ident := identity.Identity{
				WriterID: "0123456789abcdef",
				Author:   identity.Author{Name: "Alice Test", Email: "alice@example.com"},
			}
			dagStore, err := dag.Open(repoDir, ident)
			if err != nil {
				t.Fatalf("dag.Open failed on repo with %s: %v", tc.name, err)
			}
			chains, err := dag.Chains(dagStore.Storer())
			if err != nil {
				t.Fatalf("dag.Chains failed: %v", err)
			}
			if len(chains) == 0 {
				t.Fatal("expected non-empty chains")
			}

			// 8. Verify sync.Open directly
			syncClient, err := writsync.Open(repoDir, ident)
			if err != nil {
				t.Fatalf("sync.Open failed on repo with %s: %v", tc.name, err)
			}
			if syncClient.RepoDir() != repoDir {
				t.Fatalf("syncClient.RepoDir = %s, want %s", syncClient.RepoDir(), repoDir)
			}
		})
	}
}

func TestOpenSparseCheckoutRepository(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	// Run git sparse-checkout init (sets core.repositoryformatversion 0 and extensions.worktreeConfig true)
	runGitCmd(t, repoDir, "sparse-checkout", "init")

	ctx := context.Background()
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("writ.Open failed on sparse-checkout repo: %v", err)
	}
	defer s.Close()

	issueID, err := s.Issues.Create(ctx, writ.NewIssue{
		Title: "Sparse checkout test issue",
	})
	if err != nil {
		t.Fatalf("Issues.Create failed: %v", err)
	}
	if issueID == "" {
		t.Fatal("expected non-empty issue ID")
	}

	issues, err := s.Query.Issues(writ.IssueFilter{})
	if err != nil {
		t.Fatalf("Query.Issues failed: %v", err)
	}
	if len(issues) != 1 || issues[0].ObjectID != issueID {
		t.Fatalf("unexpected issues query result: %+v", issues)
	}
}
