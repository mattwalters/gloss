package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/sync"
)

func TestReview_RoundTrip_SingleRepo(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ review open
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Add OAuth2 authentication provider",
		"-description", "Implements OAuth2 login flows",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed with %d; stderr: %s", code, stderr.String())
	}

	openOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Add OAuth2 authentication provider$`)
	matches := idRe.FindStringSubmatch(openOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected review open output: %q", openOut)
	}
	reviewID := matches[1]

	// 2. writ review list
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"review", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review list failed with %d; stderr: %s", code, stderr.String())
	}
	listOut := stdout.String()
	shortID := reviewID[:8]
	if !strings.Contains(listOut, shortID) {
		t.Errorf("list output missing short ID %s: %s", shortID, listOut)
	}
	if !strings.Contains(listOut, "open") {
		t.Errorf("list output missing status 'open': %s", listOut)
	}
	if !strings.Contains(listOut, "Add OAuth2 authentication provider") {
		t.Errorf("list output missing title: %s", listOut)
	}
	if !strings.Contains(listOut, "Alice") {
		t.Errorf("list output missing author Alice: %s", listOut)
	}

	// 3. writ review comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir,
		reviewID, "-m", "Looks good so far!",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment failed with %d; stderr: %s", code, stderr.String())
	}
	commentID := strings.TrimSpace(stdout.String())
	if len(commentID) != 32 {
		t.Fatalf("unexpected comment ID format: %q", commentID)
	}

	// Threaded reply
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir,
		reviewID, "-m", "Thanks!", "-reply-to", commentID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review reply comment failed with %d; stderr: %s", code, stderr.String())
	}
	replyID := strings.TrimSpace(stdout.String())
	if len(replyID) != 32 {
		t.Fatalf("unexpected reply comment ID format: %q", replyID)
	}

	// 4. writ review approve
	// First push a revision so approval can attach to a revision head
	commitFile(t, env.repoDir, "feature.go", "package main", "feature commit")
	revHead := commitFile(t, env.repoDir, "feature.go", "package main\n// updated", "feature update")

	store, err := writ.Open(env.repoDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Reviews.PushRevision(context.Background(), reviewID, revHead, revHead); err != nil {
		t.Fatalf("push revision: %v", err)
	}
	_ = store.Close()

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "approve", "-C", env.repoDir,
		reviewID, "-verdict", "approve", "-m", "LGTM",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review approve failed with %d; stderr: %s", code, stderr.String())
	}

	// 5. writ review assign <id> -add email:alice@example.com
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "assign", "-C", env.repoDir,
		reviewID, "-add", "email:alice@example.com",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review assign failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify status shows assignee
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status after assign failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Assignees:   email:alice@example.com") {
		t.Errorf("status output missing 'Assignees:   email:alice@example.com': %s", stdout.String())
	}

	// 6. writ review assign -remove email:alice@example.com -add user:bob
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "assign", "-C", env.repoDir,
		reviewID, "-remove", "email:alice@example.com", "-add", "user:bob",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review reassign failed with %d; stderr: %s", code, stderr.String())
	}

	// 7. Verify review list with -assignee
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "list", "-C", env.repoDir,
		"-assignee", "user:bob",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review list -assignee failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), shortID) {
		t.Errorf("review list -assignee missing %s: %s", shortID, stdout.String())
	}

	// 8. writ review status (read mode)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status failed with %d; stderr: %s", code, stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "Status:      open") {
		t.Errorf("status output missing 'Status:      open': %s", statusOut)
	}
	if !strings.Contains(statusOut, "Assignees:   user:bob") {
		t.Errorf("status output missing 'Assignees:   user:bob': %s", statusOut)
	}
	if !strings.Contains(statusOut, "Revisions:   1") {
		t.Errorf("status output missing 'Revisions:   1': %s", statusOut)
	}
	if !strings.Contains(statusOut, "Approvals:   1") {
		t.Errorf("status output missing 'Approvals:   1': %s", statusOut)
	}

	// 9. writ review status <id> merged -merge-commit HEAD
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir,
		reviewID, "merged", "-merge-commit", "HEAD", "-reason", "All checks passed",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status transition to merged failed with %d; stderr: %s", code, stderr.String())
	}

	// 7. Verify status reflects merged
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status query after merge failed with %d; stderr: %s", code, stderr.String())
	}
	statusMergedOut := stdout.String()
	if !strings.Contains(statusMergedOut, "Status:      merged") {
		t.Errorf("status output missing 'Status:      merged': %s", statusMergedOut)
	}
	if !strings.Contains(statusMergedOut, "Merge commit: "+revHead) {
		t.Errorf("status output missing Merge commit: %s", statusMergedOut)
	}
	if !strings.Contains(statusMergedOut, "Reason:       All checks passed") {
		t.Errorf("status output missing Reason: %s", statusMergedOut)
	}

	// 8. Verify refusal to transition out of merged
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID, "open",
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 when transitioning out of merged, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot transition review") {
		t.Errorf("stderr missing refusal message: %s", stderr.String())
	}
}

func TestReview_EveryVerbIsThin_RevParse(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}

	baseSHA := commitFile(t, env.repoDir, "base.txt", "base content", "base commit")

	cmd := exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = env.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b feature: %v (%s)", err, string(out))
	}
	headSHA := commitFile(t, env.repoDir, "feature.txt", "feature content", "feature commit")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Feature branch review",
		"-base", "master", "-head", "feature",
		"-draft",
	}, &stdout, &stderr)
	if code != 0 {
		// If default branch is main or master
		stdout.Reset()
		stderr.Reset()
		code = run(context.Background(), []string{
			"review", "open", "-C", env.repoDir,
			"-title", "Feature branch review",
			"-base", baseSHA, "-head", "feature",
			"-draft",
		}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("review open failed with %d; stderr: %s", code, stderr.String())
		}
	}

	openOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(draft\) Feature branch review$`)
	matches := idRe.FindStringSubmatch(openOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected review open draft output: %q", openOut)
	}
	reviewID := matches[1]

	store, err := writ.Open(env.repoDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	res, err := store.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("query review: %v", err)
	}

	if res.Review.Status != "draft" {
		t.Errorf("status = %q, want draft", res.Review.Status)
	}

	if len(res.Review.Revisions) != 1 {
		t.Fatalf("expected 1 revision, got %d", len(res.Review.Revisions))
	}

	rev := res.Review.Revisions[0]
	if rev.Base != baseSHA {
		t.Errorf("revision base = %q, want full OID %q", rev.Base, baseSHA)
	}
	if rev.Head != headSHA {
		t.Errorf("revision head = %q, want full OID %q", rev.Head, headSHA)
	}
}

