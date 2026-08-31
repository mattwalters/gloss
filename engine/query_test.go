package writ_test

import (
	"context"
	"errors"
	"testing"

	"github.com/writtendev/writ/engine"
)

func TestQueryFullSuite(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create multiple reviews
	r1, err := s.Reviews.Create(ctx, writ.NewReview{
		Title:       "First Feature Review",
		Description: "Alpha feature",
	})
	if err != nil {
		t.Fatalf("Create r1: %v", err)
	}

	r2, err := s.Reviews.Create(ctx, writ.NewReview{
		Title:       "Second Bugfix Review",
		Description: "Beta bugfix",
	})
	if err != nil {
		t.Fatalf("Create r2: %v", err)
	}

	if err := s.Reviews.SetStatus(ctx, r2, writ.ReviewStatus{Status: "closed", Reason: "superseded"}); err != nil {
		t.Fatalf("SetStatus r2: %v", err)
	}

	// 2. Create multiple issues
	i1, err := s.Issues.Create(ctx, writ.NewIssue{
		Title:       "Issue One",
		Description: "Important issue",
	})
	if err != nil {
		t.Fatalf("Create i1: %v", err)
	}
	if err := s.Issues.Assign(ctx, i1, []string{"alice"}, nil); err != nil {
		t.Fatalf("Assign i1: %v", err)
	}
	if err := s.Issues.Label(ctx, i1, []string{"frontend"}, nil); err != nil {
		t.Fatalf("Label i1: %v", err)
	}

	i2, err := s.Issues.Create(ctx, writ.NewIssue{
		Title:       "Issue Two",
		Description: "Backend issue",
	})
	if err != nil {
		t.Fatalf("Create i2: %v", err)
	}
	if err := s.Issues.SetState(ctx, i2, writ.IssueState{State: "closed", Reason: "fixed"}); err != nil {
		t.Fatalf("SetState i2: %v", err)
	}
	if err := s.Issues.Assign(ctx, i2, []string{"bob"}, nil); err != nil {
		t.Fatalf("Assign i2: %v", err)
	}

	// 3. Comments on r1
	c1, err := s.Reviews.Comment(ctx, r1, writ.NewComment{Text: "Comment 1 on r1"})
	if err != nil {
		t.Fatalf("Comment r1: %v", err)
	}
	c2, err := s.Reviews.Comment(ctx, r1, writ.NewComment{Text: "Reply to comment 1", InReplyTo: c1})
	if err != nil {
		t.Fatalf("Reply r1: %v", err)
	}

	// Edit c1
	if err := s.Comments.Edit(ctx, c1, "Edited Comment 1 on r1"); err != nil {
		t.Fatalf("Edit c1: %v", err)
	}

	// Test Query.Reviews
	closedReviews, err := s.Query.Reviews(writ.ReviewFilter{Status: []string{"closed"}})
	if err != nil {
		t.Fatalf("Query.Reviews closed: %v", err)
	}
	if len(closedReviews) != 1 || closedReviews[0].ObjectID != r2 {
		t.Errorf("expected closed review r2, got %+v", closedReviews)
	}

	allReviews, err := s.Query.Reviews(writ.ReviewFilter{OrderBy: writ.OrderByCreatedAtAsc})
	if err != nil {
		t.Fatalf("Query.Reviews all: %v", err)
	}
	if len(allReviews) != 2 {
		t.Errorf("expected 2 reviews, got %d", len(allReviews))
	}

	// Test Query.Review point lookup
	resR1, err := s.Query.Review(r1)
	if err != nil {
		t.Fatalf("Query.Review(r1): %v", err)
	}
	if resR1.Review.Title != "First Feature Review" {
		t.Errorf("got title %q", resR1.Review.Title)
	}

	// Test Query.Review not found
	_, err = s.Query.Review("non-existent-id")
	if !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing review, got: %v", err)
	}

	// Test Query.Issues
	openIssues, err := s.Query.Issues(writ.IssueFilter{State: []string{"open"}})
	if err != nil {
		t.Fatalf("Query.Issues open: %v", err)
	}
	if len(openIssues) != 1 || openIssues[0].ObjectID != i1 {
		t.Errorf("expected issue i1, got %+v", openIssues)
	}

	// Test Query.Issue point lookup
	resI2, err := s.Query.Issue(i2)
	if err != nil {
		t.Fatalf("Query.Issue(i2): %v", err)
	}
	if resI2.Issue.State != "closed" {
		t.Errorf("got state %q, want 'closed'", resI2.Issue.State)
	}

	// Test Query.Issue not found
	_, err = s.Query.Issue("non-existent-id")
	if !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for missing issue, got: %v", err)
	}

	// Test Query.Comments
	comments, err := s.Query.Comments(writ.CommentFilter{SubjectID: r1})
	if err != nil {
		t.Fatalf("Query.Comments: %v", err)
	}
	if len(comments) != 2 {
		t.Errorf("expected 2 comments on r1, got %d", len(comments))
	}

	// Test Query.Threads
	threads, err := s.Query.Threads("review", r1)
	if err != nil {
		t.Fatalf("Query.Threads: %v", err)
	}
	if len(threads) != 1 || len(threads[0].Replies) != 1 || threads[0].Replies[0].ObjectID != c2 {
		t.Errorf("unexpected thread tree: %+v", threads)
	}

	// Test Query.Objects
	objects, err := s.Query.Objects(writ.ObjectFilter{})
	if err != nil {
		t.Fatalf("Query.Objects: %v", err)
	}
	if len(objects) != 6 { // 2 reviews + 2 issues + 2 comments
		t.Errorf("expected 6 objects total, got %d", len(objects))
	}

	// Test Query.GroupIssues
	groups, err := s.Query.GroupIssues(writ.GroupByState, writ.IssueFilter{})
	if err != nil {
		t.Fatalf("Query.GroupIssues: %v", err)
	}
	if len(groups) != 2 {
		t.Errorf("expected 2 groups (closed, open), got %d", len(groups))
	}

	assigneeGroups, err := s.Query.GroupIssues(writ.GroupByAssignee, writ.IssueFilter{})
	if err != nil {
		t.Fatalf("Query.GroupIssues by assignee: %v", err)
	}
	if len(assigneeGroups) != 2 {
		t.Errorf("expected 2 assignee groups (alice, bob), got %d", len(assigneeGroups))
	}
}

func TestWithoutAutoRefresh(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir,
		writ.WithSigner(dummySigner()),
		writ.WithoutAutoRefresh(),
	)
	if err != nil {
		t.Fatalf("Open WithoutAutoRefresh failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Write without auto-refresh
	id, err := s.Reviews.Create(ctx, writ.NewReview{Title: "Manual Refresh Review"})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Without refresh, projection has not folded the new review yet
	_, err = s.Query.Review(id)
	if !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound before manual refresh, got: %v", err)
	}

	// Explicit refresh
	stats, err := s.Refresh(ctx)
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if stats.ObjectsTouched != 1 {
		t.Errorf("expected 1 object touched, got %d", stats.ObjectsTouched)
	}

	// Now Query.Review finds it
	res, err := s.Query.Review(id)
	if err != nil {
		t.Fatalf("Query.Review after manual refresh failed: %v", err)
	}
	if res.Review.Title != "Manual Refresh Review" {
		t.Errorf("got title %q", res.Review.Title)
	}
}
