package writ_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/resolve"
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
	missingBase := strings.Repeat("a", 40)
	missingHead := strings.Repeat("b", 40)
	if err := s.Reviews.PushRevision(ctx, missingID, missingBase, missingHead); !errors.Is(err, writ.ErrNotFound) {
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
	if err := s.Reviews.Assign(ctx, missingID, []string{"user:alice"}, nil); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Assign on missing review, got %v", err)
	}
	if err := s.Reviews.Label(ctx, missingID, []string{"bug"}, nil); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Label on missing review, got %v", err)
	}
	if err := s.Reviews.Link(ctx, missingID, writ.Link{Target: "iss-1", Relation: "fixes"}); !errors.Is(err, writ.ErrNotFound) {
		t.Errorf("expected ErrNotFound for Link on missing review, got %v", err)
	}
}

func TestReviewsAssign(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create review
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Assignee Test Review",
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	// 2. Assign alice and bob, one under each scheme
	if err := s.Reviews.Assign(ctx, reviewID, []string{"user:alice", "email:bob@example.com"}, nil); err != nil {
		t.Fatalf("Assign failed: %v", err)
	}

	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if !reflect.DeepEqual(res.Review.Assignees, []string{"email:bob@example.com", "user:alice"}) {
		t.Fatalf("expected assignees [email:bob@example.com, user:alice], got %v", res.Review.Assignees)
	}

	// 3. Remove bob, add charlie
	if err := s.Reviews.Assign(ctx, reviewID, []string{"user:charlie"}, []string{"email:bob@example.com"}); err != nil {
		t.Fatalf("Assign update failed: %v", err)
	}

	res, err = s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if !reflect.DeepEqual(res.Review.Assignees, []string{"user:alice", "user:charlie"}) {
		t.Fatalf("expected assignees [user:alice, user:charlie], got %v", res.Review.Assignees)
	}

	// 4. Query reviews with Assignee filter
	aliceReviews, err := s.Query.Reviews(writ.ReviewFilter{
		Assignee: []string{"user:alice"},
	})
	if err != nil {
		t.Fatalf("Query.Reviews with assignee filter failed: %v", err)
	}
	if len(aliceReviews) != 1 || aliceReviews[0].ObjectID != reviewID {
		t.Fatalf("expected reviewID in alice reviews, got %v", aliceReviews)
	}

	bobReviews, err := s.Query.Reviews(writ.ReviewFilter{
		Assignee: []string{"email:bob@example.com"},
	})
	if err != nil {
		t.Fatalf("Query.Reviews with bob filter failed: %v", err)
	}
	if len(bobReviews) != 0 {
		t.Fatalf("expected 0 reviews for removed assignee bob, got %v", bobReviews)
	}
}

func TestReviewsLabel(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create review
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Label Test Review",
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	// 2. Add labels
	if err := s.Reviews.Label(ctx, reviewID, []string{"area/engine", "wip"}, nil); err != nil {
		t.Fatalf("Label failed: %v", err)
	}

	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if !reflect.DeepEqual(res.Review.Labels, []string{"area/engine", "wip"}) {
		t.Fatalf("expected labels [area/engine, wip], got %v", res.Review.Labels)
	}

	// 3. Remove wip, add needs-docs
	if err := s.Reviews.Label(ctx, reviewID, []string{"needs-docs"}, []string{"wip"}); err != nil {
		t.Fatalf("Label update failed: %v", err)
	}

	res, err = s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if !reflect.DeepEqual(res.Review.Labels, []string{"area/engine", "needs-docs"}) {
		t.Fatalf("expected labels [area/engine, needs-docs], got %v", res.Review.Labels)
	}

	// 4. Query reviews with Label filter
	matched, err := s.Query.Reviews(writ.ReviewFilter{
		Label: []string{"area/engine"},
	})
	if err != nil {
		t.Fatalf("Query.Reviews with label filter failed: %v", err)
	}
	if len(matched) != 1 || matched[0].ObjectID != reviewID {
		t.Fatalf("expected reviewID in matched reviews, got %v", matched)
	}

	unmatched, err := s.Query.Reviews(writ.ReviewFilter{
		Label: []string{"wip"},
	})
	if err != nil {
		t.Fatalf("Query.Reviews with wip filter failed: %v", err)
	}
	if len(unmatched) != 0 {
		t.Fatalf("expected 0 reviews for removed label wip, got %v", unmatched)
	}
}

func TestReviewsLink(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// 1. Create review
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Link Test Review",
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	// 2. Link to issue (fixes)
	targetID := "0123456789abcdef0123456789abcdef"
	if err := s.Reviews.Link(ctx, reviewID, writ.Link{
		Target:     targetID,
		TargetType: "issue",
		Relation:   "fixes",
	}); err != nil {
		t.Fatalf("Link failed: %v", err)
	}

	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if len(res.Review.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(res.Review.Links))
	}
	if res.Review.Links[0].Target != targetID || res.Review.Links[0].Relation != "fixes" {
		t.Fatalf("unexpected link: %+v", res.Review.Links[0])
	}

	// 3. Retract link
	if err := s.Reviews.Link(ctx, reviewID, writ.Link{
		Target:   targetID,
		Relation: "none",
	}); err != nil {
		t.Fatalf("Link retract failed: %v", err)
	}

	res, err = s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if len(res.Review.Links) != 0 {
		t.Fatalf("expected 0 links after retraction, got %v", res.Review.Links)
	}
}