func TestReview_TwoWriters_BareRemote(t *testing.T) {
	requireGit(t)
	tempDir := t.TempDir()

	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	// 1. Bare remote
	bareDir := filepath.Join(tempDir, "bare.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}

	// 2. Clone A
	cloneADir := filepath.Join(tempDir, "cloneA")
	cmd := exec.Command("git", "clone", bareDir, cloneADir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone A failed: %v (%s)", err, string(out))
	}
	const writerIDA = "aaaaaaaaaaaaaaaa"
	setGitConfig(t, cloneADir, "writ.writerId", writerIDA)
	setGitConfig(t, cloneADir, "user.name", "Alice")
	setGitConfig(t, cloneADir, "user.email", "alice@example.com")
	setupSigningKey(t, cloneADir)

	var stdoutA, stderrA bytes.Buffer
	codeA := run(context.Background(), []string{"init", "-C", cloneADir}, &stdoutA, &stderrA)
	if codeA != 0 {
		t.Fatalf("init in clone A failed: %s", stderrA.String())
	}

	baseSHA := commitFile(t, cloneADir, "file.txt", "initial", "initial commit")
	cmd = exec.Command("git", "push", "origin", "HEAD:main")
	cmd.Dir = cloneADir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git push clone A main: %v (%s)", err, string(out))
	}

	stdoutA.Reset()
	stderrA.Reset()
	codeA = run(context.Background(), []string{
		"review", "open", "-C", cloneADir,
		"-title", "Alice Review",
		"-base", baseSHA, "-head", baseSHA,
	}, &stdoutA, &stderrA)
	if codeA != 0 {
		t.Fatalf("review open in clone A failed: %s", stderrA.String())
	}

	openOut := strings.TrimSpace(stdoutA.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Alice Review$`)
	matches := idRe.FindStringSubmatch(openOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected review open output in clone A: %q", openOut)
	}
	reviewID := matches[1]

	stdoutA.Reset()
	stderrA.Reset()
	codeA = run(context.Background(), []string{
		"review", "approve", "-C", cloneADir,
		reviewID, "-verdict", "approve", "-m", "Alice LGTM",
	}, &stdoutA, &stderrA)
	if codeA != 0 {
		t.Fatalf("review approve in clone A failed: %s", stderrA.String())
	}

	// Push writ refs from clone A to origin
	identA := identity.Identity{
		WriterID: identity.WriterID(writerIDA),
		Author:   identity.Author{Name: "Alice", Email: "alice@example.com"},
	}
	syncA, err := sync.Open(cloneADir, identA)
	if err != nil {
		t.Fatalf("sync.Open clone A: %v", err)
	}
	if _, err := syncA.Push(context.Background(), "origin"); err != nil {
		t.Fatalf("sync push clone A: %v", err)
	}

	// 3. Clone B
	cloneBDir := filepath.Join(tempDir, "cloneB")
	cmd = exec.Command("git", "clone", bareDir, cloneBDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone B failed: %v (%s)", err, string(out))
	}
	const writerIDB = "bbbbbbbbbbbbbbbb"
	setGitConfig(t, cloneBDir, "writ.writerId", writerIDB)
	setGitConfig(t, cloneBDir, "user.name", "Bob")
	setGitConfig(t, cloneBDir, "user.email", "bob@example.com")
	setupSigningKey(t, cloneBDir)

	var stdoutB, stderrB bytes.Buffer
	codeB := run(context.Background(), []string{"init", "-C", cloneBDir}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("init in clone B failed: %s", stderrB.String())
	}

	cmd = exec.Command("git", "fetch", "origin")
	cmd.Dir = cloneBDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fetch in clone B failed: %v (%s)", err, string(out))
	}

	// Clone B lists reviews: Alice's review is shown as open
	stdoutB.Reset()
	stderrB.Reset()
	codeB = run(context.Background(), []string{"review", "list", "-C", cloneBDir}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("review list in clone B failed: %s", stderrB.String())
	}
	if !strings.Contains(stdoutB.String(), "Alice Review") {
		t.Errorf("clone B list output missing 'Alice Review': %s", stdoutB.String())
	}

	// Clone B checks status: Alice's approval is counted
	stdoutB.Reset()
	stderrB.Reset()
	codeB = run(context.Background(), []string{"review", "status", "-C", cloneBDir, reviewID}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("review status in clone B failed: %s", stderrB.String())
	}
	if !strings.Contains(stdoutB.String(), "Approvals:   1") {
		t.Errorf("clone B status output missing 'Approvals:   1': %s", stdoutB.String())
	}

	// Clone B approves the same head
	stdoutB.Reset()
	stderrB.Reset()
	codeB = run(context.Background(), []string{
		"review", "approve", "-C", cloneBDir,
		reviewID, "-verdict", "approve", "-m", "Bob LGTM",
	}, &stdoutB, &stderrB)
	if codeB != 0 {
		t.Fatalf("review approve in clone B failed: %s", stderrB.String())
	}

	// Verify both approvals survive (subject didn't collapse)
	storeB, err := writ.Open(cloneBDir)
	if err != nil {
		t.Fatalf("open store B: %v", err)
	}
	defer storeB.Close()

	resB, err := storeB.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("query review in B: %v", err)
	}

	if len(resB.Review.Approvals) != 2 {
		t.Fatalf("expected 2 approvals, got %d: %+v", len(resB.Review.Approvals), resB.Review.Approvals)
	}

	subjects := map[string]bool{}
	for _, app := range resB.Review.Approvals {
		subjects[app.Subject] = true
	}
	if !subjects["email:alice@example.com"] || !subjects["email:bob@example.com"] {
		t.Errorf("approvals subjects = %v, want email:alice@example.com and email:bob@example.com", subjects)
	}
}

func TestReview_ErrorSurfaces(t *testing.T) {
	t.Run("unconfigured_identity", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "open", "-C", env.repoDir, "-title", "Test"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected exit code 1 for unconfigured identity, got %d", code)
		}
		if !strings.Contains(stderr.String(), "run 'writ init'") {
			t.Errorf("stderr does not advise 'run 'writ init'': %s", stderr.String())
		}
	})

	t.Run("unknown_review_id", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "status", "-C", env.repoDir, "00000000000000000000000000000000"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("expected exit code 1 for unknown review ID, got %d", code)
		}
		if !strings.Contains(stderr.String(), "writ: no review with id 00000000000000000000000000000000") {
			t.Errorf("unexpected stderr: %s", stderr.String())
		}
	})

	t.Run("ambiguous_prefix", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		_ = run(context.Background(), []string{"review", "open", "-C", env.repoDir, "-title", "Review 1"}, &bytes.Buffer{}, &bytes.Buffer{})
		_ = run(context.Background(), []string{"review", "open", "-C", env.repoDir, "-title", "Review 2"}, &bytes.Buffer{}, &bytes.Buffer{})

		// Empty or common prefix matching both reviews
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "status", "-C", env.repoDir, ""}, &stdout, &stderr)
		if code != 2 {
			// Empty prefix is a usage error (missing ID)
			if code != 2 {
				t.Errorf("expected exit code 2 for empty ID, got %d", code)
			}
		}

		// Find common prefix if any, or query with single character if matching
		store, err := writ.Open(env.repoDir)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		reviews, _ := store.Query.Reviews(writ.ReviewFilter{})
		_ = store.Close()

		if len(reviews) >= 2 {
			id1 := reviews[0].ObjectID
			id2 := reviews[1].ObjectID
			var commonPrefix string
			for i := 0; i < len(id1) && i < len(id2); i++ {
				if id1[i] == id2[i] {
					commonPrefix += string(id1[i])
				} else {
					break
				}
			}
			if commonPrefix != "" {
				stdout.Reset()
				stderr.Reset()
				code = run(context.Background(), []string{"review", "status", "-C", env.repoDir, commonPrefix}, &stdout, &stderr)
				if code != 1 {
					t.Fatalf("expected exit code 1 for ambiguous prefix, got %d", code)
				}
				if !strings.Contains(stderr.String(), "ambiguous review ID prefix") {
					t.Errorf("stderr does not mention ambiguous prefix: %s", stderr.String())
				}
			}
		}
	})

	t.Run("bad_verdict_enum", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "approve", "-C", env.repoDir, "12345678", "-verdict", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad verdict enum, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid verdict") {
			t.Errorf("stderr missing invalid verdict message: %s", stderr.String())
		}
	})

	t.Run("bad_status_enum", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "status", "-C", env.repoDir, "12345678", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad status enum, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid status") {
			t.Errorf("stderr missing invalid status message: %s", stderr.String())
		}
	})

	t.Run("missing_comment_m", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "comment", "-C", env.repoDir, "12345678"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing -m, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-m is required") {
			t.Errorf("stderr missing '-m is required': %s", stderr.String())
		}
	})

	t.Run("conflicting_resolve_flags", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "comment", "-C", env.repoDir, "12345678", "-resolve", "-unresolve"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for conflicting flags, got %d", code)
		}
		if !strings.Contains(stderr.String(), "cannot specify both -resolve and -unresolve") {
			t.Errorf("stderr missing conflicting flags message: %s", stderr.String())
		}
	})

	t.Run("missing_open_title", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "open", "-C", env.repoDir}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for missing -title, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-title is required") {
			t.Errorf("stderr missing '-title is required': %s", stderr.String())
		}
	})

	t.Run("base_without_head", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "open", "-C", env.repoDir, "-title", "T", "-base", "main"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for -base without -head, got %d", code)
		}
	})

	t.Run("missing_assign_flags", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "assign", "-C", env.repoDir, "12345678"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for assign with no flags, got %d", code)
		}
		if !strings.Contains(stderr.String(), "at least one -add or -remove is required") {
			t.Errorf("stderr missing '-add or -remove is required': %s", stderr.String())
		}
	})

	t.Run("missing_label_flags", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "label", "-C", env.repoDir, "12345678"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for label with no flags, got %d", code)
		}
		if !strings.Contains(stderr.String(), "at least one -add or -remove is required") {
			t.Errorf("stderr missing '-add or -remove is required': %s", stderr.String())
		}
	})

	t.Run("missing_link_target", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "link", "-C", env.repoDir, "12345678", "-relation", "fixes"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for link without target, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-target is required") {
			t.Errorf("stderr missing '-target is required': %s", stderr.String())
		}
	})

	t.Run("missing_link_relation", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "link", "-C", env.repoDir, "12345678", "-target", "target1"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for link without relation, got %d", code)
		}
		if !strings.Contains(stderr.String(), "-relation is required") {
			t.Errorf("stderr missing '-relation is required': %s", stderr.String())
		}
	})

	t.Run("bad_link_relation", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)
		_ = run(context.Background(), []string{"init", "-C", env.repoDir}, &bytes.Buffer{}, &bytes.Buffer{})

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", "link", "-C", env.repoDir, "12345678", "-target", "target1", "-relation", "bogus"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("expected exit code 2 for bad relation enum, got %d", code)
		}
		if !strings.Contains(stderr.String(), "invalid relation") {
			t.Errorf("stderr missing invalid relation message: %s", stderr.String())
		}
	})
}

func TestReview_HelpAndUsage(t *testing.T) {
	for _, subcmd := range []string{"open", "comment", "approve", "assign", "label", "link", "status", "list"} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"review", subcmd, "-h"}, &stdout, &stderr)
		if code != 0 {
			t.Errorf("review %s -h returned %d, want 0", subcmd, code)
		}
		if !strings.Contains(stderr.String(), "Usage: writ review "+subcmd) && !strings.Contains(stdout.String(), "Usage: writ review "+subcmd) {
			t.Errorf("review %s -h missing usage in output", subcmd)
		}
	}
}

func TestReviewComment_ResolveWorkflow(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)
	commitFile(t, env.repoDir, "file.txt", "v1", "initial")

	var stdout, stderr bytes.Buffer

	// 1. Init repo
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}

	// 2. Open review
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Fix validation bug",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed: %s", stderr.String())
	}

	reviewID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]

	// 3. Post root comment
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-m", "Please rename this variable",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment failed: %s", stderr.String())
	}
	commentID := strings.TrimSpace(stdout.String())

	// 4. Resolve comment thread without reply
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-reply-to", commentID,
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment -resolve failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "resolved") {
		t.Errorf("expected stdout to mention resolved: %s", stdout.String())
	}

	// 5. Query store and verify resolved
	store, err := writ.Open(env.repoDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	comments, err := store.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if !comments[0].Comment.IsResolved() {
		t.Errorf("expected comment to be resolved")
	}

	// 6. Post reply with -resolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-reply-to", commentID,
		"-m", "Done and verified",
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment reply with -resolve failed: %s", stderr.String())
	}

	// 7. Unresolve comment thread
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-reply-to", commentID,
		"-unresolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment -unresolve failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "unresolved") {
		t.Errorf("expected stdout to mention unresolved: %s", stdout.String())
	}

	// 8. Re-query store and verify unresolved
	comments, err = store.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	var rootComment *writ.CommentResult
	for i := range comments {
		if comments[i].ObjectID == commentID {
			rootComment = &comments[i]
			break
		}
	}
	if rootComment == nil {
		t.Fatalf("root comment not found")
	}
	if rootComment.Comment.IsResolved() {
		t.Errorf("expected root comment to be unresolved")
	}

	// 9. Resolve directly using comment ID as argument
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, commentID,
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment <commentID> -resolve failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "resolved") {
		t.Errorf("expected stdout to mention resolved: %s", stdout.String())
	}

	// 10. Reply directly using comment ID as argument with -resolve
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, commentID,
		"-m", "Direct reply with resolve",
		"-resolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment <commentID> -m ... -resolve failed: %s", stderr.String())
	}

	// 11. Unresolve directly using comment ID as argument
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, commentID,
		"-unresolve",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review comment <commentID> -unresolve failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "unresolved") {
		t.Errorf("expected stdout to mention unresolved: %s", stdout.String())
	}

	// 12. Error when resolving review ID without comment target or message
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "comment", "-C", env.repoDir, reviewID,
		"-resolve",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected error when resolving review ID without comment target, got code %d", code)
	}
	if !strings.Contains(stderr.String(), "comment or thread ID is required to resolve") {
		t.Errorf("unexpected error message: %s", stderr.String())
	}
}

// openReviewWithComment stands up a repo with one review carrying one root
// comment, and returns both IDs.
func openReviewWithComment(t *testing.T, dir string) (reviewID, commentID string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"init", "-C", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, dir, "README.md", "# Hello", "initial commit")

	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{
		"review", "open", "-C", dir, "-title", "Resolve attribution",
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("review open failed with %d; stderr: %s", code, stderr.String())
	}
	matches := regexp.MustCompile(`^([0-9a-f]{32}) `).FindStringSubmatch(strings.TrimSpace(stdout.String()))
	if len(matches) < 2 {
		t.Fatalf("unexpected review open output: %q", stdout.String())
	}
	reviewID = matches[1]

	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{
		"review", "comment", "-C", dir, reviewID, "-m", "Please rename this variable",
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("review comment failed with %d; stderr: %s", code, stderr.String())
	}
	commentID = strings.TrimSpace(stdout.String())

	return reviewID, commentID
}

// resolveUnattributed sets a comment's resolution through the public engine
// API with no ResolvedBy, which is what the spec permits and what a bridge, an
// importer, or an older client writes.
func resolveUnattributed(t *testing.T, dir, commentID string, resolved bool) {
	t.Helper()

	store, err := writ.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Comments.Resolve(context.Background(), commentID, writ.CommentResolve{Resolved: resolved}); err != nil {
		t.Fatalf("resolve %s: %v", commentID, err)
	}
}

// commentResolution returns the folded resolution state of one comment.
func commentResolution(t *testing.T, dir, reviewID, commentID string) (resolved bool, resolvedBy string) {
	t.Helper()

	store, err := writ.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	comments, err := store.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
	if err != nil {
		t.Fatalf("query comments: %v", err)
	}
	for _, c := range comments {
		if c.ObjectID == commentID {
			return c.Comment.IsResolved(), c.Comment.ResolvedBy
		}
	}
	t.Fatalf("comment %s not found among %d comments", commentID, len(comments))
	return false, ""
}

// TestReviewComment_ResolveAttribution pins the person a CLI resolve is
// recorded against. resolved_by is the only person-level attribution a resolve
// op carries — the signed commit names a writer-id, which is device-scoped and
// names nobody — so leaving it unset makes every resolve permanently anonymous
// in a log that is never rewritten.
func TestReviewComment_ResolveAttribution(t *testing.T) {
	// The default is the writer's derived person identifier, the same one
	// `review approve` falls back to.
	t.Run("writer", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)

		reviewID, commentID := openReviewWithComment(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-resolve",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review comment -resolve failed with %d; stderr: %s", code, stderr.String())
		}

		resolved, resolvedBy := commentResolution(t, env.repoDir, reviewID, commentID)
		if !resolved {
			t.Fatalf("comment is not resolved")
		}
		if resolvedBy == "" {
			t.Fatalf("resolved_by is empty: a CLI resolve must name the person who ran it")
		}
		if resolvedBy != "email:alice@example.com" {
			t.Errorf("resolved_by = %q, want %q", resolvedBy, "email:alice@example.com")
		}
	})

	// writ.personId overrides the email derivation here exactly as it does for
	// an approval subject.
	t.Run("person_id_override", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.personId", "  User:Alice  ")
		setupSigningKey(t, env.repoDir)

		reviewID, commentID := openReviewWithComment(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-resolve",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review comment -resolve failed with %d; stderr: %s", code, stderr.String())
		}

		if _, resolvedBy := commentResolution(t, env.repoDir, reviewID, commentID); resolvedBy != "user:alice" {
			t.Errorf("resolved_by = %q, want %q (writ.personId overrides the email derivation, normalized)", resolvedBy, "user:alice")
		}
	})

	// An unresolve does not carry resolved_by. The field names the person who
	// resolved the thread, not the person who last changed its state, so
	// reopening leaves the recorded resolver in place: stale, but true.
	t.Run("unresolve_preserves_resolver", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.personId", "user:alice")
		setupSigningKey(t, env.repoDir)

		reviewID, commentID := openReviewWithComment(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-resolve",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review comment -resolve failed with %d; stderr: %s", code, stderr.String())
		}

		setGitConfig(t, env.repoDir, "writ.personId", "user:bob")

		stdout.Reset()
		stderr.Reset()
		if code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-unresolve",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review comment -unresolve failed with %d; stderr: %s", code, stderr.String())
		}

		resolved, resolvedBy := commentResolution(t, env.repoDir, reviewID, commentID)
		if resolved {
			t.Fatalf("comment is still resolved")
		}
		if resolvedBy == "user:bob" {
			t.Fatalf("resolved_by = %q: bob reopened the thread, he did not resolve it", resolvedBy)
		}
		if resolvedBy != "user:alice" {
			t.Errorf("resolved_by = %q, want %q: the recorded resolver survives the reopen", resolvedBy, "user:alice")
		}
	})

	// The reason an unresolve must not carry the field: resolved and
	// resolved_by are independent lww accumulators, so they can be sourced from
	// different ops. Interleaving with any producer that omits resolved_by —
	// which the spec permits and store.Comments.Resolve allows verbatim — would
	// otherwise let the reopener land in an otherwise empty slot and be
	// recorded, permanently, as the resolver.
	t.Run("interleaved_with_unattributed_producer", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.personId", "user:bob")
		setupSigningKey(t, env.repoDir)

		reviewID, commentID := openReviewWithComment(t, env.repoDir)

		// alice resolves through the engine API without attribution, the way a
		// bridge, an importer, or a pre-WRIT-132 client does.
		resolveUnattributed(t, env.repoDir, commentID, true)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-unresolve",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review comment -unresolve failed with %d; stderr: %s", code, stderr.String())
		}

		resolveUnattributed(t, env.repoDir, commentID, true)

		resolved, resolvedBy := commentResolution(t, env.repoDir, reviewID, commentID)
		if !resolved {
			t.Fatalf("comment is not resolved")
		}
		if resolvedBy == "user:bob" {
			t.Fatalf("resolved_by = %q: bob only reopened the thread, and an unattributed resolve must not attribute itself to him", resolvedBy)
		}
		if resolvedBy != "" {
			t.Errorf("resolved_by = %q, want empty: no op in this sequence carried one", resolvedBy)
		}
	})

	// With no person identifier to be had, the chain ends. It does not end at
	// the writer ID: that has no scheme, so writing one would record a bare
	// identifier no conforming reader can interpret. Refusing and saying what
	// to configure is the only honest option left — the same ruling
	// `review approve` follows.
	//
	// The state is driven with a writ.personId writ cannot use, not with a
	// blank user.email. Since WRIT-131 a whitespace-only user.email is not an
	// identity at all: identity.Load returns a zero Identity, so that
	// repository is the no_writer_identity case below rather than this one,
	// and it is refused one guard earlier. An unusable writ.personId is now
	// the only way to hold a valid writer identity and still have nothing to
	// attribute a resolve to, so it is what keeps this branch covered.
	t.Run("no_derivable_person_id", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)

		reviewID, commentID := openReviewWithComment(t, env.repoDir)

		// Set it after the repo is stood up: review open needs a usable
		// identity to create the comment being resolved here.
		setGitConfig(t, env.repoDir, "writ.personId", "alice")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID,
			"-m", "Fixed in latest push", "-resolve",
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("review comment -resolve should have failed with no derivable person identifier; stdout: %s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "writ.personId") {
			t.Errorf("error should name writ.personId, got %q", stderr.String())
		}
		// The value is configured, so the diagnosis must say it is wrong
		// rather than absent: telling a user to set a key they already set
		// sends them to look at something that is already there.
		if !strings.Contains(stderr.String(), "alice") {
			t.Errorf("error should quote the offending value, got %q", stderr.String())
		}

		if resolved, resolvedBy := commentResolution(t, env.repoDir, reviewID, commentID); resolved || resolvedBy != "" {
			t.Errorf("comment resolution = (%v, %q), want (false, \"\"): the refused write must leave nothing behind", resolved, resolvedBy)
		}

		// The refusal lands before any write, so the reply that accompanied the
		// resolve is not left behind either.
		store, err := writ.Open(env.repoDir)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		defer store.Close()
		comments, err := store.Query.Comments(writ.CommentFilter{SubjectID: reviewID})
		if err != nil {
			t.Fatalf("query comments: %v", err)
		}
		if len(comments) != 1 {
			t.Errorf("comment count = %d, want 1: a refused resolve must not leave its -m reply behind", len(comments))
		}
	})

	// A repository with writ refs but no writ.writerId — a clone whose owner
	// never ran `writ init` — has no identity at all, not merely no person
	// identifier. identity.Load returns a zero Identity there, so PersonID is
	// empty with no PersonIDErr to explain it; naming writ.personId and
	// user.email would send a user who has both set to look at keys that are
	// already correct. The writer ID is checked first so the refusal names the
	// fix that actually applies, matching what every other write path says.
	t.Run("no_writer_identity", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)

		_, commentID := openReviewWithComment(t, env.repoDir)

		cmd := exec.Command("git", "config", "--unset", "writ.writerId")
		cmd.Dir = env.repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config --unset writ.writerId failed: %v (%s)", err, string(out))
		}

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"review", "comment", "-C", env.repoDir, commentID, "-resolve",
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("review comment -resolve should have failed with no writer identity; stdout: %s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "run 'writ init'") {
			t.Errorf("error should point at writ init, got %q", stderr.String())
		}
		if strings.Contains(stderr.String(), "writ.personId") {
			t.Errorf("error should not name writ.personId when nothing is configured at all, got %q", stderr.String())
		}
	})
}

func TestReview_LabelAndLink(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed: %s", stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. Open review
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "open", "-C", env.repoDir,
		"-title", "Review for label and link test",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review open failed: %s", stderr.String())
	}
	reviewID := strings.Split(strings.TrimSpace(stdout.String()), " ")[0]
	shortID := reviewID[:8]

	// 2. Add labels: area/engine and wip
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "label", "-C", env.repoDir, reviewID,
		"-add", "area/engine", "-add", "wip",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review label add failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated labels") {
		t.Errorf("expected stdout to mention updated labels: %s", stdout.String())
	}

	// 3. Status shows labels
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Labels:      area/engine, wip") {
		t.Errorf("status missing labels: %s", stdout.String())
	}

	// 4. Update labels: remove wip, add needs-docs
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "label", "-C", env.repoDir, reviewID,
		"-remove", "wip", "-add", "needs-docs",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review label update failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Labels:      area/engine, needs-docs") {
		t.Errorf("status missing updated labels: %s", stdout.String())
	}

	// 5. Filter reviews by label
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "list", "-C", env.repoDir, "-label", "area/engine",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review list -label failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), shortID) {
		t.Errorf("review list -label missing review %s: %s", shortID, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "list", "-C", env.repoDir, "-label", "wip",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review list -label wip failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), shortID) {
		t.Errorf("review list -label wip unexpectedly contained review %s: %s", shortID, stdout.String())
	}

	// 6. Link review to issue (fixes)
	targetID := "0123456789abcdef0123456789abcdef"
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "link", "-C", env.repoDir, reviewID,
		"-target", targetID, "-relation", "fixes", "-target-type", "issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review link failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "link fixes -> "+targetID) {
		t.Errorf("unexpected review link output: %s", stdout.String())
	}

	// 7. Status shows links
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status failed: %s", stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "Links:") || !strings.Contains(statusOut, "fixes "+targetID) {
		t.Errorf("status missing Links section: %s", statusOut)
	}

	// 8. JSON output check for status
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID, "--json",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status --json failed: %s", stderr.String())
	}
	jsonOut := stdout.String()
	if !strings.Contains(jsonOut, `"labels":["area/engine","needs-docs"]`) {
		t.Errorf("json status missing labels: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"links":[{"target":"`+targetID+`","target_type":"issue","relation":"fixes"}]`) {
		t.Errorf("json status missing links: %s", jsonOut)
	}

	// 9. Retract link
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "link", "-C", env.repoDir, reviewID,
		"-target", targetID, "-relation", "none",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review link retract failed: %s", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"review", "status", "-C", env.repoDir, reviewID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("review status failed: %s", stderr.String())
	}
	if strings.Contains(stdout.String(), "Links:") {
		t.Errorf("status still contains Links after retraction: %s", stdout.String())
	}
}

