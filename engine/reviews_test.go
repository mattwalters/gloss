package writ_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/state"
)

func TestReviewsLifecycleAndFoldAgreement(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	headHash := runGitCmd(t, repoDir, "rev-parse", "HEAD")
	headHash = headHash[:40]

	// Create a second commit for base/head
	dummyFile2 := filepath.Join(repoDir, "feature.go")
	if err := os.WriteFile(dummyFile2, []byte("package feature\n"), 0o644); err != nil {
		t.Fatalf("write feature file: %v", err)
	}
	runGitCmd(t, repoDir, "add", "feature.go")
	runGitCmd(t, repoDir, "commit", "-m", "feature commit")
	baseHash := headHash
	headHash = runGitCmd(t, repoDir, "rev-parse", "HEAD")[:40]

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create Review
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title:       "Add OAuth2 Provider",
		Description: "Implements Google and GitHub login",
		Base:        baseHash,
		Head:        headHash,
	})
	if err != nil {
		t.Fatalf("Reviews.Create failed: %v", err)
	}
	if reviewID == "" {
		t.Fatal("expected non-empty review ID")
	}

	// 2. Update metadata
	newTitle := "Add OAuth2 Provider (Google & GitHub)"
	if err := s.Reviews.Update(ctx, reviewID, writ.ReviewEdit{Title: &newTitle}); err != nil {
		t.Fatalf("Reviews.Update failed: %v", err)
	}

	// 3. Push a revision
	revBase := baseHash
	revHead := headHash
	if err := s.Reviews.PushRevision(ctx, reviewID, revBase, revHead); err != nil {
		t.Fatalf("Reviews.PushRevision failed: %v", err)
	}

	// 4. Add a comment
	commentID, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
		Text: "Looks solid overall.",
	})
	if err != nil {
		t.Fatalf("Reviews.Comment failed: %v", err)
	}
	if commentID == "" {
		t.Fatal("expected non-empty comment ID")
	}

	// 5. Reply to the comment
	replyID, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
		Text:      "Thanks, addressed in latest revision.",
		InReplyTo: commentID,
	})
	if err != nil {
		t.Fatalf("Reviews.Comment reply failed: %v", err)
	}
	if replyID == "" {
		t.Fatal("expected non-empty reply comment ID")
	}

	// 6. Approve review
	if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict: "approve",
		Message: "LGTM!",
	}); err != nil {
		t.Fatalf("Reviews.Approve failed: %v", err)
	}

	// 7. Set status to merged
	if err := s.Reviews.SetStatus(ctx, reviewID, writ.ReviewStatus{
		Status:      "merged",
		MergeCommit: headHash,
		Reason:      "Pull request merged cleanly",
	}); err != nil {
		t.Fatalf("Reviews.SetStatus failed: %v", err)
	}

	// Assert Query agreements with fold
	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}

	if res.Review.Title != newTitle {
		t.Errorf("got Title %q, want %q", res.Review.Title, newTitle)
	}
	if res.Review.Status != "merged" {
		t.Errorf("got Status %q, want 'merged'", res.Review.Status)
	}
	if len(res.Review.Revisions) != 2 {
		t.Errorf("got %d revisions, want 2", len(res.Review.Revisions))
	}
	if len(res.Review.Approvals) != 1 || res.Review.Approvals[0].Verdict != "approve" {
		t.Errorf("unexpected approvals: %+v", res.Review.Approvals)
	}

	// Verify agreement between Query.Review and state.FoldReview over dag.Enumerate
	ident, _ := identity.ParseWriterID("0123456789abcdef")
	dagStore, err := dag.Open(repoDir, identity.Identity{WriterID: ident})
	if err != nil {
		t.Fatalf("dag.Open failed: %v", err)
	}
	enumRes, err := dagStore.Enumerate()
	if err != nil {
		t.Fatalf("dagStore.Enumerate failed: %v", err)
	}
	foldedReview, err := state.FoldReview(enumRes.Ops[reviewID])
	if err != nil {
		t.Fatalf("FoldReview failed: %v", err)
	}

	if !reflect.DeepEqual(res.Review, foldedReview) {
		t.Errorf("Query.Review and FoldReview mismatch:\n Query:  %+v\n Folded: %+v", res.Review, foldedReview)
	}

	// Verify threads query
	threads, err := s.Query.Threads("review", reviewID)
	if err != nil {
		t.Fatalf("Query.Threads failed: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 root thread, got %d", len(threads))
	}
	if threads[0].ObjectID != commentID {
		t.Errorf("root comment ID got %q, want %q", threads[0].ObjectID, commentID)
	}
	if len(threads[0].Replies) != 1 || threads[0].Replies[0].ObjectID != replyID {
		t.Errorf("reply comment ID got %+v, want %q", threads[0].Replies, replyID)
	}
}