func TestReviewsApproveSubjectNormalization(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	headHash := runGitCmd(t, repoDir, "rev-parse", "HEAD")[:40]

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Approval Subject Review",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	// A whitespace-only subject normalizes away entirely: the approval must be
	// anonymous rather than carry a schema-invalid empty subject.
	if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
		Subject: "   ",
		Verdict: "approve",
	}); err != nil {
		t.Fatalf("Approve with whitespace-only subject failed: %v", err)
	}

	// A padded subject is normalized before it is written.
	if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
		Subject: "  User:Alice  ",
		Verdict: "request-changes",
	}); err != nil {
		t.Fatalf("Approve with padded subject failed: %v", err)
	}

	ident, _ := identity.ParseWriterID("0123456789abcdef")
	dagStore, err := dag.Open(repoDir, identity.Identity{WriterID: ident})
	if err != nil {
		t.Fatalf("dag.Open failed: %v", err)
	}
	enumRes, err := dagStore.Enumerate()
	if err != nil {
		t.Fatalf("dagStore.Enumerate failed: %v", err)
	}

	var omitted, named int
	for _, op := range enumRes.Ops[reviewID] {
		if op.OpType != "approval" {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(op.Body, &body); err != nil {
			t.Fatalf("unmarshal approval body: %v", err)
		}
		subject, ok := body["subject"]
		switch {
		case !ok:
			omitted++
		case subject == "user:alice":
			named++
		default:
			t.Errorf("unexpected approval subject %v in body %+v", subject, body)
		}
	}
	if omitted != 1 || named != 1 {
		t.Errorf("got %d approvals without subject and %d for alice, want 1 and 1", omitted, named)
	}
}

// TestReviewsPersonIDLengthBound checks that the person-id length bound
// (spec/schemas/identifiers.schema.json, maxLength: 320) is enforced on every
// review write path that accepts a person identifier, not just on comment
// resolution. Over-length input is refused rather than truncated.
func TestReviewsPersonIDLengthBound(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	headHash := runGitCmd(t, repoDir, "rev-parse", "HEAD")[:40]

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Person ID Bound Review",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	atLimit := personIDAtLimit(t)
	overLimit := personIDOverLimit(t)

	// Assign: at the bound is accepted, one over is refused.
	if err := s.Reviews.Assign(ctx, reviewID, []string{atLimit}, nil); err != nil {
		t.Fatalf("Assign with a 320-character assignee failed: %v", err)
	}
	err = s.Reviews.Assign(ctx, reviewID, []string{overLimit}, nil)
	if err == nil {
		t.Fatal("expected Assign to reject a 321-character assignee, got nil error")
	}
	if !strings.Contains(err.Error(), "320") {
		t.Errorf("expected an error naming the 320-character limit, got %q", err)
	}

	// An over-length identifier on the remove side is refused too.
	if err := s.Reviews.Assign(ctx, reviewID, nil, []string{overLimit}); err == nil {
		t.Error("expected Assign to reject a 321-character removal, got nil error")
	}

	// Approval subject: same bound, same refusal.
	if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict: "approve",
		Subject: atLimit,
	}); err != nil {
		t.Fatalf("Approve with a 320-character subject failed: %v", err)
	}
	err = s.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict: "approve",
		Subject: overLimit,
	})
	if err == nil {
		t.Fatal("expected Approve to reject a 321-character subject, got nil error")
	}
	if !strings.Contains(err.Error(), "subject") || !strings.Contains(err.Error(), "320") {
		t.Errorf("expected an error naming the subject and the 320-character limit, got %q", err)
	}

	// The accepted assignee round-tripped unchanged — no truncation.
	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if !reflect.DeepEqual(res.Review.Assignees, []string{atLimit}) {
		t.Errorf("expected the 320-character assignee to round-trip unchanged, got %v", res.Review.Assignees)
	}
}

// TestReviewsApproveRejectsUnknownVerdict pins the half of WRIT-129 that the
// domain layer does not guard. Approve passes Verdict into the body unchecked;
// the codec's producer-side body validation is what stops a verdict outside the
// enum from being signed into the log.
func TestReviewsApproveRejectsUnknownVerdict(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	headHash := runGitCmd(t, repoDir, "rev-parse", "HEAD")[:40]

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Verdict Enum Review",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{Verdict: "bogus"}); err == nil {
		t.Fatal("expected Approve to reject a verdict outside the enum, got nil error")
	}

	// Nothing was written: the review still has no approvals.
	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if len(res.Review.Approvals) != 0 {
		t.Errorf("expected no approvals after the rejected verdict, got %v", res.Review.Approvals)
	}

	// Every verdict the spec defines is still accepted.
	for _, verdict := range []string{"approve", "request-changes", "none"} {
		if err := s.Reviews.Approve(ctx, reviewID, writ.Approval{Verdict: verdict}); err != nil {
			t.Errorf("Approve with verdict %q failed: %v", verdict, err)
		}
	}
}