// openReviewWithRevision initializes writ in dir, opens a review and pushes one
// revision, so that an approval has a head to attach to. Returns the review ID.
func openReviewWithRevision(t *testing.T, dir string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"init", "-C", dir}, &stdout, &stderr); code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, dir, "README.md", "# Hello", "initial commit")

	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{
		"review", "open", "-C", dir, "-title", "Approval subject",
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("review open failed with %d; stderr: %s", code, stderr.String())
	}
	matches := regexp.MustCompile(`^([0-9a-f]{32}) `).FindStringSubmatch(strings.TrimSpace(stdout.String()))
	if len(matches) < 2 {
		t.Fatalf("unexpected review open output: %q", stdout.String())
	}
	reviewID := matches[1]

	revHead := commitFile(t, dir, "feature.go", "package main", "feature commit")
	store, err := writ.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Reviews.PushRevision(context.Background(), reviewID, revHead, revHead); err != nil {
		t.Fatalf("push revision: %v", err)
	}
	_ = store.Close()

	return reviewID
}

// reviewApprovalSubjects returns the subject of every approval recorded on a
// review, in fold order.
func reviewApprovalSubjects(t *testing.T, dir, reviewID string) []string {
	t.Helper()

	store, err := writ.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	res, err := store.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("query review: %v", err)
	}
	subjects := make([]string, 0, len(res.Review.Approvals))
	for _, app := range res.Review.Approvals {
		subjects = append(subjects, app.Subject)
	}
	return subjects
}