func TestReviewsWritePathEnvelopes(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title:       "Spec Conformance Review",
		Description: "Checks envelope fields",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}

	ref, err := repo.Reference(plumbing.ReferenceName("refs/writ/0123456789abcdef/review"), true)
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

	if payload["object_id"] != reviewID {
		t.Errorf("got object_id %v, want %v", payload["object_id"], reviewID)
	}
	if payload["object_type"] != "review" {
		t.Errorf("got object_type %v, want 'review'", payload["object_type"])
	}
	if payload["op_type"] != "create" {
		t.Errorf("got op_type %v, want 'create'", payload["op_type"])
	}
	if payload["op_version"] != float64(1) {
		t.Errorf("got op_version %v, want 1", payload["op_version"])
	}
	body, ok := payload["body"].(map[string]any)
	if !ok || body["title"] != "Spec Conformance Review" {
		t.Errorf("unexpected body in op.json: %+v", body)
	}
}

func TestMultiWriterCausalParents(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	cacheDir := t.TempDir()

	// Store A (Writer A)
	sA, err := writ.Open(repoDir,
		writ.WithSigner(dummySigner()),
		writ.WithCacheDir(cacheDir),
	)
	if err != nil {
		t.Fatalf("Open Store A failed: %v", err)
	}
	defer sA.Close()

	ctx := context.Background()
	reviewID, err := sA.Reviews.Create(ctx, writ.NewReview{
		Title: "Multi-Writer Review",
	})
	if err != nil {
		t.Fatalf("Store A create review failed: %v", err)
	}

	// Find the create op commit hash
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	refA, err := repo.Reference(plumbing.ReferenceName("refs/writ/0123456789abcdef/review"), true)
	if err != nil {
		t.Fatalf("Reference A lookup: %v", err)
	}
	createOpHash := refA.Hash().String()

	// Store B (Writer B) with different writer ID
	runGitCmd(t, repoDir, "config", "writ.writerId", "fedcba9876543210")
	runGitCmd(t, repoDir, "config", "user.name", "Bob Reviewer")
	runGitCmd(t, repoDir, "config", "user.email", "bob@example.com")

	sB, err := writ.Open(repoDir,
		writ.WithSigner(dummySigner()),
		writ.WithCacheDir(cacheDir),
	)
	if err != nil {
		t.Fatalf("Open Store B failed: %v", err)
	}
	defer sB.Close()

	// Refresh B's view and add comment
	commentID, err := sB.Reviews.Comment(ctx, reviewID, writ.NewComment{
		Text: "Bob's review comment",
	})
	if err != nil {
		t.Fatalf("Store B comment failed: %v", err)
	}
	if commentID == "" {
		t.Fatal("expected non-empty comment ID")
	}

	// Verify B's comment commit parents
	refB, err := repo.Reference(plumbing.ReferenceName("refs/writ/fedcba9876543210/comment"), true)
	if err != nil {
		t.Fatalf("Reference B comment lookup: %v", err)
	}

	commitB, err := repo.CommitObject(refB.Hash())
	if err != nil {
		t.Fatalf("CommitObject B: %v", err)
	}

	// Since this is B's first comment on this chain, parents contains A's create op hash
	foundCausalParent := false
	for _, parentHash := range commitB.ParentHashes {
		if parentHash.String() == createOpHash {
			foundCausalParent = true
			break
		}
	}
	if !foundCausalParent {
		t.Fatalf("B's comment commit %s did not include A's create op %s as a parent! ParentHashes: %+v", commitB.Hash, createOpHash, commitB.ParentHashes)
	}
}

func TestReviewsValidationAndNotFound(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Invalid Base without Head fails before writing create op
	_, err = s.Reviews.Create(ctx, writ.NewReview{
		Title: "Invalid Rev",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("expected error creating review with Base but no Head")
	}

	// Verify no ops were written to review chain
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	if _, err := repo.Reference(plumbing.ReferenceName("refs/writ/0123456789abcdef/review"), true); err == nil {
		t.Fatal("expected no review ref created on validation failure")
	}

	// Non-existent review mutations return ErrNotFound
	missingID := "00000000000000000000000000000000"
	newTitle := "New Title"

	if err := s.Reviews.Update(ctx, missingID, writ.ReviewEdit{Title: &newTitle}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Update on missing review, got %v", err)
	}
	if err := s.Reviews.PushRevision(ctx, missingID, "base", "head"); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for PushRevision on missing review, got %v", err)
	}
	if err := s.Reviews.SetStatus(ctx, missingID, writ.ReviewStatus{Status: "closed"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for SetStatus on missing review, got %v", err)
	}
	if _, err := s.Reviews.Comment(ctx, missingID, writ.NewComment{Text: "text"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Comment on missing review, got %v", err)
	}
	if err := s.Reviews.Approve(ctx, missingID, writ.Approval{Verdict: "approve"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Approve on missing review, got %v", err)
	}

	// Missing comment mutation
	if err := s.Comments.Edit(ctx, missingID, "text"); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Edit on missing comment, got %v", err)
	}
	if err := s.Comments.Delete(ctx, missingID); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Delete on missing comment, got %v", err)
	}
	if err := s.Comments.Resolve(ctx, missingID, writ.CommentResolve{Resolved: true}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Resolve on missing comment, got %v", err)
	}
}

