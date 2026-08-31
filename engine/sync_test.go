package writ_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/writtendev/writ/engine"
)

func setupSyncHarness(t *testing.T) (bareDir, aliceDir, bobDir string) {
	t.Helper()
	tempDir := t.TempDir()

	// 1. Bare remote
	bareDir = filepath.Join(tempDir, "remote.git")
	runGitCmd(t, tempDir, "init", "--bare", bareDir)

	// 2. Alice's clone
	aliceDir = filepath.Join(tempDir, "alice")
	runGitCmd(t, tempDir, "init", aliceDir)
	runGitCmd(t, aliceDir, "config", "user.name", "Alice")
	runGitCmd(t, aliceDir, "config", "user.email", "alice@example.com")
	runGitCmd(t, aliceDir, "config", "writ.writerId", "0123456789abcdef")
	runGitCmd(t, aliceDir, "config", "gpg.format", "ssh")
	runGitCmd(t, aliceDir, "config", "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGalice")
	runGitCmd(t, aliceDir, "remote", "add", "origin", bareDir)

	// Commit dummy file so HEAD exists and push main branch
	dummyFile := filepath.Join(aliceDir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# Project\n"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	runGitCmd(t, aliceDir, "add", "README.md")
	runGitCmd(t, aliceDir, "commit", "-m", "initial commit")
	runGitCmd(t, aliceDir, "push", "origin", "HEAD:main")

	// 3. Bob's clone
	bobDir = filepath.Join(tempDir, "bob")
	runGitCmd(t, tempDir, "clone", bareDir, bobDir)
	runGitCmd(t, bobDir, "config", "user.name", "Bob")
	runGitCmd(t, bobDir, "config", "user.email", "bob@example.com")
	runGitCmd(t, bobDir, "config", "writ.writerId", "fedcba9876543210")
	runGitCmd(t, bobDir, "config", "gpg.format", "ssh")
	runGitCmd(t, bobDir, "config", "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGbob")

	return bareDir, aliceDir, bobDir
}

func TestStoreSyncLifecycle(t *testing.T) {
	_, aliceDir, bobDir := setupSyncHarness(t)
	ctx := context.Background()

	// 1. Open Store A (Alice)
	sA, err := writ.Open(aliceDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open Alice failed: %v", err)
	}
	defer sA.Close()

	// Initial status
	statusA, err := sA.SyncStatus(ctx, "origin")
	if err != nil {
		t.Fatalf("Alice SyncStatus before write: %v", err)
	}
	if statusA.Unsynced != 0 {
		t.Errorf("Alice expected 0 unsynced before write, got %d", statusA.Unsynced)
	}

	// Alice creates a review
	reviewID, err := sA.Reviews.Create(ctx, writ.NewReview{
		Title: "Sync Feature Review",
	})
	if err != nil {
		t.Fatalf("Alice create review: %v", err)
	}

	// Status now shows 1 unsynced op
	statusA, err = sA.SyncStatus(ctx, "origin")
	if err != nil {
		t.Fatalf("Alice SyncStatus after write: %v", err)
	}
	if statusA.Unsynced != 1 {
		t.Errorf("Alice expected 1 unsynced after write, got %d", statusA.Unsynced)
	}

	// Alice syncs with origin
	syncResA, err := sA.Sync(ctx, "origin")
	if err != nil {
		t.Fatalf("Alice Sync failed: %v", err)
	}
	if syncResA.OpsPushed != 1 {
		t.Errorf("Alice expected 1 op pushed, got %d", syncResA.OpsPushed)
	}
	if syncResA.Unsynced != 0 {
		t.Errorf("Alice expected 0 unsynced after sync, got %d", syncResA.Unsynced)
	}

	// 2. Open Store B (Bob)
	sB, err := writ.Open(bobDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open Bob failed: %v", err)
	}
	defer sB.Close()

	// Bob syncs with origin and receives Alice's review
	syncResB, err := sB.Sync(ctx, "origin")
	if err != nil {
		t.Fatalf("Bob Sync failed: %v", err)
	}
	if syncResB.OpsFetched != 1 {
		t.Errorf("Bob expected 1 op fetched, got %d", syncResB.OpsFetched)
	}
	if syncResB.ObjectsTouched != 1 {
		t.Errorf("Bob expected 1 object touched, got %d", syncResB.ObjectsTouched)
	}

	// Bob queries and approves Alice's review
	resB, err := sB.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Bob Query.Review failed: %v", err)
	}
	if resB.Review.Title != "Sync Feature Review" {
		t.Errorf("Bob got title %q", resB.Review.Title)
	}

	// Push a revision and approve
	headHash := runGitCmd(t, bobDir, "rev-parse", "HEAD")[:40]
	if err := sB.Reviews.PushRevision(ctx, reviewID, headHash, headHash); err != nil {
		t.Fatalf("Bob PushRevision failed: %v", err)
	}
	if err := sB.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict: "approve",
		Message: "Looks great from Bob!",
	}); err != nil {
		t.Fatalf("Bob Approve failed: %v", err)
	}

	// Bob pushes approval to origin
	syncResB2, err := sB.Sync(ctx, "origin")
	if err != nil {
		t.Fatalf("Bob Sync 2 failed: %v", err)
	}
	if syncResB2.OpsPushed != 2 { // revision + approval
		t.Errorf("Bob expected 2 ops pushed, got %d", syncResB2.OpsPushed)
	}

	// 3. Alice syncs and observes Bob's approval
	syncResA2, err := sA.Sync(ctx, "origin")
	if err != nil {
		t.Fatalf("Alice Sync 2 failed: %v", err)
	}
	if syncResA2.OpsFetched != 2 {
		t.Errorf("Alice expected 2 ops fetched, got %d", syncResA2.OpsFetched)
	}

	resA2, err := sA.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Alice Query.Review after fetch failed: %v", err)
	}
	if len(resA2.Review.Approvals) != 1 || resA2.Review.Approvals[0].Verdict != "approve" {
		t.Errorf("Alice unexpected approvals: %+v", resA2.Review.Approvals)
	}
}