// TestReviewApprove_Subject covers both halves of the approval subject fallback
// chain. Approvals are keyed on {subject, revision}, so a subject that
// normalizes away does not merely lose attribution: every anonymous approval on
// a revision collapses into one entry, last verdict winning, and two reviewers
// silently overwrite each other.
func TestReviewApprove_Subject(t *testing.T) {
	// An explicit -subject is used as given, and a whitespace-only one falls
	// back to the writer email. The engine normalizes the subject on the way
	// into the op, so a raw != "" guard on the flag would skip the fallback and
	// record an approval attributed to nobody.
	t.Run("flag", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)

		reviewID := openReviewWithRevision(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID, "-subject", "   ",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review approve with whitespace subject failed with %d; stderr: %s", code, stderr.String())
		}

		// A real -subject must reach the op rather than being overridden by the
		// writer email, and is normalized on the way in.
		stdout.Reset()
		stderr.Reset()
		if code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID, "-subject", "  User:Bob ",
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review approve with explicit subject failed with %d; stderr: %s", code, stderr.String())
		}

		subjects := reviewApprovalSubjects(t, env.repoDir, reviewID)
		if len(subjects) != 2 {
			t.Fatalf("approval subjects = %q, want 2 distinct subjects", subjects)
		}
		got := map[string]bool{}
		for _, s := range subjects {
			got[s] = true
		}
		if !got["email:alice@example.com"] {
			t.Errorf("approval subjects = %q, missing email:alice@example.com (whitespace -subject must fall back to the writer person ID)", subjects)
		}
		if !got["user:bob"] {
			t.Errorf("approval subjects = %q, missing user:bob (an explicit -subject must reach the op, normalized)", subjects)
		}
	})

	// writ.personId overrides the derivation, which is how a workspace that
	// identifies people by handle rather than by address configures itself —
	// and how anyone keeps their address out of a public, unretractable log.
	t.Run("person_id_override", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.personId", "  User:Alice  ")
		setupSigningKey(t, env.repoDir)

		reviewID := openReviewWithRevision(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID,
		}, &stdout, &stderr); code != 0 {
			t.Fatalf("review approve failed with %d; stderr: %s", code, stderr.String())
		}

		subjects := reviewApprovalSubjects(t, env.repoDir, reviewID)
		if len(subjects) != 1 || subjects[0] != "user:alice" {
			t.Errorf("approval subjects = %q, want [\"user:alice\"] (writ.personId overrides the email derivation, normalized)", subjects)
		}
	})

	// With no -subject and nothing to derive a person identifier from, the
	// chain ends. It used to end at the writer ID, which is not a person
	// identifier at all: it has no scheme, so writing one would record a bare
	// identifier that no conforming reader can interpret. Refusing and saying
	// what to configure is the only honest option left.
	t.Run("no_derivable_person_id", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.writerId", "cccccccccccccccc")
		setupSigningKey(t, env.repoDir)
		reviewID := openReviewWithRevision(t, env.repoDir)

		// Blank the address only now. Since WRIT-131 identity.Load treats a
		// whitespace-only user.email as unset, so a repo configured this way
		// from the start cannot open a review to approve in the first place.
		// The state under test is the one that outlives configuration: a
		// review already on disk, and no person identifier left to sign it
		// over to.
		setGitConfig(t, env.repoDir, "user.email", "   ")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID,
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("review approve should have failed with no derivable person identifier; stdout: %s", stdout.String())
		}
		if !strings.Contains(stderr.String(), "writ.personId") {
			t.Errorf("error should name writ.personId, got %q", stderr.String())
		}

		if subjects := reviewApprovalSubjects(t, env.repoDir, reviewID); len(subjects) != 0 {
			t.Errorf("approval subjects = %q, want none: the refused write must leave nothing behind", subjects)
		}
	})
}

