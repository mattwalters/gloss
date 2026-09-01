package main

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestQuickstart(t *testing.T) {
	requireGit(t)
	ctx := context.Background()

	env := setupTestCLIEnv(t)
	aliceDir := env.repoDir
	setupSigningKey(t, aliceDir)

	// Step 1: Initial commit in repo
	commitFile(t, aliceDir, "README.md", "# My Project\n", "Initial commit")
	cmd := exec.Command("git", "branch", "-M", "main")
	cmd.Dir = aliceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rename branch to main: %v (%s)", err, string(out))
	}

	// Step 2: writ init
	var stdout, stderr bytes.Buffer
	code := run(ctx, []string{"init", "-C", aliceDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("step 2 (writ init) failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Writer ID:") {
		t.Errorf("step 2 output missing Writer ID: %s", stdout.String())
	}

	// Step 3: Create feature branch and open review
	cmd = exec.Command("git", "checkout", "-b", "feature")
	cmd.Dir = aliceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout feature branch: %v (%s)", err, string(out))
	}
	commitFile(t, aliceDir, "main.go", "package main\n", "Add main entry point")

	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{
		"review", "open", "-C", aliceDir,
		"-title", "Add main entry point",
		"-base", "main", "-head", "feature",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("step 3 (review open) failed with %d; stderr: %s", code, stderr.String())
	}

	openOut := strings.TrimSpace(stdout.String())
	idRe := regexp.MustCompile(`^([0-9a-f]{32}) \(open\) Add main entry point$`)
	matches := idRe.FindStringSubmatch(openOut)
	if len(matches) < 2 {
		t.Fatalf("unexpected review open output: %q", openOut)
	}
	reviewID := matches[1]

	// Step 4: Add a comment
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{
		"review", "comment", "-C", aliceDir,
		reviewID, "-m", "Looks great, ready for review.",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("step 4 (review comment) failed with %d; stderr: %s", code, stderr.String())
	}
	commentID := strings.TrimSpace(stdout.String())
	if len(commentID) != 32 {
		t.Fatalf("unexpected comment ID format: %q", commentID)
	}

	// Step 5: Record approval
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{
		"review", "approve", "-C", aliceDir,
		reviewID, "-verdict", "approve", "-m", "LGTM",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("step 5 (review approve) failed with %d; stderr: %s", code, stderr.String())
	}

	// Step 6: Set up remote and sync
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(bareDir, true); err != nil {
		t.Fatalf("init bare remote: %v", err)
	}
	addRemote(t, aliceDir, "origin", bareDir)

	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"sync", "-C", aliceDir, "origin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("step 6 (writ sync) failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "origin: pushed") {
		t.Errorf("step 6 output missing pushed ops: %s", stdout.String())
	}

	// Step 7: Collaborator clones and syncs
	bobDir := filepath.Join(t.TempDir(), "collab")
	cmd = exec.Command("git", "clone", bareDir, bobDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone to bobDir: %v (%s)", err, string(out))
	}
	setGitConfig(t, bobDir, "user.name", "Bob")
	setGitConfig(t, bobDir, "user.email", "bob@example.com")
	setupSigningKey(t, bobDir)

	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"init", "-C", bobDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bob init failed with %d; stderr: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"sync", "-C", bobDir, "origin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bob sync failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "origin: fetched") {
		t.Errorf("bob sync output missing fetched ops: %s", stdout.String())
	}

	// Collaborator lists reviews
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"review", "list", "-C", bobDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bob review list failed with %d; stderr: %s", code, stderr.String())
	}
	listOut := stdout.String()
	if !strings.Contains(listOut, reviewID[:8]) {
		t.Errorf("bob review list missing review ID %s: %s", reviewID[:8], listOut)
	}
	if !strings.Contains(listOut, "Add main entry point") {
		t.Errorf("bob review list missing review title: %s", listOut)
	}

	// Collaborator views review status
	stdout.Reset()
	stderr.Reset()
	code = run(ctx, []string{"review", "status", "-C", bobDir, reviewID[:8]}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("bob review status failed with %d; stderr: %s", code, stderr.String())
	}
	statusOut := stdout.String()
	if !strings.Contains(statusOut, "Approvals:   1") {
		t.Errorf("bob review status missing Approvals: 1: %s", statusOut)
	}
	if !strings.Contains(statusOut, "Revisions:   1") {
		t.Errorf("bob review status missing Revisions: 1: %s", statusOut)
	}
}
