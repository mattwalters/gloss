package writ_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
)

func setupTestRepoWithID(t *testing.T, name, email string) (string, identity.RepoID) {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", name)
	runGitCmd(t, dir, "config", "user.email", email)
	runGitCmd(t, dir, "config", "gpg.format", "ssh")
	runGitCmd(t, dir, "config", "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGdummy")

	// Mint and ensure writer ID
	_, _, err := identity.EnsureWriterID(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("ensure writer ID: %v", err)
	}

	// Mint and ensure repo ID
	repoID, _, err := identity.EnsureRepoID(context.Background(), dir)
	if err != nil {
		t.Fatalf("ensure repo ID: %v", err)
	}

	// Commit initial file
	dummyFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	runGitCmd(t, dir, "add", "README.md")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	return dir, repoID
}

func TestWorkspaceExistingCallersUnaffected(t *testing.T) {
	// A repository with no writ.workspace and no writ.repo-id
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.name", "Alice Test")
	runGitCmd(t, dir, "config", "user.email", "alice@example.com")
	runGitCmd(t, dir, "config", "writ.writerId", "0123456789abcdef")
	runGitCmd(t, dir, "config", "gpg.format", "ssh")
	runGitCmd(t, dir, "config", "user.signingKey", "key::ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGdummy")

	dummyFile := filepath.Join(dir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# Test\n"), 0o644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}
	runGitCmd(t, dir, "add", "README.md")
	runGitCmd(t, dir, "commit", "-m", "initial commit")

	store, err := writ.Open(dir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	if store.Workspace == nil {
		t.Fatal("expected non-nil store.Workspace handle")
	}

	info := store.Workspace.Info()
	if info.Configured {
		t.Errorf("expected Configured = false, got true")
	}
	if store.Workspace.IsConfigured() {
		t.Errorf("expected IsConfigured() = false")
	}

	// Normal operations proceed locally without error
	revID, err := store.Reviews.Create(context.Background(), writ.NewReview{Title: "Local Review"})
	if err != nil {
		t.Fatalf("Reviews.Create failed: %v", err)
	}
	if revID == "" {
		t.Fatal("expected non-empty revID")
	}

	issID, err := store.Issues.Create(context.Background(), writ.NewIssue{Title: "Local Issue"})
	if err != nil {
		t.Fatalf("Issues.Create failed: %v", err)
	}
	if issID == "" {
		t.Fatal("expected non-empty issID")
	}

	// Ref without repo ID returns bare object ID
	if ref := store.Ref(revID); ref != revID {
		t.Errorf("store.Ref = %q, want bare %q", ref, revID)
	}
}

func TestWorkspaceRemoteURLRejected(t *testing.T) {
	dir := t.TempDir()
	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "writ.workspace", "https://github.com/acme/workspace.git")

	_, err := writ.Open(dir)
	if err == nil {
		t.Fatal("expected error opening repo with remote URL workspace, got nil")
	}
	if !errors.Is(err, writ.ErrWorkspaceRemoteURLNotSupported) {
		t.Errorf("expected ErrWorkspaceRemoteURLNotSupported, got: %v", err)
	}
}

func TestWorkspaceDiscoveryAndRegistration(t *testing.T) {
	ctx := context.Background()

	// 1. Create workspace repo
	wsDir, wsRepoID := setupTestRepoWithID(t, "Workspace Admin", "admin@example.com")
	wsStore, err := writ.Open(wsDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open wsStore: %v", err)
	}
	defer wsStore.Close()

	// 2. Create code repos A and B
	repoADir, repoAID := setupTestRepoWithID(t, "Alice Dev", "alice@example.com")
	repoBDir, repoBID := setupTestRepoWithID(t, "Bob Dev", "bob@example.com")

	// Configure writ.workspace on Repo A
	runGitCmd(t, repoADir, "config", "writ.workspace", wsDir)

	// Open Repo A with discovered workspace
	storeA, err := writ.Open(repoADir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open storeA: %v", err)
	}
	defer storeA.Close()

	if !storeA.Workspace.IsConfigured() {
		t.Fatal("expected storeA workspace to be configured")
	}

	// Open Repo B with explicit WithWorkspace option
	storeB, err := writ.Open(repoBDir, writ.WithWorkspace(wsDir), writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open storeB: %v", err)
	}
	defer storeB.Close()

	if !storeB.Workspace.IsConfigured() {
		t.Fatal("expected storeB workspace to be configured")
	}

	// 3. Register repos in workspace
	err = storeA.Workspace.Register(ctx, "acme/repo-a", []string{"git@github.com:acme/repo-a.git"})
	if err != nil {
		t.Fatalf("Register Repo A: %v", err)
	}

	err = storeB.Workspace.Register(ctx, "acme/repo-b", []string{"git@github.com:acme/repo-b.git"})
	if err != nil {
		t.Fatalf("Register Repo B: %v", err)
	}

	// 4. Query registered repos from Repo A's workspace handle
	repos, err := storeA.Workspace.Repos(ctx)
	if err != nil {
		t.Fatalf("storeA.Workspace.Repos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 registered repos, got %d", len(repos))
	}

	infoA := storeA.Workspace.Info()
	if infoA.LocalRepoID != string(repoAID) {
		t.Errorf("infoA LocalRepoID = %q, want %q", infoA.LocalRepoID, repoAID)
	}
	if infoA.WorkspaceRepoID != string(wsRepoID) {
		t.Errorf("infoA WorkspaceRepoID = %q, want %q", infoA.WorkspaceRepoID, wsRepoID)
	}
	if infoA.Slug != "acme/repo-a" {
		t.Errorf("infoA Slug = %q, want 'acme/repo-a'", infoA.Slug)
	}

	// Test Store.Ref
	if refA := storeA.Ref("rev-123"); refA != string(repoAID)+"#rev-123" {
		t.Errorf("storeA.Ref = %q, want %q", refA, string(repoAID)+"#rev-123")
	}
	if refB := storeB.Ref("rev-456"); refB != string(repoBID)+"#rev-456" {
		t.Errorf("storeB.Ref = %q, want %q", refB, string(repoBID)+"#rev-456")
	}
}

func TestEndToEndCrossRepoIssueLinkAndResolution(t *testing.T) {
	ctx := context.Background()

	// 1. Setup workspace repo and two code repos
	wsDir, _ := setupTestRepoWithID(t, "Workspace Admin", "admin@example.com")
	repoADir, repoAID := setupTestRepoWithID(t, "Alice Dev", "alice@example.com")
	repoBDir, repoBID := setupTestRepoWithID(t, "Bob Dev", "bob@example.com")

	// Both repos configured with workspace
	storeA, err := writ.Open(repoADir, writ.WithWorkspace(wsDir), writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open storeA: %v", err)
	}
	defer storeA.Close()

	storeB, err := writ.Open(repoBDir, writ.WithWorkspace(wsDir), writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open storeB: %v", err)
	}
	defer storeB.Close()

	// Register repos in workspace
	if err := storeA.Workspace.Register(ctx, "acme/repo-a", []string{"git@github.com:acme/repo-a.git"}); err != nil {
		t.Fatalf("register repo A: %v", err)
	}
	if err := storeB.Workspace.Register(ctx, "acme/repo-b", []string{"git@github.com:acme/repo-b.git"}); err != nil {
		t.Fatalf("register repo B: %v", err)
	}

	// 2. Create review in Repo B
	reviewID, err := storeB.Reviews.Create(ctx, writ.NewReview{
		Title: "Fix performance bug in worker",
		Base:  "main",
		Head:  "feat/fix-worker",
	})
	if err != nil {
		t.Fatalf("storeB.Reviews.Create: %v", err)
	}

	// 3. Create issue in Repo A (which targets the workspace repo)
	issueID, err := storeA.Issues.Create(ctx, writ.NewIssue{
		Title:       "High CPU in worker pool",
		Description: "Worker pool hits 100% CPU under high batch load",
	})
	if err != nil {
		t.Fatalf("storeA.Issues.Create: %v", err)
	}

	// 4. Link issue to review in Repo B
	qualifiedReviewRef := storeB.Ref(reviewID)
	err = storeA.Issues.Link(ctx, issueID, writ.Link{
		Target:     qualifiedReviewRef,
		TargetType: "review",
		Relation:   "fixed_by",
	})
	if err != nil {
		t.Fatalf("storeA.Issues.Link: %v", err)
	}

	// 5. Query issue in Repo A and verify link
	issueRes, err := storeA.Query.Issue(issueID)
	if err != nil {
		t.Fatalf("storeA.Query.Issue: %v", err)
	}

	if len(issueRes.Issue.Links) != 1 {
		t.Fatalf("expected 1 link on issue, got %d", len(issueRes.Issue.Links))
	}
	link := issueRes.Issue.Links[0]
	if link.Target != qualifiedReviewRef {
		t.Errorf("link.Target = %q, want %q", link.Target, qualifiedReviewRef)
	}
	if link.Relation != "fixed_by" {
		t.Errorf("link.Relation = %q, want 'fixed_by'", link.Relation)
	}

	// 6. Resolve link target through Repo A's workspace resolver
	resolved, err := storeA.Workspace.Resolve(ctx, link.Target)
	if err != nil {
		t.Fatalf("storeA.Workspace.Resolve: %v", err)
	}

	if !resolved.IsResolved() {
		t.Errorf("expected reference to be resolved")
	}
	if resolved.Scope != "cross-repo" {
		t.Errorf("resolved.Scope = %q, want 'cross-repo'", resolved.Scope)
	}
	if resolved.RepoID != string(repoBID) {
		t.Errorf("resolved.RepoID = %q, want %q", resolved.RepoID, repoBID)
	}
	if resolved.Slug != "acme/repo-b" {
		t.Errorf("resolved.Slug = %q, want 'acme/repo-b'", resolved.Slug)
	}
	if len(resolved.Remotes) != 1 || resolved.Remotes[0] != "git@github.com:acme/repo-b.git" {
		t.Errorf("resolved.Remotes = %v, want ['git@github.com:acme/repo-b.git']", resolved.Remotes)
	}
	if resolved.ObjectID != reviewID {
		t.Errorf("resolved.ObjectID = %q, want %q", resolved.ObjectID, reviewID)
	}

	// 7. Test auto-qualification of bare link target
	bareReviewID := "11112222333344445555666677778888"
	err = storeA.Issues.Link(ctx, issueID, writ.Link{
		Target:   bareReviewID,
		Relation: "relates_to",
	})
	if err != nil {
		t.Fatalf("storeA.Issues.Link bare: %v", err)
	}

	issueRes2, err := storeA.Query.Issue(issueID)
	if err != nil {
		t.Fatalf("storeA.Query.Issue second: %v", err)
	}
	// Check that bare target was qualified with repo A's ID
	var foundBareQualified bool
	expectedAutoQualified := string(repoAID) + "#" + bareReviewID
	for _, l := range issueRes2.Issue.Links {
		if l.Target == expectedAutoQualified {
			foundBareQualified = true
			break
		}
	}
	if !foundBareQualified {
		t.Errorf("expected bare target to be auto-qualified as %q, links are: %+v", expectedAutoQualified, issueRes2.Issue.Links)
	}
}

func TestWorkspaceIssueThreadsRouting(t *testing.T) {
	ctx := context.Background()

	wsDir, _ := setupTestRepoWithID(t, "Workspace Admin", "admin@example.com")
	repoDir, _ := setupTestRepoWithID(t, "Alice Dev", "alice@example.com")

	store, err := writ.Open(repoDir, writ.WithWorkspace(wsDir), writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()

	// Create issue in workspace
	issueID, err := store.Issues.Create(ctx, writ.NewIssue{Title: "Workspace Issue for Commenting"})
	if err != nil {
		t.Fatalf("Issues.Create: %v", err)
	}

	// Add comment on issue
	commID, err := store.Issues.Comment(ctx, issueID, writ.NewComment{Text: "A discussion comment on the issue"})
	if err != nil {
		t.Fatalf("Issues.Comment: %v", err)
	}

	// Query threads for the issue from local repo store handle
	threads, err := store.Query.Threads("issue", issueID)
	if err != nil {
		t.Fatalf("Query.Threads: %v", err)
	}

	if len(threads) != 1 {
		t.Fatalf("expected 1 root thread on issue, got %d", len(threads))
	}
	if threads[0].ObjectID != commID {
		t.Errorf("thread.ObjectID = %q, want %q", threads[0].ObjectID, commID)
	}
	if threads[0].Comment.Text != "A discussion comment on the issue" {
		t.Errorf("thread comment text = %q", threads[0].Comment.Text)
	}
}