// TestReviewsCreateWritesNothingWhenTheRevisionIsRefused pins the all-or-
// nothing contract on the one public method that appends twice. Before the
// domain guard, a ref name in Base got past Create's argument checks, the
// create op was appended and signed, and only then did the producer refuse the
// revision body. The caller saw an error and an empty id while the log kept a
// review forever that nothing could address, retry or withdraw.
func TestReviewsCreateWritesNothingWhenTheRevisionIsRefused(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	id, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Ref names are not OIDs",
		Base:  "main",
		Head:  "feature/oauth2",
	})
	if err == nil {
		t.Fatal("expected Create to refuse a ref name in Base, got nil error")
	}
	if id != "" {
		t.Errorf("expected an empty id on failure, got %q", id)
	}
	// The error names the field and the value, not a JSON Pointer.
	if !strings.Contains(err.Error(), "base must be a commit OID") || !strings.Contains(err.Error(), `"main"`) {
		t.Errorf("expected a legible domain error naming base and its value, got %v", err)
	}

	reviews, err := s.Query.Reviews(writ.ReviewFilter{})
	if err != nil {
		t.Fatalf("Query.Reviews failed: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("Create left %d review(s) in the log after refusing: %+v", len(reviews), reviews)
	}

	// The same call with resolved OIDs still works, and writes both ops.
	headHash := strings.TrimSpace(runGitCmd(t, repoDir, "rev-parse", "HEAD"))
	id, err = s.Reviews.Create(ctx, writ.NewReview{
		Title: "Resolved first",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create with OIDs failed: %v", err)
	}
	res, err := s.Query.Review(id)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if len(res.Review.Revisions) != 1 {
		t.Errorf("expected the initial revision to be appended, got %d", len(res.Review.Revisions))
	}
}

// TestReviewsCommentAnchorsAtFileEdges pins the nil-collar case through the
// public API. writ.Anchor is a public type and Context.Before/After have no
// omitempty, so a nil slice marshals to null, which anchor.schema.json rejects
// as not an array. A comment on the first line of a file has no before-collar
// and a comment on the last line has no after-collar: both are ordinary, and
// both must still be writable now that the producer judges the body.
func TestReviewsCommentAnchorsAtFileEdges(t *testing.T) {
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
		Title: "Anchored comments",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	sideAt := func(start, end int, before, after []string) *writ.Anchor {
		return &writ.Anchor{
			Version: 1,
			New: &resolve.SideAnchor{
				Commit: headHash,
				Path:   "README.md",
				Blob:   blobHash,
				Range:  &resolve.Range{Start: start, End: end},
				Context: &resolve.Context{
					Before: before,
					Lines:  []string{"# Test"},
					After:  after,
				},
			},
		}
	}

	cases := []struct {
		name   string
		anchor *writ.Anchor
	}{
		// A comment on line 1: nothing precedes it, so Before is the zero value.
		{"first line, nil before", sideAt(1, 1, nil, []string{"next line"})},
		// A comment on the last line: nothing follows it.
		{"last line, nil after", sideAt(1, 1, []string{"previous line"}, nil)},
		// A one-line file: neither collar exists.
		{"only line, both nil", sideAt(1, 1, nil, nil)},
		// The populated case, which worked before and must keep working.
		{"both collars", sideAt(1, 1, []string{"previous line"}, []string{"next line"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commentID, err := s.Reviews.Comment(ctx, reviewID, writ.NewComment{
				Text:   "anchored",
				Anchor: tc.anchor,
			})
			if err != nil {
				t.Fatalf("Comment with an edge-of-file anchor was refused: %v", err)
			}
			if commentID == "" {
				t.Fatal("expected a comment id")
			}
		})
	}
}

// TestReviewsUpdateRejectsEmptyTitle records the ruling on clearing a title: a
// review has one for its whole life (create requires it, update_body pins
// minLength 1), so an empty title is a legible rejection rather than a
// supported clear. Description, which has no minLength, still clears.
func TestReviewsUpdateRejectsEmptyTitle(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	headHash := strings.TrimSpace(runGitCmd(t, repoDir, "rev-parse", "HEAD"))

	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	id, err := s.Reviews.Create(ctx, writ.NewReview{
		Title: "Has a title",
		Base:  headHash,
		Head:  headHash,
	})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	empty := ""
	err = s.Reviews.Update(ctx, id, writ.ReviewEdit{Title: &empty})
	if err == nil {
		t.Fatal("expected Update to refuse an empty title, got nil error")
	}
	if !strings.Contains(err.Error(), "review title cannot be empty") {
		t.Errorf("expected a legible domain error, got %v", err)
	}

	if err := s.Reviews.Update(ctx, id, writ.ReviewEdit{Description: &empty}); err != nil {
		t.Errorf("clearing the description must still work: %v", err)
	}

	res, err := s.Query.Review(id)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if res.Review.Title != "Has a title" {
		t.Errorf("title changed after the refused update: %q", res.Review.Title)
	}
}