// TestReviewApprove_PersonIDDiagnosis pins the invalid half of the diagnosis:
// a user who set writ.personId to something that is not a person identifier
// must be told the value is wrong, not that nothing is configured — the second
// sends them to look at a key that is already there.
//
// It used to pin the difference between that and the missing half. It no
// longer can. Since WRIT-131 a whitespace-only user.email is not an identity,
// so Load returns a zero Identity with a nil PersonIDErr, engine/open.go
// collapses the failure into ErrNoIdentity, and the missing case here takes
// review approve's generic fallback rather than the PersonIDErr branch. The
// missing half is pinned where it still runs, in TestInit_PersonID.
func TestReviewApprove_PersonIDDiagnosis(t *testing.T) {
	t.Run("invalid writ.personId is reported as invalid", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setGitConfig(t, env.repoDir, "writ.personId", "alice")
		setupSigningKey(t, env.repoDir)

		reviewID := openReviewWithRevision(t, env.repoDir)

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID,
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("approve should refuse a bare writ.personId; stdout: %s", stdout.String())
		}
		got := stderr.String()
		// The diagnosis must say the configured value is wrong, not that
		// nothing is configured.
		if !strings.Contains(got, "invalid") {
			t.Errorf("error should report writ.personId as invalid, got %q", got)
		}
		if !strings.Contains(got, "alice") {
			t.Errorf("error should quote the offending value, got %q", got)
		}
		if strings.Contains(got, "or user.email") {
			t.Errorf("error told a user who configured writ.personId to configure it; got %q", got)
		}
	})

	t.Run("nothing configured is reported as missing", func(t *testing.T) {
		env := setupTestCLIEnv(t)
		setupSigningKey(t, env.repoDir)

		reviewID := openReviewWithRevision(t, env.repoDir)

		// Blank the address only now: since WRIT-131 a whitespace-only
		// user.email is not an identity, so a repo configured this way from
		// the start could not open the review being approved here.
		setGitConfig(t, env.repoDir, "user.email", "   ")

		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{
			"review", "approve", "-C", env.repoDir, reviewID,
		}, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("approve should refuse with nothing to derive from; stdout: %s", stdout.String())
		}
		got := stderr.String()
		if !strings.Contains(got, "writ.personId") {
			t.Errorf("error should name writ.personId, got %q", got)
		}
		// The guidance naming a concrete identifier must reach the reader
		// whichever arm produces it.
		if !strings.Contains(got, "user:alice") {
			t.Errorf("error should carry the example through, got %q", got)
		}
	})
}
